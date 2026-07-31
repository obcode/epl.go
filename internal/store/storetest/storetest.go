// Package storetest hands an integration test a real, migrated PostgreSQL schema.
//
// Real, because the rules this project has to get right do not survive a mock. The wish
// visibility filter is a WHERE clause; "no aggregates before publication" is what COUNT does
// with that clause; the generic error mapping on the write path exists because PostgreSQL
// raises a specific SQLSTATE. A fake store would let all three pass while the shipped query
// leaks.
//
// # Where the database comes from
//
// Two sources, resolved once per test binary:
//
//   - $TALLOX_TEST_DB_URL — a database that already exists. The DevContainer sets it, and it
//     is the fast path: no image pull, no container start, a test run costs milliseconds.
//   - Testcontainers — a throwaway postgres:18 container, started on demand. Needs a Docker
//     socket, which CI has and the DevContainer does not.
//
// Default is "whichever works", env first. $TALLOX_TEST_DB pins it to "env" or "container"
// when that matters — CI runs the suite both ways, so neither path can rot unnoticed.
//
// # Why a schema and not a database
//
// Every call to New creates its own PostgreSQL schema, migrates it, and drops it in
// t.Cleanup. Tests are therefore isolated without being serialised: they can call
// t.Parallel(), and a failing test leaves nothing behind for the next one to trip over.
// Creating a schema is also roughly two orders of magnitude cheaper than creating a database,
// which is what makes "one schema per test" affordable rather than a thing people work
// around by sharing fixtures.
//
// # Skipping
//
// With no database reachable, tests skip — a laptop without Docker should still be able to
// run `go test ./...`. That default is dangerous in CI, where "skipped" and "passed" print
// the same colour, so CI sets $TALLOX_TEST_DB_REQUIRED=1 and the skip becomes a failure.
package storetest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	// Registers the "pgx" database/sql driver, which is how goose reaches the same schema the
	// pool is bound to.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/obcode/tallox.go/internal/store"
)

// Environment variables this package reads.
const (
	// EnvURL points at an existing, reachable database.
	EnvURL = "TALLOX_TEST_DB_URL"
	// EnvMode pins the source: "auto" (default), "env" or "container".
	EnvMode = "TALLOX_TEST_DB"
	// EnvRequired turns "no database, skip" into "no database, fail".
	EnvRequired = "TALLOX_TEST_DB_REQUIRED"
)

// Schema is one test's private corner of the database.
type Schema struct {
	// Name of the PostgreSQL schema. Useful in assertions that name objects explicitly.
	Name string
	// DSN connects to this schema: search_path is already set, so unqualified table names
	// resolve here and nowhere else.
	DSN string
	// Pool is what the store layer uses.
	Pool *pgxpool.Pool
	// DB is the database/sql handle goose needs. Same schema, separate connection.
	DB *sql.DB
}

// New creates a schema, applies every migration to it and returns the handles.
//
// The schema is dropped in t.Cleanup, so a test never has to tidy up and a panicking test
// does not poison the next run.
func New(t *testing.T) *Schema {
	t.Helper()

	s := NewEmpty(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if _, err := store.Migrate(ctx, s.DB); err != nil {
		t.Fatalf("cannot migrate schema %s: %v", s.Name, err)
	}
	return s
}

// NewEmpty creates the schema but applies no migrations.
//
// For tests about migrations themselves — that they apply cleanly from nothing, that Down
// undoes Up. Everything else wants New.
func NewEmpty(t *testing.T) *Schema {
	t.Helper()

	base := baseDSN(t)
	name := schemaName(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("cannot connect to the test database: %v", err)
	}
	// The identifier comes from schemaName, which emits [a-z0-9_] only — but quoting it is
	// free and this is the one place in the codebase that concatenates SQL.
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %q", name)); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("cannot create schema %s: %v", name, err)
	}
	if err := admin.Close(ctx); err != nil {
		t.Logf("cannot close admin connection: %v", err)
	}

	dsn := withSearchPath(t, base, name)

	pool, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("cannot open pool on schema %s: %v", name, err)
	}

	sqlDB, err := openSQL(t, dsn)
	if err != nil {
		pool.Close()
		t.Fatalf("cannot open database/sql handle on schema %s: %v", name, err)
	}

	t.Cleanup(func() {
		pool.Close()
		if err := sqlDB.Close(); err != nil {
			t.Logf("cannot close database/sql handle: %v", err)
		}
		dropSchema(t, base, name)
	})

	return &Schema{Name: name, DSN: dsn, Pool: pool, DB: sqlDB}
}

