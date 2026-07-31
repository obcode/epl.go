package store_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/obcode/tallox.go/internal/policy"
	"github.com/obcode/tallox.go/internal/store"
	"github.com/obcode/tallox.go/internal/store/storetest"
	"github.com/obcode/tallox.go/internal/testdata"
)

// TestPersonByMailReturnsTheRolesInOneRoundTrip covers the query the browser door
// authenticates with.
//
// Roles come back with the person on purpose. Two queries would leave a window in which a
// person exists and their grants do not, and the authenticator would have to decide what to
// do with a caller whose permissions it could not read — a decision with only bad answers.
func TestPersonByMailReturnsTheRolesInOneRoundTrip(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	storetest.SeedPerson(t, s, testdata.Drei,
		string(policy.RoleLecturer), string(policy.RoleSubjectGroupLead))

	got, err := s.Queries().PersonByMail(t.Context(), testdata.Drei.Mail)
	if err != nil {
		t.Fatalf("cannot read %s: %v", testdata.Drei.Mail, err)
	}

	if got.ID != testdata.Drei.ID() {
		t.Errorf("id is %v, want %v", got.ID, testdata.Drei.ID())
	}
	if !got.Active {
		t.Error("a freshly created person is not active")
	}
	// Sorted by the query, so that a role set renders identically in a log line, a golden file
	// and a GraphQL response regardless of the order the grants were inserted in.
	want := []string{string(policy.RoleLecturer), string(policy.RoleSubjectGroupLead)}
	if strings.Join(got.Roles, ",") != strings.Join(want, ",") {
		t.Errorf("roles are %v, want %v", got.Roles, want)
	}
}

// TestPersonWithoutRolesHasAnEmptySlice pins what the LEFT JOIN does for somebody who has
// been imported but not yet granted anything.
//
// The dangerous alternative is a nil that a caller reads as "roles unknown". Everything
// downstream treats an empty role set as "no permissions", which is the right answer for a
// person who genuinely holds no grant — but only if it actually arrives as empty rather than
// as an error nobody handles.
func TestPersonWithoutRolesHasAnEmptySlice(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	storetest.SeedPerson(t, s, testdata.Zwei)

	got, err := s.Queries().PersonByMail(t.Context(), testdata.Zwei.Mail)
	if err != nil {
		t.Fatalf("cannot read %s: %v", testdata.Zwei.Mail, err)
	}
	if len(got.Roles) != 0 {
		t.Errorf("a person with no grants has roles %v", got.Roles)
	}
}

// TestMailMatchingIgnoresCase is the citext column doing its job.
//
// The casing in X-Remote-User comes from the identity provider and is not ours to rely on. A
// case-sensitive column would create a second account the day it changes, and the symptom
// reported would be "my wishes are gone" — a data-loss bug report for what is really a
// collation choice.
func TestMailMatchingIgnoresCase(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	storetest.SeedPerson(t, s, testdata.Eins, string(policy.RoleLecturer))

	for _, spelling := range []string{
		testdata.Eins.Mail,
		strings.ToUpper(testdata.Eins.Mail),
		"Prof.Eins@Example.ORG",
	} {
		got, err := s.Queries().PersonByMail(t.Context(), spelling)
		if err != nil {
			t.Fatalf("cannot read %q: %v", spelling, err)
		}
		if got.ID != testdata.Eins.ID() {
			t.Errorf("%q resolved to %v, want %v", spelling, got.ID, testdata.Eins.ID())
		}
	}

	// The other half: a differently-cased address cannot be inserted as a second person.
	_, err := s.Queries().CreatePerson(t.Context(), store.CreatePersonParams{
		ID:   uuid.New(),
		Mail: strings.ToUpper(testdata.Eins.Mail),
		Name: "Doppelgänger",
	})
	if err == nil {
		t.Error("the same address in different casing was accepted as a second person")
	}
}

// TestTheNilPersonIsRejected guards the value principal.Actor.Owns treats as "nobody".
//
// An unauthenticated caller carries uuid.Nil, and so does an owner column that was never
// filled in. Owns refuses to match it on either side — which is a defence only as long as no
// real person holds it. A row with the nil id would turn that guard into a bug report.
func TestTheNilPersonIsRejected(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)

	_, err := s.Queries().CreatePerson(t.Context(), store.CreatePersonParams{
		ID:   uuid.Nil,
		Mail: "niemand@example.org",
		Name: "Niemand",
	})
	if err == nil {
		t.Error("a person with the nil UUID was accepted")
	}
}

