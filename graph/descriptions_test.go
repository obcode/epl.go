package graph_test

import (
	"strings"
	"testing"

	"github.com/obcode/tallox.go/graph"
	"github.com/obcode/tallox.go/graph/generated"
)

// TestDescriptionsAreWrittenForTheirReaders guards a boundary that only became one when the
// GUI started rendering the schema.
//
// A description is documentation now: it appears in a colleague's editor completion and on
// the reference page in tallox.gui. Notes to ourselves — what still has to be built, why a
// decision went the way it did, which CI test will catch something — belong in `#` comments,
// which introspection does not report and which therefore stay in this repository.
//
// The failure this prevents is not a crash. It is a paragraph beginning "NOTE: once the
// @scope directive lands" appearing in the documentation of somebody who is trying to write
// an evaluation, where it reads as an unfinished product.
func TestDescriptionsAreWrittenForTheirReaders(t *testing.T) {
	t.Parallel()

	// Markers of a note to ourselves. Deliberately short: a longer list would start matching
	// legitimate prose, and this is a smell test rather than a grammar.
	forbidden := []string{
		"NOTE:",
		"TODO",
		"FIXME",
		"CI test",
		"fail-closed",
		"resolver",
		"internal/",
	}

	schema := generated.NewExecutableSchema(generated.Config{Resolvers: &graph.Resolver{}}).Schema()

	check := func(t *testing.T, where, description string) {
		t.Helper()

		for _, marker := range forbidden {
			if strings.Contains(description, marker) {
				t.Errorf("the description of %s contains %q — that is a note to ourselves, and "+
					"it is rendered in the GUI's schema reference and in editor completion.\n"+
					"Move it to a `#` comment: introspection does not report those.\n\n%s",
					where, marker, description)
			}
		}
	}

	for _, def := range schema.Types {
		if strings.HasPrefix(def.Name, "__") {
			continue
		}
		check(t, def.Name, def.Description)

		for _, field := range def.Fields {
			if strings.HasPrefix(field.Name, "__") {
				continue
			}
			check(t, def.Name+"."+field.Name, field.Description)

			for _, arg := range field.Arguments {
				check(t, def.Name+"."+field.Name+"("+arg.Name+")", arg.Description)
			}
		}
	}

	for _, directive := range schema.Directives {
		check(t, "@"+directive.Name, directive.Description)
	}
}

// TestEveryFieldIsDescribed: a field with no description is a row in the reference page that
// explains nothing, and a completion entry that helps nobody.
func TestEveryFieldIsDescribed(t *testing.T) {
	t.Parallel()

	schema := generated.NewExecutableSchema(generated.Config{Resolvers: &graph.Resolver{}}).Schema()

	// The obvious ones. `id` and `name` on a Person do not need a sentence, and requiring one
	// produces filler, which is worse than nothing.
	obvious := map[string]bool{"id": true, "name": true, "createdAt": true}

	for _, def := range schema.Types {
		if strings.HasPrefix(def.Name, "__") || def.BuiltIn {
			continue
		}
		if def.Description == "" {
			t.Errorf("the type %s has no description", def.Name)
		}
		for _, field := range def.Fields {
			if strings.HasPrefix(field.Name, "__") || obvious[field.Name] {
				continue
			}
			if field.Description == "" {
				t.Errorf("%s.%s has no description", def.Name, field.Name)
			}
		}
	}
}