// withSearchPath returns base with search_path pinned to the given schema.
//
// A DSN string rather than a pgx.ConnConfig, because it has to work three ways: pgxpool for
// the store, database/sql for goose, and — for anything that boots a real server against this
// schema — an environment variable for a subprocess. pgx passes connection-string settings it
// does not recognise through as session runtime parameters, so search_path travels as a plain
// query parameter and every consumer of the string gets it for free.
//
// `public` stays on the path on purpose: extensions live there (see the first migration), so
// dropping it would make citext columns fail to resolve. It comes second, so a test table can
// never be shadowed by something in public.
func withSearchPath(t *testing.T, base, schema string) string {
	t.Helper()

	const searchPath = "search_path"
	value := schema + ",public"

	// Keyword/value form ("host=… user=…") rather than a URL. pgx accepts both; only the URL
	// form needs escaping.
	if !strings.HasPrefix(base, "postgres://") && !strings.HasPrefix(base, "postgresql://") {
		return fmt.Sprintf("%s %s=%s", base, searchPath, value)
	}

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("cannot parse %s: %v", EnvURL, err)
	}
	q := u.Query()
	q.Set(searchPath, value)
	u.RawQuery = q.Encode()

	return u.String()
}

func openSQL(t *testing.T, dsn string) (*sql.DB, error) {
	t.Helper()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	// goose runs one statement after another; more than one connection buys nothing and
	// makes "which session am I in" a question in a package whose whole point is that the
	// answer is always the same schema.
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func dropSchema(t *testing.T, base, name string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Logf("cannot connect to drop schema %s: %v", name, err)
		return
	}
	defer func() { _ = admin.Close(ctx) }()

	if _, err := admin.Exec(ctx, fmt.Sprintf("DROP SCHEMA %q CASCADE", name)); err != nil {
		t.Logf("cannot drop schema %s: %v", name, err)
	}
}

// schemaCounter disambiguates schemas within one test binary. Subtests of a table-driven test
// share a prefix after sanitising, and two schemas with the same name are a confusing failure
// three tests later rather than here.
var schemaCounter atomic.Uint64

var unsafeIdentifierChars = regexp.MustCompile(`[^a-z0-9]+`)

// schemaName derives a PostgreSQL identifier from the test name.
//
// Readable on purpose: when a run leaves a schema behind — a killed process, a hard timeout —
// `\dn` in psql should say which test did it.
func schemaName(t *testing.T) string {
	t.Helper()

	base := unsafeIdentifierChars.ReplaceAllString(strings.ToLower(t.Name()), "_")
	base = strings.Trim(base, "_")

	suffix := fmt.Sprintf("_%d", schemaCounter.Add(1))
	// PostgreSQL truncates identifiers at 63 bytes, silently. Two long test names that differ
	// only past that point would collide, so trim the readable part and keep the counter.
	const maxIdentifier = 63
	if max := maxIdentifier - len("t_") - len(suffix); len(base) > max {
		base = base[:max]
	}
	return "t_" + base + suffix
}

// resolved caches the base DSN for the whole test binary: starting one container per test
// would turn a fast suite into a coffee break.
var resolved = sync.OnceValues(resolveBaseDSN)

func baseDSN(t *testing.T) string {
	t.Helper()

	dsn, err := resolved()
	if err == nil {
		return dsn
	}

	if required() {
		t.Fatalf("no test database: %v\n"+
			"%s=1 is set, so this is a failure rather than a skip — a skipped integration "+
			"test and a passing one look identical in a CI log.", err, EnvRequired)
	}
	t.Skipf("no test database: %v\n"+
		"Set %s, or make a Docker socket available for Testcontainers.", err, EnvURL)
	return ""
}

func required() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvRequired)))
	return v == "1" || v == "true" || v == "yes"
}

func resolveBaseDSN() (string, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(EnvMode)))
	if mode == "" {
		mode = "auto"
	}

	url := strings.TrimSpace(os.Getenv(EnvURL))

	switch mode {
	case "env":
		if url == "" {
			return "", fmt.Errorf("%s=env but %s is empty", EnvMode, EnvURL)
		}
		if err := reachable(url); err != nil {
			return "", fmt.Errorf("%s is set but unreachable: %w", EnvURL, err)
		}
		return url, nil

	case "container":
		return startContainer()

	case "auto":
		if url != "" {
			if err := reachable(url); err == nil {
				return url, nil
			}
			// Deliberately not returning here: a stale URL in a shell profile should not stop
			// a machine that has Docker from running the suite.
		}
		dsn, err := startContainer()
		if err != nil {
			return "", fmt.Errorf("no usable database — %s is unset or unreachable, and "+
				"Testcontainers failed: %w", EnvURL, err)
		}
		return dsn, nil

	default:
		return "", fmt.Errorf("%s=%q is not one of auto, env, container", EnvMode, mode)
	}
}

func reachable(dsn string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	return conn.Close(ctx)
}
