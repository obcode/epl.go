package store_test

import (
	"testing"

	"github.com/obcode/tallox.go/internal/policy"
	"github.com/obcode/tallox.go/internal/store"
	"github.com/obcode/tallox.go/internal/store/storetest"
	"github.com/obcode/tallox.go/internal/testdata"
)

// TestBootstrapCreatesTheFirstAdministrator covers the case the mechanism exists for: a
// database that has just been created, where nobody can sign in and therefore nobody can be
// given access either.
func TestBootstrapCreatesTheFirstAdministrator(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)

	created, err := store.BootstrapAdmin(t.Context(), s.Pool, "admin@example.org", "Admin")
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	if !created {
		t.Fatal("bootstrap reported that it created nobody on an empty database")
	}

	got, err := s.Queries().PersonByMail(t.Context(), "admin@example.org")
	if err != nil {
		t.Fatalf("the bootstrapped person is not readable: %v", err)
	}
	if !got.Active {
		t.Error("the first administrator is not active")
	}
	if len(got.Roles) != 1 || got.Roles[0] != string(policy.RoleAdmin) {
		t.Errorf("the first administrator holds %v, want only %s — an administrator without "+
			"the role is the one outcome worse than no administrator, because the bootstrap "+
			"never runs again", got.Roles, policy.RoleAdmin)
	}
}

// TestBootstrapNeverTouchesAPopulatedDatabase is the safety property, and the reason a flag
// may carry a mail address at all.
//
// If this could act on a database that already has people in it, the flag would be a way to
// mint an administrator on a running installation — available to anybody who can change the
// deploy configuration, and invisible in the audit log because it happens before anybody
// signs in. The WHERE NOT EXISTS in the query is what prevents that; this test is what keeps
// it there.
func TestBootstrapNeverTouchesAPopulatedDatabase(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	// One ordinary person, no ADMIN anywhere.
	storetest.SeedPerson(t, s, testdata.Zwei, string(policy.RoleLecturer))

	for _, mail := range []string{
		"neu@example.org",  // somebody who does not exist
		testdata.Zwei.Mail, // and somebody who does
	} {
		created, err := store.BootstrapAdmin(t.Context(), s.Pool, mail, "Would-be Admin")
		if err != nil {
			t.Fatalf("bootstrap on a populated database returned an error: %v", err)
		}
		if created {
			t.Errorf("bootstrap created %q although the database already had people", mail)
		}
	}

	// Nothing was added, and — the part that matters — nobody was promoted.
	existing, err := s.Queries().PersonByMail(t.Context(), testdata.Zwei.Mail)
	if err != nil {
		t.Fatalf("cannot read back: %v", err)
	}
	if len(existing.Roles) != 1 || existing.Roles[0] != string(policy.RoleLecturer) {
		t.Errorf("%s now holds %v — the bootstrap promoted an existing account",
			testdata.Zwei.Name, existing.Roles)
	}

	if _, err := s.Queries().PersonByMail(t.Context(), "neu@example.org"); err == nil {
		t.Error("bootstrap created a second person on a populated database")
	}
}

// TestBootstrapIsIdempotent: the flag stays in the deploy configuration, so this runs on
// every restart. The second run has to be a silent no-op rather than an error that stops the
// server from starting.
func TestBootstrapIsIdempotent(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)

	first, err := store.BootstrapAdmin(t.Context(), s.Pool, "admin@example.org", "Admin")
	if err != nil || !first {
		t.Fatalf("first run: created=%v err=%v", first, err)
	}

	second, err := store.BootstrapAdmin(t.Context(), s.Pool, "admin@example.org", "Admin")
	if err != nil {
		t.Fatalf("second run failed: %v — the server would refuse to start on every restart "+
			"after the first", err)
	}
	if second {
		t.Error("the second run reported that it created somebody")
	}
}

// TestBootstrapWithoutAMailIsRefused: the flag is a string, and an empty one has to be a
// clear error rather than a person with no identity — the mail address is what the auth proxy
// matches against, so a blank one is an account nobody can ever sign in to.
func TestBootstrapWithoutAMailIsRefused(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)

	if _, err := store.BootstrapAdmin(t.Context(), s.Pool, "", "Admin"); err == nil {
		t.Error("an empty mail address was accepted")
	}
}

// TestBootstrapDefaultsTheNameToTheMail documents what the GUI will show.
//
// A placeholder that is obviously a placeholder, on purpose: there is no name to invent at
// first boot, and a mail address in a name column is self-explaining once somebody looks at
// it. The alternative — deriving "Oliver Braun" from an address — guesses at how people write
// their own names and gets it wrong for anybody with a title, a double name or an umlaut.
func TestBootstrapDefaultsTheNameToTheMail(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)

	if _, err := store.BootstrapAdmin(t.Context(), s.Pool, "admin@example.org", ""); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	got, err := s.Queries().PersonByMail(t.Context(), "admin@example.org")
	if err != nil {
		t.Fatalf("cannot read back: %v", err)
	}
	if got.Name != "admin@example.org" {
		t.Errorf("name is %q, want the mail address as a visible placeholder", got.Name)
	}
}
