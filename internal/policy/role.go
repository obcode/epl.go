package policy

import (
	"sort"

	"github.com/obcode/tallox.go/internal/principal"
)

// Role is a grant a person holds.
//
// The names are English, like every other identifier in this repository, and the GUI
// translates them for the people who read them. The mapping to the faculty's own vocabulary,
// once, so that nobody has to guess it twice:
//
//	LECTURER            Dozent:in
//	SUBJECT_GROUP_LEAD  Fachgruppenleitung
//	PROGRAMME_LEAD      Studiengangsleitung
//	DEANS_OFFICE        Dekanat
//	ADMIN               Administration
//
// That table is the only German in this package, and it is here so that nobody has to
// reconstruct the mapping from a GUI label.
type Role string

const (
	// RoleLecturer is the baseline: teaches, maintains a profile and a competence profile,
	// registers interest in instances. Everybody who appears in the planning has it.
	RoleLecturer Role = "LECTURER"

	// RoleSubjectGroupLead assigns the instances of one subject group.
	//
	// The grant is stored unscoped for now — which group it applies to becomes a question the
	// moment subject groups exist as rows, and the migration that creates them is where the
	// scoped grant belongs. Nothing in this file depends on the answer: the only rule shipped
	// so far, wish confidentiality, is faculty-wide by construction. Anything that does depend
	// on it must wait for the scoped form rather than approximate it with this one.
	RoleSubjectGroupLead Role = "SUBJECT_GROUP_LEAD"

	// RoleProgrammeLead declares the demand of one study programme: which instances have to be
	// offered, the compulsory and elective catalogues, the FWP wildcards. Unscoped for the same
	// reason as above.
	RoleProgrammeLead Role = "PROGRAMME_LEAD"

	// RoleDeansOffice reads across programmes for the import/export statistics and maintains
	// the teaching-load traffic light. Reads a great deal, writes almost nothing.
	RoleDeansOffice Role = "DEANS_OFFICE"

	// RoleAdmin administers users, roles and tokens.
	//
	// Deliberately NOT a reader of unpublished wishes — see mayReadUnpublishedWishes. Running
	// the system is a different job from planning with it, and the confidentiality rule is
	// worth more when the list of people it does not apply to is short and justifiable to the
	// colleagues it protects.
	RoleAdmin Role = "ADMIN"
)

// AllRoles returns every role this package knows, in a stable order.
//
// The order is "how much of the process the role touches", which is what makes the rendered
// visibility matrix readable. It carries no authority: roles are a set, not a ladder, and any
// rule that treated it as one would be wrong the first time somebody holds two of them.
func AllRoles() []Role {
	return []Role{
		RoleLecturer,
		RoleSubjectGroupLead,
		RoleProgrammeLead,
		RoleDeansOffice,
		RoleAdmin,
	}
}

// ParseRole turns a stored role string into a Role, reporting whether it is one this package
// knows.
//
// Unknown strings are not an error and not a panic: they are simply not a role. The database
// constrains the column to the list above, so a value outside it means the two lists have
// drifted — and the safe reading of "the policy does not know what this grant means" is that
// it grants nothing. TestDatabaseAndPolicyAgreeOnRoles in internal/store turns that drift
// into a red test rather than a silent loss of permissions.
func ParseRole(s string) (Role, bool) {
	for _, r := range AllRoles() {
		if string(r) == s {
			return r, true
		}
	}
	return "", false
}

// RoleSet is the set of roles one actor holds. A person can hold several — the same colleague
// may lead a subject group and a study programme — so every rule below asks about the set,
// never about "the" role.
type RoleSet map[Role]bool

// RolesOf reads the grants off an actor, dropping anything this package does not recognise.
//
// This is the one place where the opaque role strings on a principal.Actor acquire meaning,
// which is what keeps internal/principal free of the rules and the rules free of the
// transport.
func RolesOf(a principal.Actor) RoleSet {
	set := make(RoleSet, len(a.Roles))
	for _, raw := range a.Roles {
		if r, ok := ParseRole(raw); ok {
			set[r] = true
		}
	}
	return set
}

// Has reports whether the set contains the role.
func (rs RoleSet) Has(r Role) bool { return rs[r] }

// Any reports whether the set contains at least one of the roles.
func (rs RoleSet) Any(roles ...Role) bool {
	for _, r := range roles {
		if rs[r] {
			return true
		}
	}
	return false
}

// Plans reports whether these roles run the planning process — the people who have to see
// what is on the table before it is public, because their job is to fill the gaps.
func (rs RoleSet) Plans() bool {
	return rs.Any(RoleSubjectGroupLead, RoleProgrammeLead)
}

// Sorted returns the roles in AllRoles order, for rendering and for stable test output.
func (rs RoleSet) Sorted() []Role {
	order := make(map[Role]int, len(AllRoles()))
	for i, r := range AllRoles() {
		order[r] = i
	}

	out := make([]Role, 0, len(rs))
	for r, ok := range rs {
		if ok {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })
	return out
}
