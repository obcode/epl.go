package graph

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/obcode/tallox.go/graph/generated"

	"github.com/obcode/tallox.go/internal/policy"
	"github.com/obcode/tallox.go/internal/principal"
)

// Directives returns the directive implementations the generated schema needs.
//
// One place, wired once in bootstrap. gqlgen fails at construction time if an entry is
// missing, which is the property that makes a directive worth more than a helper function:
// declaring `@interactiveOnly` in the schema and forgetting to implement it does not compile
// into a server that quietly allows everything.
func Directives() generated.DirectiveRoot {
	return generated.DirectiveRoot{InteractiveOnly: interactiveOnly}
}

// interactiveOnly refuses a field to anything that is not an audited browser session.
//
// The decision itself is policy.MayReadInteractiveOnly; everything here is plumbing — reading
// the actor out of the context and choosing how to say no.
//
// # Why null on a nullable field
//
// Because the alternative punishes the wrong request. A colleague's evaluation script asking
// for a person and, in the same query, something interactive-only would get an error for the
// whole operation and no data at all. Answering null for the field it may not have keeps the
// rest of the response useful, and the schema documents the boundary by making the field
// visibly nullable.
//
// On a non-null field there is no such choice: null is not a legal value there, so a refusal
// has to be an error. Mutations are all non-null for exactly that reason — a write that
// silently does nothing is worse than one that says no.
func interactiveOnly(ctx context.Context, _ any, next graphql.Resolver) (any, error) {
	if policy.MayReadInteractiveOnly(principal.From(ctx)) {
		return next(ctx)
	}

	// The field's own type decides how the refusal can be expressed. GetFieldContext is nil
	// only outside field resolution, which cannot happen for a FIELD_DEFINITION directive —
	// but a nil dereference in an authorization path would fail open, so it is checked.
	if fc := graphql.GetFieldContext(ctx); fc != nil && !fc.Field.Definition.Type.NonNull {
		return nil, nil
	}
	return nil, &gqlerror.Error{
		Message:    policy.InteractiveOnlyReason,
		Extensions: map[string]any{"code": "INTERACTIVE_ONLY"},
	}
}
