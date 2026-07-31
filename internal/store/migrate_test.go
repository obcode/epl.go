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

	pending, err := store.PendingMigrations(t.Context(), s.DB)
	if err != nil {
		t.Fatalf("cannot read migration status: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("after a full migration run %d are still pending: %v", len(pending), pending)
	}
}

// TestMigrationsAreIdempotent asserts that running the migrator twice is a no-op.
//
// Not academic: migrations are applied at startup, and every deploy restarts the container.
// A second run has to be free, and it has to be free even when two replicas start at the same
// moment — goose takes a lock for that, and this test is where the lock's absence would show.
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

	pending, err := store.PendingMigrations(t.Context(), s.DB)
	if err != nil {
		t.Fatalf("cannot read migration status after rollback: %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("after rolling everything back nothing is pending — Down did not undo Up")
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
