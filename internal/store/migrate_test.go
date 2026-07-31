package store_test

import (
	"context"
	"testing"

	"github.com/obcode/tallox.go/internal/store"
	"github.com/obcode/tallox.go/internal/store/storetest"
)

// TestMigrationsApplyFromNothing is the test that keeps a fresh database honest.
//
// The DevContainer's Postgres has been migrated incrementally for as long as it has existed,
// so it will happily run a migration that only works because of something an earlier,
// since-edited migration left behind. Production is not like that: the host's volume was
// created once, and a Testcontainers database is created from nothing on every CI run. This
// test forces that second case.
func TestMigrationsApplyFromNothing(t *testing.T) {
	t.Parallel()

	s := storetest.NewEmpty(t)

	applied, err := store.Migrate(t.Context(), s.DB)
	if err != nil {
		t.Fatalf("cannot migrate an empty schema: %v", err)
	}
	if applied == 0 {
		t.Fatal("no migrations were applied — db/migrations is empty or was not embedded")
	}

	status, err := store.StatusDSN(t.Context(), s.DSN)
	if err != nil {
		t.Fatalf("cannot read migration status: %v", err)
	}
	if len(status.Pending) != 0 {
		t.Errorf("after a full migration run %d are still pending: %v",
			len(status.Pending), status.Pending)
	}
	if len(status.Applied) != applied {
		t.Errorf("status reports %d applied, the run applied %d", len(status.Applied), applied)
	}
}

// TestMigrationsAreIdempotent asserts that running the migrator twice is a no-op.
//
// Not academic: migrations are applied at startup, and every deploy restarts the container,
// so a second run has to cost nothing. Sequentially, that is — the simultaneous case is
// TestMigrationsSurviveConcurrentMigrators below, and it is a genuinely different question.
func TestMigrationsAreIdempotent(t *testing.T) {
	t.Parallel()

	s := storetest.New(t) // already migrated once

	applied, err := store.Migrate(t.Context(), s.DB)
	if err != nil {
		t.Fatalf("cannot re-run migrations: %v", err)
	}
	if applied != 0 {
		t.Errorf("a second migration run applied %d migrations, want 0", applied)
	}
}

// TestMigrationsSurviveConcurrentMigrators runs several migrators at the same moment.
//
// This is the test that a red CI job bought. `CREATE EXTENSION IF NOT EXISTS` is not atomic:
// the existence check and the catalog insert are separate steps, so two sessions doing it
// simultaneously both see "not there" and the loser gets SQLSTATE 23505 on
// pg_extension_name_index. Sequential idempotence — running twice, one after the other —
// passes right through that.
//
// It matters twice over. In CI the integration tests migrate their schemas in parallel
// against a database created seconds earlier, and in production a deploy that starts two API
// replicas has both migrating at startup. Neither is reproducible on a developer machine,
// where the extension has existed since the volume was created and the IF NOT EXISTS
// short-circuits — so locally this test is nearly vacuous and in CI it is the real thing.
// That asymmetry is exactly why the from-scratch CI job exists.
func TestMigrationsSurviveConcurrentMigrators(t *testing.T) {
	t.Parallel()

	const migrators = 4

	schemas := make([]*storetest.Schema, migrators)
	for i := range schemas {
		schemas[i] = storetest.NewEmpty(t)
	}

	// A barrier, so the migrations genuinely overlap. Starting four goroutines and hoping
	// the scheduler interleaves them would make this test pass for the wrong reason on a
	// loaded machine.
	start := make(chan struct{})
	errs := make(chan error, migrators)

	for _, s := range schemas {
		go func() {
			<-start
			_, err := store.Migrate(context.Background(), s.DB)
			errs <- err
		}()
	}
	close(start)

	for range migrators {
		if err := <-errs; err != nil {
			t.Errorf("concurrent migration failed: %v", err)
		}
	}
}

// TestMigrationsAreReversible runs up → down → up.
//
// The Down direction is not decoration. It is what makes a rollback on the host something
// other than a restore from backup, and the only moment anyone finds out whether it works is
// the moment they need it. Running it in CI moves that discovery to a Tuesday afternoon.
func TestMigrationsAreReversible(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)

	if err := store.MigrateDown(t.Context(), s.DB); err != nil {
		t.Fatalf("cannot roll back: %v", err)
	}

	status, err := store.Status(t.Context(), s.DB)
	if err != nil {
		t.Fatalf("cannot read migration status after rollback: %v", err)
	}
	if len(status.Pending) == 0 {
		t.Fatal("after rolling everything back nothing is pending — Down did not undo Up")
	}
	if len(status.Applied) != 0 {
		t.Errorf("after rolling everything back %v are still applied", status.Applied)
	}

	if _, err := store.Migrate(t.Context(), s.DB); err != nil {
		t.Fatalf("cannot migrate up again after a rollback: %v", err)
	}
}

// TestOpenRejectsAnUnreachableDatabase covers the failure the deploy script depends on: a bad
// DSN has to stop the server at startup rather than surface later as a 500 on whichever
// request happened to arrive first.
func TestOpenRejectsAnUnreachableDatabase(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // no network round-trip needed: an already-cancelled context proves the path

	if _, err := store.Open(ctx, "postgres://nobody@127.0.0.1:1/nothing?sslmode=disable"); err == nil {
		t.Error("Open returned no error for an unreachable database")
	}
}
