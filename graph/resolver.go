// Package graph is the GraphQL layer: schema, generated execution code and resolvers.
//
// Resolvers stay thin. They translate between the wire format and internal/domain and do
// nothing else — no database access (the architecture test in internal/arch enforces that),
// no authorization decisions (those belong in internal/policy, so that the token path
// cannot differ from the browser path).
package graph

import (
	"github.com/obcode/tallox.go/internal/buildinfo"
	"github.com/obcode/tallox.go/internal/domain"
)

//go:generate go tool gqlgen generate

// Resolver is the dependency root of the GraphQL layer. Everything a resolver needs is a
// field here, injected once in bootstrap — there is no package-level state and no service
// locator, so a test can construct a Resolver with exactly the seams it wants to observe.
type Resolver struct {
	Build buildinfo.Info
	// Tokens is token management. Nil in the handful of tests that only ask for buildInfo —
	// a resolver that panics on a field nobody exercised is louder than a fake that answers.
	Tokens *domain.TokenService
	// People is user administration, on the same terms as Tokens.
	People *domain.PeopleService
}
