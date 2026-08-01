package policy_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/obcode/tallox.go/internal/policy"
	"github.com/obcode/tallox.go/internal/principal"
)

// TestOnlyAnInteractiveAdminAdministersPeople.
//
// Two conditions, and the second is the one that gets dropped by somebody tidying up: ADMIN is
// necessary and not sufficient, because granting a role through a long-lived token in a script
// would decouple "who did this" from any sign-in — and the thing it would decouple is the
// granting of access itself.
func TestOnlyAnInteractiveAdminAdministersPeople(t *testing.T) {
	t.Parallel()

	id := uuid.New()

	for _, tc := range []struct {
		name  string
		actor principal.Actor
		want  bool
	}{
		{
			name:  "admin in a browser",
			actor: principal.Actor{ID: id, Roles: []string{"ADMIN"}, Kind: principal.KindInteractive},
			want:  true,
		},
		{
			name:  "admin through a token",
			actor: principal.Actor{ID: id, Roles: []string{"ADMIN"}, Kind: principal.KindToken},
			want:  false,
		},
		{
			name: "every other role, in a browser",
			actor: principal.Actor{ID: id, Kind: principal.KindInteractive, Roles: []string{
				"LECTURER", "SUBJECT_GROUP_LEAD", "PROGRAMME_LEAD", "DEANS_OFFICE",
			}},
			want: false,
		},
		{
			name:  "anonymous",
			actor: principal.Anonymous,
			want:  false,
		},
		{
			name: "a mail address the proxy asserted but no person row",
			// Authenticated() is the id, not the address. A caller the lookup never resolved
			// must not administer anybody.
			actor: principal.Actor{Mail: "nobody@example.org", Roles: []string{"ADMIN"},
				Kind: principal.KindInteractive},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.MayAdministerPeople(tc.actor); got != tc.want {
				t.Errorf("MayAdministerPeople = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNarrowCanOnlyEverRemove is the security property of the whole role-preview feature, and
// it is asserted exhaustively rather than by example.
//
// Every combination of held roles crossed with every combination of selected roles: the
// effective set must be a subset of the held set, always. That is what makes the selection
// safe to take from an untrusted header — a hand-written X-Tallox-Assume-Roles sent straight at
// /query gains the sender nothing, so nothing downstream has to check where it came from.
//
// If this ever fails, the feature is a privilege escalation and not a preview.
func TestNarrowCanOnlyEverRemove(t *testing.T) {
	t.Parallel()

	all := policy.AllRoles()

	for held := 0; held < 1<<len(all); held++ {
		for selected := 0; selected < 1<<len(all); selected++ {
			actor := principal.Actor{ID: uuid.New(), Roles: subset(all, held)}
			narrowed := policy.Narrow(actor, policy.ParseRoles(subset(all, selected)))

			heldSet := policy.RolesOf(actor)
			for _, r := range policy.RolesOf(narrowed).Sorted() {
				if !heldSet.Has(r) {
					t.Fatalf("held %v, selected %v: narrowing produced %s — the preview "+
						"granted a role the person does not hold",
						subset(all, held), subset(all, selected), r)
				}
			}
		}
	}
}

// TestNarrowIsTheIntersection pins the rest of the behaviour: not just "no more than held",
// but exactly held ∩ selected. A narrowing that quietly dropped a role somebody did select
// would make the preview lie in the other direction.
func TestNarrowIsTheIntersection(t *testing.T) {
	t.Parallel()

	actor := principal.Actor{
		ID:    uuid.New(),
		Roles: []string{"ADMIN", "LECTURER", "DEANS_OFFICE"},
	}

	narrowed := policy.Narrow(actor, []policy.Role{
		policy.RoleLecturer,
		policy.RoleProgrammeLead, // not held — must not appear
	})

	got := policy.RolesOf(narrowed).Sorted()
	if len(got) != 1 || got[0] != policy.RoleLecturer {
		t.Errorf("effective roles are %v, want only LECTURER", got)
	}
}

// TestNarrowRemembersWhatWasHeld: the interface has to be able to say "this is a preview" and
// to offer the way back, and the way back is the set that was there before.
func TestNarrowRemembersWhatWasHeld(t *testing.T) {
	t.Parallel()

	actor := principal.Actor{ID: uuid.New(), Roles: []string{"ADMIN", "LECTURER"}}
	narrowed := policy.Narrow(actor, []policy.Role{policy.RoleLecturer})

	if !narrowed.Narrowed() {
		t.Fatal("the actor does not report being narrowed — the banner would never appear, " +
			"and somebody would report a missing feature as a bug")
	}
	if len(narrowed.NarrowedFrom) != 2 {
		t.Errorf("NarrowedFrom is %v, want both held roles", narrowed.NarrowedFrom)
	}
}

// TestNarrowToNothingIsAllowed.
//
// "No roles at all" is a real view worth being able to look at: it is what a colleague sees on
// the day the import created their person row and nobody has granted them anything yet. That
// page should be seen by somebody before it happens to them.
func TestNarrowToNothingIsAllowed(t *testing.T) {
	t.Parallel()

	actor := principal.Actor{ID: uuid.New(), Roles: []string{"ADMIN", "LECTURER"}}
	narrowed := policy.Narrow(actor, nil)

	if got := policy.RolesOf(narrowed).Sorted(); len(got) != 0 {
		t.Errorf("effective roles are %v, want none", got)
	}
	if !narrowed.Narrowed() {
		t.Error("narrowing to nothing did not mark the actor as narrowed")
	}
	if policy.MayAdministerPeople(narrowed) {
		t.Error("an administrator narrowed to nothing may still administer — the narrowing " +
			"does not reach the rules")
	}
}

// TestNarrowMarksEvenANoOpSelection.
//
// Selecting everything one holds changes no permission, and the marker is still set. "I asked
// to be narrowed and nothing changed" is a state to show honestly; comparing the two sets and
// silently dropping the marker would make the banner flicker on and off depending on which
// roles somebody was granted that morning.
func TestNarrowMarksEvenANoOpSelection(t *testing.T) {
	t.Parallel()

	actor := principal.Actor{ID: uuid.New(), Roles: []string{"LECTURER"}}
	narrowed := policy.Narrow(actor, []policy.Role{policy.RoleLecturer})

	if !narrowed.Narrowed() {
		t.Error("a selection that changed nothing was not marked as a narrowing")
	}
}

// TestParseRolesDropsWhatThePolicyDoesNotKnow, exactly as RolesOf does for stored grants: a
// role this package cannot interpret grants nothing, so not selecting it is the fail-closed
// reading. The caller learns nothing from the difference either — the intersection would have
// removed it regardless.
func TestParseRolesDropsWhatThePolicyDoesNotKnow(t *testing.T) {
	t.Parallel()

	got := policy.ParseRoles([]string{"LECTURER", "SUPERUSER", "", "admin"})
	if len(got) != 1 || got[0] != policy.RoleLecturer {
		t.Errorf("ParseRoles returned %v, want only LECTURER", got)
	}
}

// subset renders the bits of mask as a slice of role strings.
func subset(all []policy.Role, mask int) []string {
	out := make([]string, 0, len(all))
	for i, r := range all {
		if mask&(1<<i) != 0 {
			out = append(out, string(r))
		}
	}
	return out
}
