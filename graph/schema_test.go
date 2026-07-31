package graph_test

import (
	"testing"

	"github.com/obcode/tallox.go/graph"
	"github.com/obcode/tallox.go/graph/generated"
	"github.com/obcode/tallox.go/internal/policy"
)

// TestSchemaAndPolicyAgreeOnRoles is the third side of a triangle.
//
// The list of roles exists in three places that cannot import one another: the GraphQL schema,
// internal/policy, and the CHECK constraint in db/migrations. Two of the three are compared
// here; the database and the policy are compared in internal/store.
//
// The drift is silent in both directions and both are unpleasant. A role in the schema that
// the policy does not know is a value the GUI can offer and nothing will honour. A role the
// policy knows that the schema lacks cannot be marshalled at all, so the field errors — for
// exactly the people who hold that grant, and only for them, which is the kind of bug that
// gets reported as "it works for me".
func TestSchemaAndPolicyAgreeOnRoles(t *testing.T) {
	t.Parallel()

	schema := generated.NewExecutableSchema(generated.Config{Resolvers: &graph.Resolver{}}).Schema()

	enum, ok := schema.Types["Role"]
	if !ok {
		t.Fatal("the schema has no Role enum — has it been renamed?")
	}

	inSchema := map[string]bool{}
	for _, value := range enum.EnumValues {
		inSchema[value.Name] = true
	}

	for _, role := range policy.AllRoles() {
		if !inSchema[string(role)] {
			t.Errorf("internal/policy knows the role %s and the schema does not — the field "+
				"errors for exactly the people who hold that grant", role)
		}
		delete(inSchema, string(role))
	}
	for leftover := range inSchema {
		t.Errorf("the schema offers the role %s, which internal/policy does not know — the "+
			"GUI can offer a grant that nothing will honour", leftover)
	}
}
