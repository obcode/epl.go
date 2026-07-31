// Package policy holds the rules about who may see and do what.
//
// It is pure: no database handle, no HTTP request, no GraphQL types. Everything it needs
// arrives as arguments — a principal.Actor, the state of a semester, the record in question —
// and everything it produces is an answer. That is what makes the rules testable as a table
// and renderable as a matrix, and it is why they can be enforced identically on both doors:
// there is nothing in here that knows a door exists.
//
// # Why the rules live here and not in the resolvers
//
// Because the dangerous call sites are the ones that do not exist yet. Wish confidentiality
// has to hold for the GraphQL API, for the Personal Access Token path that bypasses the GUI
// entirely, and for every CSV export, PDF and mail digest this system will grow. Filtering in
// N resolvers means N places to remember; one forgotten place is not a bug report here, it is
// the political failure the project exists to prevent.
//
// The layering test in internal/arch is the other half of that argument: no package outside
// internal/store may talk to the database, so every read necessarily passes the layer that
// applies these rules.
//
// # The two forms of every visibility rule
//
// Each rule appears twice, and the pair is deliberate:
//
//	CanSeeWish(actor, state, wish) bool          — the guard, for a record already in hand
//	WishVisibility(actor, state) WishFilter      — the filter, for the WHERE clause
//
// The guard reads like the sentence in CLAUDE.md and is what a reviewer checks the rule
// against. The filter is the same rule in a shape the database can apply, so that a list of
// ten thousand wishes is narrowed by an index rather than by ten thousand calls to the guard
// — and, crucially, so that COUNT is narrowed by exactly the same predicate as the rows.
//
// The realistic way this design fails is drift: somebody adjusts one form and not the other.
// TestGuardAndFilterAgree asserts equality over the whole cartesian product, which is small
// enough to enumerate and therefore not a sampling argument.
//
// # Language
//
// Everything here is English, identifiers and comments alike. The domain is German — the
// faculty says Fachgruppe, Deputat, Wunsch — and the translation happens once, in the GUI,
// rather than being scattered through the code as a half-German vocabulary that a newcomer
// has to learn before they can read a rule.
package policy
