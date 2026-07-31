package storetest_test

import (
	"testing"

	"github.com/obcode/tallox.go/internal/store/storetest"
)

// TestSchemasAreIsolated is the property the whole harness rests on.
//
// Every integration test in this repository assumes it can create a table, insert fixtures
// and assert on counts without another test's rows appearing. If schema isolation ever
// silently stopped working — a search_path that does not travel with the DSN, a name
// collision after PostgreSQL's 63-byte truncation — the symptom would not be this test
// failing. It would be an unrelated policy test failing intermittently in CI, and a day spent
// looking for a bug in the policy.
func TestSchemasAreIsolated(t *testing.T) {
	t.Parallel()

	a, b := storetest.New(t), storetest.New(t)

	if a.Name == b.Name {
		t.Fatalf("two calls to New returned the same schema %q", a.Name)
	}

	if _, err := a.Pool.Exec(t.Context(), `CREATE TABLE isolation_probe (id int)`); err != nil {
		t.Fatalf("cannot create a table in %s: %v", a.Name, err)
	}
	if _, err := a.Pool.Exec(t.Context(), `INSERT INTO isolation_probe VALUES (1)`); err != nil {
		t.Fatalf("cannot insert into %s: %v", a.Name, err)
	}

	// Unqualified, on purpose: this asserts that b's search_path really points at b.
	var n int
	err := b.Pool.QueryRow(t.Context(), `SELECT count(*) FROM isolation_probe`).Scan(&n)
	if err == nil {
		t.Fatalf("schema %s can see the table created in %s (%d rows) — the schemas are not "+
			"isolated", b.Name, a.Name, n)
	}
}

// TestSearchPathPointsAtTheTestSchema pins the mechanism rather than the symptom: a table
// created without a schema qualifier must land in this test's schema, not in public.
//
// Worth its own test because the failure is quiet. If search_path fell back to public, every
// test would still pass — right up to the point where two parallel tests create the same
// table name, and then the failure looks like flakiness.
func TestSearchPathPointsAtTheTestSchema(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)

	var current string
	if err := s.Pool.QueryRow(t.Context(), `SELECT current_schema()`).Scan(&current); err != nil {
		t.Fatalf("cannot read current_schema(): %v", err)
	}
	if current != s.Name {
		t.Errorf("current_schema() is %q, want %q", current, s.Name)
	}
}

// TestCitextIsAvailable guards the reason the first migration exists.
//
// Case-insensitive e-mail comparison is a property of the column type, so if the extension is
// missing the migration that declares such a column fails — but only on a database created
// from nothing, which locally is never. This asserts it on whichever database the harness
// picked.
func TestCitextIsAvailable(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)

	var equal bool
	err := s.Pool.QueryRow(t.Context(),
		`SELECT 'Prof.Eins@Example.ORG'::citext = 'prof.eins@example.org'::citext`).Scan(&equal)
	if err != nil {
		t.Fatalf("cannot compare citext values: %v", err)
	}
	if !equal {
		t.Error("citext comparison is case-sensitive — the extension is not the one we expect")
	}
}

// TestGoldenPoolUsesBerlinTime pins the session timezone.
//
// Milestone deadlines and phase boundaries are local-time (main.go sets time.Local to
// Europe/Berlin). A pool whose sessions default to UTC makes ::date in SQL and the same
// computation in Go disagree — for one hour a day, and only in summer, which is the worst
// possible way to find out.
func TestGoldenPoolUsesBerlinTime(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)

	var tz string
	if err := s.Pool.QueryRow(t.Context(), `SHOW timezone`).Scan(&tz); err != nil {
		t.Fatalf("cannot read the session timezone: %v", err)
	}
	if tz != "Europe/Berlin" {
		t.Errorf("session timezone is %q, want Europe/Berlin", tz)
	}
}
