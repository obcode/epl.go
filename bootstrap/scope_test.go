package bootstrap_test

import (
	"net/http"
	"testing"

	"github.com/obcode/tallox.go/bootstrap"
	"github.com/obcode/tallox.go/internal/auth"
	"github.com/obcode/tallox.go/internal/buildinfo"
	"github.com/obcode/tallox.go/internal/graphqltest"
	"github.com/obcode/tallox.go/internal/policy"
	"github.com/obcode/tallox.go/internal/store"
	"github.com/obcode/tallox.go/internal/store/storetest"
	"github.com/obcode/tallox.go/internal/testdata"
)

// meQuery is shared with auth_test.go — the same operation, so that a scope refusal and an
// authentication refusal are visibly about the same request.
const buildInfoQuery = `{ buildInfo { version } }`

// scopedHandler seeds one persona with a token carrying exactly the given scopes, and returns
// the handler Serve would build.
//
// Scopes are seeded rather than minted, because there is no way to choose them at creation
// yet. That gap is the reason this file exists: the enforcement is live and the only caller
// that can currently exercise it is a test, so if the test is missing, the mechanism is
// unverified until the day somebody depends on it.
func scopedHandler(t *testing.T, persona testdata.Persona, scopes []string) http.Handler {
	t.Helper()

	s := storetest.New(t)
	storetest.SeedPerson(t, s, persona, string(policy.RoleLecturer), string(policy.RoleAdmin))

	parsed, err := auth.ParseToken(persona.Token)
	if err != nil {
		t.Fatalf("fixture token of %s does not parse: %v", persona.Name, err)
	}
	storetest.SeedToken(t, s, persona, auth.HashSecret(parsed.Secret), storetest.TokenOptions{
		Description: "scope enforcement test",
		Scopes:      scopes,
	})

	directory := store.NewDirectory(s.Pool)

	return bootstrap.Handler(bootstrap.Options{
		Build: buildinfo.Info{Version: "test"},
		Auth:  auth.Config{Mode: auth.ModeProxy, Users: directory, Tokens: directory},
	})
}

// assertInsufficientScope checks that a refusal is the scope one, by code and not by prose.
func assertInsufficientScope(t *testing.T, resp graphqltest.Response, want policy.Scope) {
	t.Helper()

	if !resp.Failed() {
		t.Fatalf("expected a refusal for want=%s, got:\n%s", want, resp.Body)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("expected exactly one error, got %d:\n%s", len(resp.Errors), resp.Body)
	}

	got := resp.Errors[0]
	if code, _ := got.Extensions["code"].(string); code != "INSUFFICIENT_SCOPE" {
		t.Errorf("refusal code is %q, want INSUFFICIENT_SCOPE — the GUI and any script branch "+
			"on the code, never on the sentence.\n%s", code, resp.Body)
	}
	if scope, _ := got.Extensions["scope"].(string); scope != want.String() {
		t.Errorf("refusal names scope %q, want %q", scope, want)
	}
}

// TestAnUnscopedTokenIsNotNarrowed is the compatibility half, and the one worth stating
// loudest: an empty scope list means unrestricted, so shipping the enforcement did not break a
// single token that already existed.
//
// Through both doors, because "the same principal gets the same answer" is exactly what
// EachDoor is for — a session has no scopes at all and an unscoped token has an empty list,
// and neither is narrowed.
func TestAnUnscopedTokenIsNotNarrowed(t *testing.T) {
	t.Parallel()

	h := scopedHandler(t, testdata.Eins, nil)

	graphqltest.EachDoor(t, h, testdata.Eins.Mail, testdata.Eins.Token,
		func(t *testing.T, c *graphqltest.Client) {
			var out struct {
				Me *struct {
					Mail string `json:"mail"`
				} `json:"me"`
			}
			c.MustQuery(t, meQuery, nil, &out)

			if out.Me == nil || out.Me.Mail != testdata.Eins.Mail {
				t.Errorf("me = %+v, want %s", out.Me, testdata.Eins.Mail)
			}
		})
}