// TestDatabaseAndPolicyAgreeOnRoles is the drift test between two files that are maintained
// in different languages and cannot import each other.
//
// The CHECK constraint decides which grants can be stored; internal/policy decides what they
// mean. Drift is silent in both directions and unpleasant in both: a role the policy knows and
// the database rejects makes granting it fail with a constraint violation in the GUI, and a
// role the database accepts and the policy does not know is a grant somebody was told they
// had and that does nothing.
func TestDatabaseAndPolicyAgreeOnRoles(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)

	// Scoped to this test's schema. Every parallel test has a constraint of the same name, and
	// they come and go while this query runs — an unscoped lookup fails with "could not open
	// relation with OID …" as another schema is dropped underneath it.
	var definition string
	err := s.Pool.QueryRow(t.Context(),
		`SELECT pg_get_constraintdef(c.oid)
		   FROM pg_constraint c
		   JOIN pg_class t ON t.oid = c.conrelid
		   JOIN pg_namespace n ON n.oid = t.relnamespace
		  WHERE c.conname = 'person_role_role_known' AND n.nspname = current_schema()`,
	).Scan(&definition)
	if err != nil {
		t.Fatalf("cannot read the role constraint — has it been renamed? %v", err)
	}

	for _, role := range policy.AllRoles() {
		if !strings.Contains(definition, "'"+string(role)+"'") {
			t.Errorf("policy knows the role %s but the database constraint does not list it:\n  %s",
				role, definition)
		}
	}

	// And the other direction: every literal in the constraint has to be a role the policy
	// gives meaning to.
	for _, literal := range strings.Split(definition, "'") {
		if literal == "" || strings.ContainsAny(literal, " (),=:") {
			continue // SQL between the quoted literals
		}
		if _, ok := policy.ParseRole(literal); !ok {
			t.Errorf("the database accepts the role %q, which internal/policy does not know — "+
				"granting it would do nothing", literal)
		}
	}
}

// TestUnknownRolesCannotBeGranted covers the write side of the same agreement. A typo in a
// hand-written INSERT — or in a future admin mutation — has to fail at the database rather
// than land as a grant that silently means nothing.
func TestUnknownRolesCannotBeGranted(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	storetest.SeedPerson(t, s, testdata.Vier)

	// "PLANNER" is the word the documentation uses for the concept, which makes it exactly the
	// string somebody will one day try to grant.
	err := s.Queries().GrantRole(t.Context(), store.GrantRoleParams{
		PersonID: testdata.Vier.ID(),
		Role:     "PLANNER",
	})
	if err == nil {
		t.Error("the role PLANNER was accepted — the CHECK constraint is not doing its job")
	}
}

// TestGrantingTwiceIsNotAnError: the grant is idempotent, so an admin mutation does not have
// to read first and race with itself.
func TestGrantingTwiceIsNotAnError(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	storetest.SeedPerson(t, s, testdata.Vier, string(policy.RoleLecturer))

	grant := store.GrantRoleParams{
		PersonID: testdata.Vier.ID(),
		Role:     string(policy.RoleLecturer),
	}
	if err := s.Queries().GrantRole(t.Context(), grant); err != nil {
		t.Fatalf("granting an existing role failed: %v", err)
	}

	got, err := s.Queries().PersonByID(t.Context(), testdata.Vier.ID())
	if err != nil {
		t.Fatalf("cannot read back: %v", err)
	}
	if len(got.Roles) != 1 {
		t.Errorf("roles are %v after granting the same role twice", got.Roles)
	}
}

// TestRevokingARoleTakesItAway, with the demotion it implies: permissions are read from the
// person on every request, so this is also what demotes every token that person holds.
func TestRevokingARoleTakesItAway(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	storetest.SeedPerson(t, s, testdata.Vier,
		string(policy.RoleLecturer), string(policy.RoleProgrammeLead))

	err := s.Queries().RevokeRole(t.Context(), store.RevokeRoleParams{
		PersonID: testdata.Vier.ID(),
		Role:     string(policy.RoleProgrammeLead),
	})
	if err != nil {
		t.Fatalf("cannot revoke: %v", err)
	}

	got, err := s.Queries().PersonByID(t.Context(), testdata.Vier.ID())
	if err != nil {
		t.Fatalf("cannot read back: %v", err)
	}
	if len(got.Roles) != 1 || got.Roles[0] != string(policy.RoleLecturer) {
		t.Errorf("roles after revocation are %v, want only %s", got.Roles, policy.RoleLecturer)
	}
}
