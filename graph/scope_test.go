package graph_test

import (
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/obcode/tallox.go/graph"
	"github.com/obcode/tallox.go/graph/generated"
	"github.com/obcode/tallox.go/internal/policy"
)

// rootTypes returns the operation roots the schema defines, with the verb each one implies.
//
// Subscription is included even though there is none: the transports are listed explicitly in
// bootstrap precisely so that a subscription cannot appear without somebody deciding to add
// it, and if that day comes, the scope rules should already apply rather than be remembered.
func rootTypes(schema *ast.Schema) map[*ast.Definition]policy.ScopeVerb {
	roots := map[*ast.Definition]policy.ScopeVerb{}
	if schema.Query != nil {
		roots[schema.Query] = policy.ScopeVerbRead
	}
	if schema.Mutation != nil {
		roots[schema.Mutation] = policy.ScopeVerbWrite
	}
	if schema.Subscription != nil {
		roots[schema.Subscription] = policy.ScopeVerbWrite
	}
	return roots
}

// TestEveryRootFieldDeclaresAScope is the test the whole mechanism is for.
//
// The runtime already fails closed: a root field with no annotation is taken to require
// policy.ScopeFallback, which no ordinary token holds. That is the safety net, and a safety
// net is a bad place to land — the field would be unreachable through the API for as long as
// it took somebody to notice, and "unreachable through the API" is a bug report from a
// colleague rather than a failing build.
//
// So the annotation is compulsory, and this is what makes it compulsory. The value is almost
// entirely in the future: the demand planning, the wish table and the statistics are dozens of
// root fields that do not exist yet, and every one of them will be written by somebody who is
// thinking about the domain and not about scopes. This test is what asks them.
func TestEveryRootFieldDeclaresAScope(t *testing.T) {
	t.Parallel()

	schema := generated.NewExecutableSchema(generated.Config{Resolvers: &graph.Resolver{}}).Schema()

	for root := range rootTypes(schema) {
		for _, field := range root.Fields {
			// __schema, __type, __typename. Introspection is deliberately on, in production
			// too, and it is not ours to annotate.
			if strings.HasPrefix(field.Name, "__") {
				continue
			}

			where := root.Name + "." + field.Name

			directive := field.Directives.ForName("scope")
			if directive == nil {
				t.Errorf("%s has no @scope. Every root field needs one — add "+
					"@scope(area: …, verb: …) in the schema.\n"+
					"Until then the field requires %s and answers nothing through a scoped "+
					"token.", where, policy.ScopeFallback)
				continue
			}

			area := directive.Arguments.ForName("area")
			verb := directive.Arguments.ForName("verb")
			if area == nil || verb == nil {
				t.Errorf("%s has an @scope without both arguments", where)
				continue
			}

			scope := policy.Scope{
				Area: policy.ScopeArea(area.Value.Raw),
				Verb: policy.ScopeVerb(verb.Value.Raw),
			}
			if !scope.Valid() {
				t.Errorf("%s declares @scope(area: %s, verb: %s), which internal/policy does "+
					"not know", where, area.Value.Raw, verb.Value.Raw)
			}
		}
	}
}

// TestScopeVerbMatchesTheOperationType keeps the annotation honest about what the field does.
//
// A mutation annotated READ is not a security hole — graph/scope.go sets the verb from the
// operation type and ignores the annotation when they disagree, which is the sibling project's
// bug fixed at the root. It is a documentation hole, and a worse one than it looks: the
// annotation is what a colleague reads to decide which scopes their token needs, so a mutation
// that claims READ sends them to mint a token that cannot call it.
func TestScopeVerbMatchesTheOperationType(t *testing.T) {
	t.Parallel()

	schema := generated.NewExecutableSchema(generated.Config{Resolvers: &graph.Resolver{}}).Schema()

	for root, expected := range rootTypes(schema) {
		for _, field := range root.Fields {
			if strings.HasPrefix(field.Name, "__") {
				continue
			}

			directive := field.Directives.ForName("scope")
			if directive == nil {
				continue // TestEveryRootFieldDeclaresAScope reports this one.
			}
			verb := directive.Arguments.ForName("verb")
			if verb == nil {
				continue
			}

			if policy.ScopeVerb(verb.Value.Raw) != expected {
				t.Errorf("%s.%s declares verb %s, but every field on %s is a %s",
					root.Name, field.Name, verb.Value.Raw, root.Name, expected)
			}
		}
	}
}

// TestSchemaAndPolicyAgreeOnScopes is the same triangle as the roles one, with a shorter third
// side: there is no CHECK constraint here, because a scope is a string in a text[] column on
// purpose — an older binary has to survive reading a scope a newer one wrote.
//
// So there are two places, and gqlgen.yml binds the enums to the policy types, which means
// they cannot disagree about the Go type. They can still disagree about the *values*: adding a
// constant in internal/policy without adding the enum value is a scope that can be stored,
// checked and never named in the schema — invisible to introspection and therefore to the
// colleagues who have to know it exists.
func TestSchemaAndPolicyAgreeOnScopes(t *testing.T) {
	t.Parallel()

	schema := generated.NewExecutableSchema(generated.Config{Resolvers: &graph.Resolver{}}).Schema()

	t.Run("areas", func(t *testing.T) {
		t.Parallel()

		known := make([]string, 0, len(policy.AllScopeAreas()))
		for _, area := range policy.AllScopeAreas() {
			known = append(known, string(area))
		}
		assertEnumMatches(t, schema, "ScopeArea", known)
	})

	t.Run("verbs", func(t *testing.T) {
		t.Parallel()

		known := make([]string, 0, len(policy.AllScopeVerbs()))
		for _, verb := range policy.AllScopeVerbs() {
			known = append(known, string(verb))
		}
		assertEnumMatches(t, schema, "ScopeVerb", known)
	})
}

func assertEnumMatches(t *testing.T, schema *ast.Schema, name string, known []string) {
	t.Helper()

	enum, ok := schema.Types[name]
	if !ok {
		t.Fatalf("the schema has no %s enum — has it been renamed?", name)
	}

	inSchema := map[string]bool{}
	for _, value := range enum.EnumValues {
		inSchema[value.Name] = true
	}

	for _, value := range known {
		if !inSchema[value] {
			t.Errorf("internal/policy knows %s.%s and the schema does not — a scope nothing "+
				"can discover", name, value)
		}
		delete(inSchema, value)
	}
	for leftover := range inSchema {
		t.Errorf("the schema offers %s.%s, which internal/policy does not know — "+
			"policy.ParseScope will discard it", name, leftover)
	}
}
