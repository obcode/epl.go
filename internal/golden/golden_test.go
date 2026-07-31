package golden_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/obcode/tallox.go/internal/golden"
)

// Deliberately none of these tests call t.Parallel(): they change the working directory and
// the -update-golden flag, both of which are process-wide.

// TestAssertMatchesTheCommittedFile is the happy path, and it doubles as proof that the
// committed fixture next to it is reachable from a test run in this package.
func TestAssertMatchesTheCommittedFile(t *testing.T) {
	golden.Assert(t, "example", "role\tphase\tvisible\nLECTURER\tWISHES\town only\n")
}

// TestAssertTolerantOfTrailingNewlines covers the diff that would otherwise appear on every
// save in an editor configured to add a final newline — and which, once it appears often
// enough, teaches reviewers to skim past golden diffs. That habit is the real risk: a
// mechanism whose alerts get ignored protects nothing.
func TestAssertTolerantOfTrailingNewlines(t *testing.T) {
	golden.Assert(t, "example", "role\tphase\tvisible\nLECTURER\tWISHES\town only")
}

// TestUpdateRecordsAndThenMatches exercises -update-golden, the path CI never takes.
//
// Worth a test precisely because CI never takes it: if re-recording broke, the first person to
// find out would be someone mid-review trying to update a matrix, at the least convenient
// moment. Runs against a temporary package directory so the repository's own testdata is left
// alone.
func TestUpdateRecordsAndThenMatches(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	const content = "role\tphase\tvisible\nPLANER\tZUTEILUNG\tall\n"

	withFlag(t, "update-golden", "true", func() {
		golden.Assert(t, "recorded", content)
	})

	written, err := os.ReadFile(filepath.Join(dir, "testdata", "recorded.golden"))
	if err != nil {
		t.Fatalf("-update-golden did not create the file: %v", err)
	}
	if string(written) != content {
		t.Errorf("recorded %q, want %q", written, content)
	}

	// And the recording has to satisfy the comparison it was recorded for. If these two
	// halves disagreed — a normalisation applied on write but not on read, say — every golden
	// file would be permanently dirty right after being updated.
	golden.Assert(t, "recorded", content)
}

// withFlag sets a test flag for the duration of fn and restores it afterwards.
func withFlag(t *testing.T, name, value string, fn func()) {
	t.Helper()

	f := flag.Lookup(name)
	if f == nil {
		t.Fatalf("flag -%s is not registered", name)
	}
	before := f.Value.String()

	if err := flag.Set(name, value); err != nil {
		t.Fatalf("cannot set -%s: %v", name, err)
	}
	defer func() {
		if err := flag.Set(name, before); err != nil {
			t.Fatalf("cannot restore -%s: %v", name, err)
		}
	}()

	fn()
}

func chdir(t *testing.T, dir string) {
	t.Helper()

	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot read the working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("cannot change to %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(before); err != nil {
			t.Fatalf("cannot change back to %s: %v", before, err)
		}
	})
}
