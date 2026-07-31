package store_test

import (
	"strings"
	"testing"

	"github.com/obcode/tallox.go/internal/store"
	"github.com/obcode/tallox.go/internal/store/storetest"
)

// TestStatusOnAnEmptySchemaListsEverythingAsPending covers the answer somebody wants before a
// deploy: what is about to happen when this image starts.
//
// The empty schema is the interesting direction, because the wrong implementation — reading
// the goose version table and calling anything missing "applied" — produces exactly the
// reassuring answer on a database that has had nothing done to it.
func TestStatusOnAnEmptySchemaListsEverythingAsPending(t *testing.T) {
	t.Parallel()

	s := storetest.NewEmpty(t)

	status, err := store.StatusDSN(t.Context(), s.DSN)
	if err != nil {
		t.Fatalf("cannot read status: %v", err)
	}

	if len(status.Applied) != 0 {
		t.Errorf("an unmigrated schema reports %v as applied", status.Applied)
	}
	if len(status.Pending) == 0 {
		t.Fatal("an unmigrated schema reports nothing pending — the embedded migrations were " +
			"not found, and the report would be reassuring and wrong")
	}

	// Ordered, because the report is read as a list of what will run in which order.
	for i := 1; i < len(status.Pending); i++ {
		if status.Pending[i] <= status.Pending[i-1] {
			t.Errorf("pending migrations are not in order: %v", status.Pending)
			break
		}
	}
}

// TestStatusIsReadOnly is the property that makes the flag safe to point at production.
//
// A status command that migrates as a side effect is worse than no status command: it is used
// precisely at the moment somebody is trying to find out whether a change is safe, and it
// would make the change.
func TestStatusIsReadOnly(t *testing.T) {
	t.Parallel()

	s := storetest.NewEmpty(t)

	for range 3 {
		if _, err := store.StatusDSN(t.Context(), s.DSN); err != nil {
			t.Fatalf("cannot read status: %v", err)
		}
	}

	// Still nothing applied: asking did not answer itself.
	status, err := store.StatusDSN(t.Context(), s.DSN)
	if err != nil {
		t.Fatalf("cannot read status: %v", err)
	}
	if len(status.Applied) != 0 {
		t.Errorf("reading the status applied %v — the command changes what it reports on",
			status.Applied)
	}
}

// TestStatusOnAMigratedSchemaIsQuiet: the ordinary case, and the one the deploy check reads.
func TestStatusOnAMigratedSchemaIsQuiet(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)

	status, err := store.StatusDSN(t.Context(), s.DSN)
	if err != nil {
		t.Fatalf("cannot read status: %v", err)
	}
	if len(status.Pending) != 0 {
		t.Errorf("a migrated schema still reports %v as pending", status.Pending)
	}
	if len(status.Applied) == 0 {
		t.Error("a migrated schema reports nothing as applied")
	}
}

// TestStatusReportSaysWhatARollbackDoesNot pins the sentence, not the formatting.
//
// The report is what somebody reads while deciding whether pinning an older tag is safe, and
// the answer they need is the one that is not obvious: rolling the image back does not roll
// the schema back. Saying it where the question is asked is worth more than saying it in a
// runbook nobody opens under time pressure.
func TestStatusReportSaysWhatARollbackDoesNot(t *testing.T) {
	t.Parallel()

	pending := store.MigrationStatus{Applied: []int64{1}, Pending: []int64{2}}
	report := pending.String()

	if !strings.Contains(report, "2") {
		t.Errorf("the report does not name the pending version:\n%s", report)
	}
	if !strings.Contains(strings.ToLower(report), "rollback") {
		t.Errorf("the report does not mention what a rollback does not do:\n%s", report)
	}

	clean := store.MigrationStatus{Applied: []int64{1, 2}}.String()
	if strings.Contains(strings.ToLower(clean), "rollback") {
		t.Errorf("the warning appears when nothing is pending, which trains people to skip "+
			"it:\n%s", clean)
	}
}
