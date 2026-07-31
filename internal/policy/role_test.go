package policy_test

import (
	"testing"

	"github.com/obcode/tallox.go/internal/policy"
	"github.com/obcode/tallox.go/internal/principal"
	"github.com/obcode/tallox.go/internal/testdata"
)

// TestAllRolesRoundTrip keeps the list, the parser and the stored strings in one piece.
//
// Role values travel as text: into the database, into the GraphQL enum, into the golden
// matrix. A role that is in AllRoles but not parseable, or vice versa, produces a grant that
// exists in one direction only — stored but never recognised, which reads as "my permissions
// vanished" and debugs as anything but.
func TestAllRolesRoundTrip(t *testing.T) {
	t.Parallel()

	seen := map[policy.Role]bool{}

	for _, r := range policy.AllRoles() {
		if seen[r] {
			t.Errorf("%s appears twice in AllRoles", r)
		}
		seen[r] = true

		parsed, ok := policy.ParseRole(string(r))
		if !ok {
			t.Errorf("%s is in AllRoles but ParseRole rejects it", r)
		}
		if parsed != r {
			t.Errorf("ParseRole(%q) = %q", r, parsed)
		}
		if string(r) == "" {
			t.Error("a role with an empty name would be granted by every empty string in the " +
				"database")
		}
	}

	for _, unknown := range []string{"", "planer", "PLANER", "Dozent", "ADMIN "} {
		if _, ok := policy.ParseRole(unknown); ok {
			t.Errorf("ParseRole accepted %q — role matching is exact, and case-insensitive "+
				"matching would make a lower-cased typo a grant", unknown)
		}
	}
}

// TestRolesOfDropsWhatItDoesNotKnow: the one place opaque grant strings acquire meaning, and
// the place where an unknown one has to lose it.
func TestRolesOfDropsWhatItDoesNotKnow(t *testing.T) {
	t.Parallel()

	actor := testdata.Drei.Actor(principal.KindInteractive,
		string(policy.RoleDozent), "PLANER", string(policy.RoleFachgruppenleitung))

	roles := policy.RolesOf(actor)

	if !roles.Has(policy.RoleDozent) || !roles.Has(policy.RoleFachgruppenleitung) {
		t.Errorf("known grants were lost: %v", roles.Sorted())
	}
	if len(roles) != 2 {
		t.Errorf("RolesOf returned %v, want exactly the two known grants", roles.Sorted())
	}

	if roles := policy.RolesOf(principal.Anonymous); len(roles) != 0 {
		t.Errorf("the anonymous actor holds %v", roles.Sorted())
	}
}

// TestPlansIsTheSetOfPeopleWhoFillGaps documents which roles the planning exception covers, in
// a place that fails when somebody widens it.
func TestPlansIsTheSetOfPeopleWhoFillGaps(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		roles []policy.Role
		want  bool
	}{
		{[]policy.Role{}, false},
		{[]policy.Role{policy.RoleDozent}, false},
		{[]policy.Role{policy.RoleAdmin}, false},
		{[]policy.Role{policy.RoleDekanat}, false}, // reads across programmes, does not plan
		{[]policy.Role{policy.RoleFachgruppenleitung}, true},
		{[]policy.Role{policy.RoleStudiengangsleitung}, true},
		{[]policy.Role{policy.RoleDozent, policy.RoleStudiengangsleitung}, true},
	} {
		set := policy.RoleSet{}
		for _, r := range tc.roles {
			set[r] = true
		}
		if got := set.Plans(); got != tc.want {
			t.Errorf("RoleSet%v.Plans() = %v, want %v", tc.roles, got, tc.want)
		}
	}
}

// TestSortedIsStable: the golden matrix and every log line render role sets, and a map
// iteration order would make both of them differ from run to run.
func TestSortedIsStable(t *testing.T) {
	t.Parallel()

	set := policy.RoleSet{
		policy.RoleAdmin:               true,
		policy.RoleDozent:              true,
		policy.RoleStudiengangsleitung: true,
		policy.RoleDekanat:             false, // present in the map, not granted
	}

	first := set.Sorted()
	for range 20 {
		got := set.Sorted()
		if len(got) != len(first) {
			t.Fatalf("Sorted returned %v and %v on successive calls", first, got)
		}
		for i := range got {
			if got[i] != first[i] {
				t.Fatalf("Sorted returned %v and %v on successive calls", first, got)
			}
		}
	}

	want := []policy.Role{policy.RoleDozent, policy.RoleStudiengangsleitung, policy.RoleAdmin}
	if len(first) != len(want) {
		t.Fatalf("Sorted() = %v, want %v — a false entry is not a grant", first, want)
	}
	for i := range want {
		if first[i] != want[i] {
			t.Errorf("Sorted() = %v, want %v (AllRoles order)", first, want)
		}
	}
}
