package policy

import (
	"time"

	"github.com/google/uuid"

	"github.com/obcode/tallox.go/internal/principal"
)

// SemesterState is the part of a semester the read rules depend on.
//
// A struct rather than two arguments, because it is about to grow (milestones, the point from
// which the requested SWS stop being editable) and because "which of these two timestamps did
// I pass first" is a bug that compiles.
type SemesterState struct {
	// Phase is where the semester stands. Deliberately unused by wish visibility — see
	// TestVisibilityDoesNotDependOnThePhase — and carried here anyway, because every rule
	// that follows this one is a write rule and every write rule needs it.
	Phase Phase
	// WishesPublishedAt is the moment the wishes of this semester became public, or the zero
	// time if that has not happened. Mirrors semester.wishes_published_at, where the rule is
	// IS NOT NULL.
	//
	// A zero time.Time rather than a *time.Time: the nil pointer would be one more thing every
	// call site could dereference by accident, and there is no meaningful difference here
	// between "never published" and "no value".
	WishesPublishedAt time.Time
}

// WishesPublished reports whether the confidentiality window has closed.
func (s SemesterState) WishesPublished() bool { return !s.WishesPublishedAt.IsZero() }

// Wish is the minimum of a wish that the visibility rule needs: who entered it.
//
// Nothing else — not the instance part, not the priority, not the SWS. Keeping it minimal is
// what lets internal/store apply the rule to a row it has not fully loaded, and what keeps
// this package from acquiring a second copy of the domain model.
type Wish struct {
	// OwnerID is the person who registered the interest.
	OwnerID uuid.UUID
}

// WishScope is how much of the wish table a caller may read.
type WishScope string

const (
	// WishScopeNone grants nothing at all. The anonymous caller.
	WishScopeNone WishScope = "none"
	// WishScopeOwn grants only the caller's own entries. The ordinary case during the
	// confidentiality window — and the one that makes aggregates safe, because a COUNT
	// narrowed this way returns the caller's own number and not the faculty's.
	WishScopeOwn WishScope = "own"
	// WishScopeAll grants everything: no restriction.
	WishScopeAll WishScope = "all"
)

// WishFilter is the visibility rule in the shape a query can apply.
//
// internal/store translates it, and the translation is the whole point of the type:
//
//	WishScopeNone → WHERE false
//	WishScopeOwn  → WHERE owner_id = @ownerID
//	WishScopeAll  → no additional predicate
//
// The rule that makes this worth a type rather than a bool: **the same filter goes on the
// COUNT**. "3 Kolleg:innen haben bereits Interesse" leaks the confidential information
// completely without naming anybody, so an aggregate that skips the filter is the same
// failure as a list that skips it, only harder to notice. A filter that has to be passed
// explicitly is one that shows up as missing in review.
type WishFilter struct {
	// Scope is how much may be read.
	Scope WishScope
	// OwnerID is the person the scope is restricted to. Meaningful only for WishScopeOwn.
	OwnerID uuid.UUID
}

// Matches reports whether a single wish passes this filter.
//
// The counterpart of the WHERE clause, and the reason the property test can compare the two
// forms of the rule at all. The nil check on WishScopeOwn is the same guard as
// principal.Actor.Owns: an unset owner column must not match an unset filter owner.
func (f WishFilter) Matches(w Wish) bool {
	switch f.Scope {
	case WishScopeAll:
		return true
	case WishScopeOwn:
		return f.OwnerID != uuid.Nil && w.OwnerID == f.OwnerID
	case WishScopeNone:
		return false
	default:
		// An unknown scope is a programming error, and the safe reading of "I do not know how
		// much you may see" is "nothing".
		return false
	}
}

// CanSeeWish reports whether the actor may see this wish. The guard form of the rule.
//
// Reads as the sentence it implements: visible iff owner, or the wishes of the semester have
// been published, or the caller is one of the people the process requires to look early — and
// then only in an interactive session.
//
// The Kind condition is the least obvious clause and the one most likely to be dropped by
// somebody tidying up. It is there because a Personal Access Token is a different risk class
// from a browser session: it is long-lived, it sits in a script, and it makes silent bulk
// export possible while decoupling "who saw this" from any login event. A planner reading
// unpublished wishes in the GUI leaves a session behind; the same person's token in a cron
// job does not. Their own wishes stay readable through both doors — that is their data.
func CanSeeWish(a principal.Actor, s SemesterState, w Wish) bool {
	switch {
	case !a.Authenticated():
		return false
	case a.Owns(w.OwnerID):
		return true
	case s.WishesPublished():
		return true
	default:
		return a.Interactive() && mayReadUnpublishedWishes(RolesOf(a))
	}
}

// WishVisibility returns the same rule as a query filter. The filter form.
//
// Deliberately a second implementation rather than a call to CanSeeWish over every row: this
// one has to survive the trip into SQL, where "call the guard for each candidate" is not
// available and would be a table scan if it were. TestGuardAndFilterAgree is the bridge
// between the two, over the complete cartesian product.
func WishVisibility(a principal.Actor, s SemesterState) WishFilter {
	switch {
	case !a.Authenticated():
		return WishFilter{Scope: WishScopeNone}
	case s.WishesPublished():
		return WishFilter{Scope: WishScopeAll}
	case a.Interactive() && mayReadUnpublishedWishes(RolesOf(a)):
		return WishFilter{Scope: WishScopeAll}
	default:
		return WishFilter{Scope: WishScopeOwn, OwnerID: a.ID}
	}
}

// mayReadUnpublishedWishes is the exception list, in one place so that it can be read as a
// list of people rather than reconstructed from conditions.
//
// Planning roles because the process requires it: somebody has to see what is on the table in
// order to fill the gaps. Dekanat because it evaluates across programmes.
//
// ADMIN is deliberately absent. Administering the system is a different job from planning
// with it, and an exception list that a colleague can read and accept is worth more than one
// that quietly includes whoever holds the keys. An admin who genuinely needs to look can be
// granted DEKANAT — visibly, as an audited role grant, rather than by virtue of being an
// admin. If the faculty decides otherwise at the retreat, this function and one line of the
// golden matrix are the whole change.
func mayReadUnpublishedWishes(rs RoleSet) bool {
	return rs.Plans() || rs.Has(RoleDekanat)
}