// TestABrowserSessionIsNeverScopeRefused asserts a place where the doors are *supposed* to
// differ, so it says so per door rather than quietly covering one.
//
// The interactive caller is the person, present and audited; there is no long-lived credential
// to narrow. Were this to regress, the symptom would be the GUI failing for everybody at once
// — loud, but only after a deploy.
func TestABrowserSessionIsNeverScopeRefused(t *testing.T) {
	t.Parallel()

	// The token this persona holds is scoped away from PROFILE. The browser door must not care.
	h := scopedHandler(t, testdata.Eins, []string{"PUBLIC:READ"})

	t.Run("browser", func(t *testing.T) {
		t.Parallel()

		c := graphqltest.New(h).AsUser(testdata.Eins.Mail)

		var out struct {
			Me *struct {
				Mail string `json:"mail"`
			} `json:"me"`
		}
		c.MustQuery(t, meQuery, nil, &out)

		if out.Me == nil || out.Me.Mail != testdata.Eins.Mail {
			t.Errorf("me = %+v, want %s — a session carries no scopes to be narrowed by",
				out.Me, testdata.Eins.Mail)
		}
	})

	t.Run("token", func(t *testing.T) {
		t.Parallel()

		resp := graphqltest.New(h).WithToken(testdata.Eins.Token).Do(t, meQuery, nil)
		assertInsufficientScope(t, resp,
			policy.Scope{Area: policy.ScopeAreaProfile, Verb: policy.ScopeVerbRead})
	})
}

func TestAScopedTokenReachesOnlyItsAreas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scopes  []string
		query   string
		allowed bool
		missing policy.Scope
	}{
		{
			name:    "the exact scope",
			scopes:  []string{"PROFILE:READ"},
			query:   meQuery,
			allowed: true,
		},
		{
			name: "the write scope of the same area, because write implies read",
			// Not a rule the GraphQL layer knows: policy.Scope.Grants decides it, and this is
			// the assertion that the decision actually reaches the door.
			scopes:  []string{"PROFILE:WRITE"},
			query:   meQuery,
			allowed: true,
		},
		{
			name:    "another area entirely",
			scopes:  []string{"ADMIN:WRITE"},
			query:   meQuery,
			missing: policy.Scope{Area: policy.ScopeAreaProfile, Verb: policy.ScopeVerbRead},
		},
		{
			name:    "one of several matches",
			scopes:  []string{"ADMIN:WRITE", "PROFILE:READ"},
			query:   meQuery,
			allowed: true,
		},
		{
			name:    "buildInfo stays reachable when the rest is refused",
			scopes:  []string{"PUBLIC:READ"},
			query:   buildInfoQuery,
			allowed: true,
		},
		{
			name: "a scope this build cannot parse is not a free pass",
			// The forward-compatibility rule from policy.ParseScope, at the door: an older
			// binary reading a scope a newer one wrote ignores it. Ignoring must mean "this
			// grants nothing", never "this grants everything".
			scopes:  []string{"WISHES:READ"},
			query:   meQuery,
			missing: policy.Scope{Area: policy.ScopeAreaProfile, Verb: policy.ScopeVerbRead},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := scopedHandler(t, testdata.Eins, tt.scopes)
			c := graphqltest.New(h).WithToken(testdata.Eins.Token)

			resp := c.Do(t, tt.query, nil)
			if tt.allowed {
				if resp.Failed() {
					t.Fatalf("scopes %v were expected to reach the field: %v",
						tt.scopes, resp.Messages())
				}
				return
			}
			assertInsufficientScope(t, resp, tt.missing)
		})
	}
}

// TestAnOperationIsRefusedWholeSale is the reason this is an operation middleware and not a
// field directive.
//
// A query asking for one permitted and one forbidden field executes neither. The alternative —
// answering buildInfo and erroring on me — would mean a credential that may not make the call
// has made part of it, and it would hand a caller a way to probe which areas a token covers
// while still getting useful data back.
func TestAnOperationIsRefusedWholeSale(t *testing.T) {
	t.Parallel()

	h := scopedHandler(t, testdata.Eins, []string{"PUBLIC:READ"})
	c := graphqltest.New(h).WithToken(testdata.Eins.Token)

	resp := c.Do(t, `{ buildInfo { version } me { mail } }`, nil)
	assertInsufficientScope(t, resp,
		policy.Scope{Area: policy.ScopeAreaProfile, Verb: policy.ScopeVerbRead})

	// buildInfo is permitted and must still not have run.
	var out struct {
		BuildInfo *struct {
			Version string `json:"version"`
		} `json:"buildInfo"`
	}
	if len(resp.Data) > 0 {
		resp.Decode(t, &out)
	}
	if out.BuildInfo != nil {
		t.Errorf("buildInfo answered %+v — the permitted half of a refused operation must not "+
			"execute", out.BuildInfo)
	}
}

