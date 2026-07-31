package graphqltest_test

import (
	"strings"
	"testing"

	"github.com/obcode/tallox.go/internal/graphqltest"
)

// TestLeaksCatchesTheRealisticWordings checks the leak detector against the strings
// PostgreSQL and pgx actually produce.
//
// A detector that only recognises the wording someone imagined while writing it is worse than
// none: it makes the write-path tests look thorough while waving through the message that
// would really appear.
func TestLeaksCatchesTheRealisticWordings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		message   string
		forbidden []string
		want      []string
	}{
		{
			name:      "raw uniqueness violation",
			message:   `ERROR: duplicate key value violates unique constraint "wishes_part_person_key" (SQLSTATE 23505)`,
			forbidden: graphqltest.DatabaseNoise(),
			want:      []string{"SQLSTATE", "duplicate key", "violates unique constraint", "ERROR:"},
		},
		{
			name:      "another person's mail address, different case",
			message:   "Prof.Zwei@Example.ORG hat sich bereits eingetragen.",
			forbidden: []string{"prof.zwei@example.org"},
			want:      []string{"prof.zwei@example.org"},
		},
		{
			name:      "the generic message we actually want to ship",
			message:   "Der Wunsch konnte nicht gespeichert werden.",
			forbidden: append(graphqltest.DatabaseNoise(), "prof.zwei@example.org"),
			want:      nil,
		},
		{
			// Forbidden lists get built from optional fields — a person without a second
			// mail address contributes "". Treating that as a match would fail every test.
			name:      "empty needles are ignored rather than matching everything",
			message:   "Der Wunsch konnte nicht gespeichert werden.",
			forbidden: []string{""},
			want:      nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := graphqltest.Leaks(tc.message, tc.forbidden)

			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("Leaks(%q) = %v, want %v", tc.message, got, tc.want)
			}
		})
	}
}

// TestDatabaseNoiseCoversTheCommonSuspects is a cheap guard against someone trimming the list
// down to "SQLSTATE" during a cleanup. Each entry is there because it appears in a real error.
func TestDatabaseNoiseCoversTheCommonSuspects(t *testing.T) {
	t.Parallel()

	noise := strings.Join(graphqltest.DatabaseNoise(), "|")
	for _, want := range []string{"SQLSTATE", "duplicate key", "violates unique constraint"} {
		if !strings.Contains(noise, want) {
			t.Errorf("DatabaseNoise no longer mentions %q", want)
		}
	}
}
