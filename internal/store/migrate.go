package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"

	"github.com/obcode/tallox.go/db"
)

// migrationsFS is db/migrations, re-rooted so goose sees the .sql files at the top level.
func migrationsFS() (fs.FS, error) {
	sub, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("cannot open embedded migrations: %w", err)
	}
	return sub, nil
}

// Migrate applies all pending migrations to whatever schema the handle's search_path points
// at, and reports how many it applied.
//
// It takes a *sql.DB rather than the pgxpool because goose speaks database/sql. That is also
// why this function lives here and not in a migration package of its own: database/sql is on
// the list of imports the architecture test confines to internal/store, and confining it is
// the point.
//
// Uses goose's provider API rather than the package-level goose.Up: the package-level
// functions keep the dialect and the base filesystem in globals, which is fine for a CLI and
// a race for parallel integration tests, each of which migrates its own throwaway schema.
func Migrate(ctx context.Context, sqlDB *sql.DB) (int, error) {
	fsys, err := migrationsFS()
	if err != nil {
		return 0, err
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys)
	if err != nil {
		return 0, fmt.Errorf("cannot set up migrations: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("cannot apply migrations: %w", err)
	}
	return len(results), nil
}

// MigrateDown rolls back every applied migration, down to an empty schema.
//
// Exists for tests — the reversibility test in this package, and any harness that wants to
// prove a migration pair is symmetric before it is released. Never called by the server: a
// production process that can undo its own schema is one signal handler away from an
// incident.
func MigrateDown(ctx context.Context, sqlDB *sql.DB) error {
	fsys, err := migrationsFS()
	if err != nil {
		return err
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys)
	if err != nil {
		return fmt.Errorf("cannot set up migrations: %w", err)
	}

	if _, err := provider.DownTo(ctx, 0); err != nil {
		return fmt.Errorf("cannot roll back migrations: %w", err)
	}
	return nil
}

// PendingMigrations reports the versions that are embedded but not yet applied.
//
// The server uses this to log what it is about to do; the deploy runbook uses it to answer
// "is this rollback safe" without guessing from tags.
func PendingMigrations(ctx context.Context, sqlDB *sql.DB) ([]int64, error) {
	fsys, err := migrationsFS()
	if err != nil {
		return nil, err
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys)
	if err != nil {
		return nil, fmt.Errorf("cannot set up migrations: %w", err)
	}

	sources, err := provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot read migration status: %w", err)
	}

	pending := []int64{}
	for _, s := range sources {
		if s.State == goose.StatePending {
			pending = append(pending, s.Source.Version)
		}
	}
	return pending, nil
}
