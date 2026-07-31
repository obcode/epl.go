package principal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/obcode/tallox.go/internal/principal"
)

// TestContextWithoutAnActorIsAnonymous pins the fail-closed default.
//
// Every rule in this system reads the actor out of the context. The question this test
// answers is what happens on a code path where nobody put one in — a handler mounted outside
// the auth middleware, a background job, a test that forgot the setup. The answer has to be
// "somebody with no identity and no roles", because the alternative failure mode is a rule
// that reads a zero value and treats missing roles as unrestricted.
func TestContextWithoutAnActorIsAnonymous(t *testing.T) {
	t.Parallel()

	got := principal.From(context.Background())

	if got.Authenticated() {
		t.Error("an actor read from an empty context reports as authenticated")
	}
	if len(got.Roles) != 0 || len(got.Scopes) != 0 {
		t.Errorf("an actor read from an empty context carries roles %v and scopes %v",
			got.Roles, got.Scopes)
	}
	if got.Kind != principal.KindNone {
		t.Errorf("kind is %q, want the zero Kind", got.Kind)
	}

	if _, ok := principal.Lookup(context.Background()); ok {
		t.Error("Lookup reported that the middleware ran on a context it never touched")
	}
}

// TestRoundTrip covers the ordinary case, and that Lookup can tell the two situations apart.
func TestRoundTrip(t *testing.T) {
	t.Parallel()

	want := principal.Actor{
		ID:      uuid.New(),
		Mail:    "prof.eins@example.org",
		Name:    "Prof. Eins",
		Roles:   []string{"DOZENT"},
		Kind:    principal.KindInteractive,
		Scopes:  []string{"wishes:read"},
		TokenID: "",
	}

	ctx := principal.NewContext(context.Background(), want)

	got, ok := principal.Lookup(ctx)
	if !ok {
		t.Fatal("Lookup did not find the actor that was just put in")
	}
	if got.ID != want.ID || got.Mail != want.Mail || got.Kind != want.Kind {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !got.Authenticated() {
		t.Error("an actor with an ID does not report as authenticated")
	}
}

// TestAnonymousIsNeverAnOwner is a security test, not a null check.
//
// Ownership is the exception that lets somebody see their own unpublished wish. An
// unauthenticated caller has the nil UUID, and so does any row whose owner column was never
// filled in — so the obvious implementation, a.ID == ownerID, answers "yes, that is yours" to
// a caller who is nobody, about a record that belongs to nobody. That is the confidentiality
// rule failing in the direction that leaks, and it fails silently.
func TestAnonymousIsNeverAnOwner(t *testing.T) {
	t.Parallel()

	someone := uuid.New()

	for _, tc := range []struct {
		name  string
		actor principal.Actor
		owner uuid.UUID
		want  bool
	}{
		{
			name:  "anonymous actor, unset owner",
			actor: principal.Anonymous,
			owner: uuid.Nil,
			want:  false,
		},
		{
			name:  "anonymous actor, real owner",
			actor: principal.Anonymous,
			owner: someone,
			want:  false,
		},
		{
			name:  "real actor, unset owner",
			actor: principal.Actor{ID: someone},
			owner: uuid.Nil,
			want:  false,
		},
		{
			name:  "real actor, own record",
			actor: principal.Actor{ID: someone},
			owner: someone,
			want:  true,
		},
		{
			name:  "real actor, somebody else's record",
			actor: principal.Actor{ID: someone},
			owner: uuid.New(),
			want:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.actor.Owns(tc.owner); got != tc.want {
				t.Errorf("Owns() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestInteractiveDistinguishesTheDoors covers the one place the two doors are allowed to
// differ. The rules that use it are the @interactiveOnly ones; if Kind stopped being set,
// every one of them would silently start permitting the token path.
func TestInteractiveDistinguishesTheDoors(t *testing.T) {
	t.Parallel()

	browser := principal.Actor{ID: uuid.New(), Kind: principal.KindInteractive}
	token := principal.Actor{ID: uuid.New(), Kind: principal.KindToken, TokenID: "AAAAAAAAAAAAAAAA"}

	if !browser.Interactive() {
		t.Error("an interactive actor does not report as interactive")
	}
	if token.Interactive() {
		t.Error("a token actor reports as interactive — every @interactiveOnly rule would " +
			"start answering on the token path")
	}
	if principal.Anonymous.Interactive() {
		t.Error("the anonymous actor reports as interactive")
	}
}

// TestStringNeverCarriesMoreThanItShould keeps log lines useful and harmless: the public
// token ID belongs there (it is the first question when a script misbehaves), a secret never
// could — this type does not hold one — and an anonymous caller has to render as something
// other than an empty string.
func TestStringNeverCarriesMoreThanItShould(t *testing.T) {
	t.Parallel()

	if got := principal.Anonymous.String(); got != "anonymous" {
		t.Errorf("anonymous actor renders as %q", got)
	}

	token := principal.Actor{
		ID:      uuid.New(),
		Mail:    "prof.eins@example.org",
		Kind:    principal.KindToken,
		TokenID: "AAAAAAAAAAAAAAAA",
	}
	got := token.String()
	if !strings.Contains(got, "prof.eins@example.org") || !strings.Contains(got, "AAAAAAAAAAAAAAAA") {
		t.Errorf("token actor renders as %q, which does not say who or which token", got)
	}
}