// TestAFragmentDoesNotHideARootField is the sibling project's bug, asserted through the real
// handler rather than against the walk alone.
//
// There it is reachable: the root-field walk ignores fragments, so the operation looks empty,
// the write check falls through, and the mutation runs. Both halves of the defence are
// exercised here at once — the walk follows the fragment, and the verb comes from the
// operation type regardless.
func TestAFragmentDoesNotHideARootField(t *testing.T) {
	t.Parallel()

	h := scopedHandler(t, testdata.Eins, []string{"PROFILE:READ"})
	c := graphqltest.New(h).WithToken(testdata.Eins.Token)

	admin := policy.Scope{Area: policy.ScopeAreaAdmin, Verb: policy.ScopeVerbWrite}

	t.Run("inline fragment", func(t *testing.T) {
		t.Parallel()

		resp := c.Do(t, `mutation {
			... on Mutation { setPersonActive(id: "00000000-0000-0000-0000-000000000000", active: false) { id } }
		}`, nil)
		assertInsufficientScope(t, resp, admin)
	})

	t.Run("named fragment", func(t *testing.T) {
		t.Parallel()

		resp := c.Do(t, `mutation { ...f }
			fragment f on Mutation {
				setPersonActive(id: "00000000-0000-0000-0000-000000000000", active: false) { id }
			}`, nil)
		assertInsufficientScope(t, resp, admin)
	})
}

// TestTheScopeCheckRunsBeforeTheResolvers pins the ordering against @interactiveOnly.
//
// Both directives refuse this call and only one of them can answer first. The scope check wins
// because it is the cheaper and earlier of the two — nothing has resolved yet — and pinning it
// means a client that branches on the code sees a stable answer rather than one that depends
// on which refusal happens to be evaluated.
func TestTheScopeCheckRunsBeforeTheResolvers(t *testing.T) {
	t.Parallel()

	h := scopedHandler(t, testdata.Eins, []string{"PROFILE:READ"})
	c := graphqltest.New(h).WithToken(testdata.Eins.Token)

	resp := c.Do(t, `{ myTokens { id } }`, nil)
	assertInsufficientScope(t, resp,
		policy.Scope{Area: policy.ScopeAreaTokens, Verb: policy.ScopeVerbRead})
}

// TestIntrospectionNeedsNoScope protects a product decision, not a mechanism.
//
// Introspection is what makes editor completion, codegen and schema exploration work for the
// colleagues writing their own evaluations, and it is deliberately on in production. A scope
// check that swept it up would break that for every scoped token, and it would do so silently:
// the symptom is a code generator producing an empty client.
func TestIntrospectionNeedsNoScope(t *testing.T) {
	t.Parallel()

	h := scopedHandler(t, testdata.Eins, []string{"PUBLIC:READ"})
	c := graphqltest.New(h).WithToken(testdata.Eins.Token)

	var out struct {
		Schema struct {
			QueryType struct {
				Name string `json:"name"`
			} `json:"queryType"`
		} `json:"__schema"`
	}
	c.MustQuery(t, `{ __schema { queryType { name } } }`, nil, &out)

	if out.Schema.QueryType.Name != "Query" {
		t.Errorf("__schema.queryType.name = %q, want Query", out.Schema.QueryType.Name)
	}
}

// TestAnAnonymousCallerIsNotScopeRefused keeps the two refusals apart.
//
// Anonymous means no credential, so there is no scope list and nothing to narrow: buildInfo has
// to answer, because the GUI footer renders before a session exists and the deploy smoke check
// depends on it. Whatever else an anonymous caller is refused, it must not be refused *here* —
// a scope error for somebody who holds no token is a misleading diagnosis.
func TestAnAnonymousCallerIsNotScopeRefused(t *testing.T) {
	t.Parallel()

	h := scopedHandler(t, testdata.Eins, []string{"PUBLIC:READ"})

	for _, door := range []graphqltest.Door{graphqltest.Browser, graphqltest.Token} {
		t.Run(door.Name, func(t *testing.T) {
			t.Parallel()

			c := graphqltest.New(h).On(door).Anonymous()

			var out struct {
				BuildInfo struct {
					Version string `json:"version"`
				} `json:"buildInfo"`
			}
			c.MustQuery(t, buildInfoQuery, nil, &out)

			if out.BuildInfo.Version != "test" {
				t.Errorf("buildInfo.version = %q, want test", out.BuildInfo.Version)
			}
		})
	}
}
