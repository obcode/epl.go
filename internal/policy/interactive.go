package policy

import "github.com/obcode/tallox.go/internal/principal"

// MayReadInteractiveOnly reports whether an actor may read a field marked @interactiveOnly.
//
// The third factor of the invariant, on its own:
//
//	effective permission = (what the Role allows) ∩ (what the Scopes grant) ∩ (what the Kind allows)
//
// It is deliberately not "is this a token" spelled inline at the directive. The question is a
// rule, rules live here, and putting it here means the answer is the same for the GraphQL
// directive, for a future CSV export and for anything else that grows a reason to ask.
//
// Anonymous callers fail it too, and that is the correct reading rather than an accident of
// the implementation: an unauthenticated request has no audited session behind it either, so
// none of the arguments for the interactive path apply to it.
//
// What sits behind it: the Deputat/overtime traffic light and everything else that counts as
// personnel data, other people's unpublished wishes, free-text notes about people, the audit
// log, and token management itself — a leaked token that can mint its successors outlives its
// own expiry date.
func MayReadInteractiveOnly(a principal.Actor) bool {
	return a.Authenticated() && a.Interactive()
}

// InteractiveOnlyReason is what a caller who failed that check is told.
//
// German, like every other string a person reads, and specific enough to act on: the useful
// next step is a browser, not a retry. It deliberately does not name the field — the schema
// already documents which fields are interactive-only, and a message that enumerates them
// would be a list to keep in sync.
const InteractiveOnlyReason = "Dieses Feld ist über ein Personal Access Token nicht verfügbar. " +
	"Bitte im Browser aufrufen."
