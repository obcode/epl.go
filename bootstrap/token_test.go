package bootstrap_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/obcode/tallox.go/bootstrap"
	"github.com/obcode/tallox.go/internal/auth"
	"github.com/obcode/tallox.go/internal/buildinfo"
	"github.com/obcode/tallox.go/internal/domain"
	"github.com/obcode/tallox.go/internal/graphqltest"
	"github.com/obcode/tallox.go/internal/policy"
	"github.com/obcode/tallox.go/internal/store"
	"github.com/obcode/tallox.go/internal/store/storetest"
	"github.com/obcode/tallox.go/internal/testdata"
)

const (
	createMutation = `mutation ($d: String!, $days: Int) {
		createPersonalAccessToken(description: $d, expiresInDays: $days) {
			secret
			token { id description expiresAt revokedAt lastUsedAt }
		}
	}`
	myTokensQuery  = `{ myTokens { id description revokedAt } }`
	revokeMutation = `mutation ($id: ID!) { revokePersonalAccessToken(id: $id) { id revokedAt } }`
)

type createdTokenResponse struct {
	CreatePersonalAccessToken struct {
		Secret string `json:"secret"`
		Token  struct {
			ID          string  `json:"id"`
			Description string  `json:"description"`
			ExpiresAt   string  `json:"expiresAt"`
			RevokedAt   *string `json:"revokedAt"`
			LastUsedAt  *string `json:"lastUsedAt"`
		} `json:"token"`
	} `json:"createPersonalAccessToken"`
}

type myTokensResponse struct {
	MyTokens *[]struct {
		ID          string  `json:"id"`
		Description string  `json:"description"`
		RevokedAt   *string `json:"revokedAt"`
	} `json:"myTokens"`
}

// tokenHandler returns a handler on a fresh schema with the given personas seeded, and the
// token service wired the way Serve wires it.
func tokenHandler(t *testing.T, people ...testdata.Persona) http.Handler {
	t.Helper()

	s := storetest.New(t)
	for _, p := range people {
		storetest.SeedPerson(t, s, p, string(policy.RoleLecturer))
	}

	directory := store.NewDirectory(s.Pool)

	return bootstrap.Handler(bootstrap.Options{
		Build:  buildinfo.Info{Version: "test"},
		Auth:   auth.Config{Mode: auth.ModeProxy, Users: directory, Tokens: directory},
		Tokens: domain.NewTokenService(store.NewTokens(s.Pool), nil),
	})
}

// TestATokenCreatedInTheBrowserWorksOnTheTokenDoor is the whole feature in one test, and the
// only one that proves the pieces fit: mint through the browser door, then authenticate with
// the result through the other door.
//
// Everything in between is covered elsewhere — the format in internal/auth, the arithmetic in
// internal/domain, the queries in internal/store — and all of it can be individually correct
// while the product is unusable, because the one thing none of those tests can see is whether
// the secret handed to a person is the secret the server will accept back.
func TestATokenCreatedInTheBrowserWorksOnTheTokenDoor(t *testing.T) {
	t.Parallel()

	h := tokenHandler(t, testdata.Eins)
	browser := graphqltest.New(h).AsUser(testdata.Eins.Mail)

	var created createdTokenResponse
	browser.MustQuery(t, createMutation, map[string]any{"d": "Auswertung Lehrdeputat"}, &created)

	secret := created.CreatePersonalAccessToken.Secret
	if secret == "" {
		t.Fatal("no secret came back — it is returned exactly once, so there is no second chance")
	}
	if created.CreatePersonalAccessToken.Token.RevokedAt != nil {
		t.Error("a fresh token is already revoked")
	}
	if created.CreatePersonalAccessToken.Token.LastUsedAt != nil {
		t.Error("a token that has never been used reports a last use")
	}

	// The secret is a whole token, and it identifies its owner through the other door.
	var me meResponse
	graphqltest.New(h).WithToken(secret).MustQuery(t, meQuery, nil, &me)
	if me.Me == nil {
		t.Fatal("the token the server just issued does not authenticate against it")
	}
	if me.Me.Mail != testdata.Eins.Mail {
		t.Errorf("the token authenticates as %s, want its owner %s", me.Me.Mail, testdata.Eins.Mail)
	}
}

// TestTokenManagementIsNotReachableWithAToken is the @interactiveOnly rule, both of its
// answers, through the door it exists to close.
//
// A leaked token that can enumerate and mint its successors outlives its own expiry date, and
// revoking the one you know about achieves nothing. That is the failure this closes, and it
// is worth the awkwardness of a nullable list.
func TestTokenManagementIsNotReachableWithAToken(t *testing.T) {
	t.Parallel()

	h := tokenHandler(t, testdata.Eins)

	// A real, working token — so the refusal below is about the field, not the credential.
	var created createdTokenResponse
	graphqltest.New(h).AsUser(testdata.Eins.Mail).
		MustQuery(t, createMutation, map[string]any{"d": "Auswertung"}, &created)
	tokenClient := graphqltest.New(h).WithToken(created.CreatePersonalAccessToken.Secret)

	t.Run("the nullable list answers null instead of failing", func(t *testing.T) {
		var got myTokensResponse
		// MustQuery: the operation has to *succeed*. A script asking for this alongside
		// something else keeps the something else — that is the point of the nullable field.
		tokenClient.MustQuery(t, myTokensQuery, nil, &got)

		if got.MyTokens != nil {
			t.Errorf("myTokens answered %v through the token door", *got.MyTokens)
		}
	})

	t.Run("the mutation refuses outright", func(t *testing.T) {
		messages := tokenClient.MustFail(t, createMutation, map[string]any{"d": "Zweitschlüssel"})

		joined := strings.Join(messages, " ")
		if !strings.Contains(joined, "Personal Access Token") {
			t.Errorf("the refusal does not say why: %v", messages)
		}
		// A write that silently does nothing is worse than one that says no, so this may not
		// degrade to null the way the query does.
		if !strings.Contains(joined, "Browser") {
			t.Errorf("the refusal does not say what to do instead: %v", messages)
		}
	})

	t.Run("and revoking is closed too", func(t *testing.T) {
		tokenClient.MustFail(t, revokeMutation,
			map[string]any{"id": created.CreatePersonalAccessToken.Token.ID})
	})

	// The same caller, through the browser door, still sees everything.
	var mine myTokensResponse
	graphqltest.New(h).AsUser(testdata.Eins.Mail).MustQuery(t, myTokensQuery, nil, &mine)
	if mine.MyTokens == nil || len(*mine.MyTokens) != 1 {
		t.Fatalf("the browser door sees %v", mine.MyTokens)
	}
}

// TestNobodySeesOrRevokesSomebodyElsesTokens covers the confidentiality half: a token list is
// a list of credentials somebody holds, and the id in it is what a revocation takes.
func TestNobodySeesOrRevokesSomebodyElsesTokens(t *testing.T) {
	t.Parallel()

	h := tokenHandler(t, testdata.Eins, testdata.Zwei)

	var created createdTokenResponse
	graphqltest.New(h).AsUser(testdata.Eins.Mail).
		MustQuery(t, createMutation, map[string]any{"d": "Eins' Auswertung"}, &created)
	id := created.CreatePersonalAccessToken.Token.ID

	colleague := graphqltest.New(h).AsUser(testdata.Zwei.Mail)

	var theirs myTokensResponse
	colleague.MustQuery(t, myTokensQuery, nil, &theirs)
	if theirs.MyTokens == nil || len(*theirs.MyTokens) != 0 {
		t.Errorf("a colleague sees %v", theirs.MyTokens)
	}

	messages := colleague.MustFail(t, revokeMutation, map[string]any{"id": id})

	// The same message a token id that exists nowhere produces. Anything more specific turns
	// the mutation into an oracle for which ids exist.
	if !strings.Contains(strings.Join(messages, " "), "existiert nicht") {
		t.Errorf("revoking somebody else's token said %v — it has to be the same refusal as "+
			"a token that does not exist", messages)
	}
	graphqltest.AssertNoLeak(t, strings.Join(messages, " "),
		append(graphqltest.DatabaseNoise(), testdata.Mails(testdata.All())...)...)

	// And it really did not happen.
	var owner myTokensResponse
	graphqltest.New(h).AsUser(testdata.Eins.Mail).MustQuery(t, myTokensQuery, nil, &owner)
	if owner.MyTokens == nil || len(*owner.MyTokens) != 1 {
		t.Fatalf("the owner sees %v", owner.MyTokens)
	}
	if (*owner.MyTokens)[0].RevokedAt != nil {
		t.Error("a colleague revoked somebody else's token")
	}
}

// TestRevokingStopsTheTokenImmediately: revocation is a timestamp the authenticator reads on
// every request, so it takes effect on the next call rather than at the end of some cache
// interval. That is the property that makes revocation a useful answer to "it leaked".
func TestRevokingStopsTheTokenImmediately(t *testing.T) {
	t.Parallel()

	h := tokenHandler(t, testdata.Eins)
	browser := graphqltest.New(h).AsUser(testdata.Eins.Mail)

	var created createdTokenResponse
	browser.MustQuery(t, createMutation, map[string]any{"d": "kurzlebig"}, &created)
	secret := created.CreatePersonalAccessToken.Secret

	// Works.
	var me meResponse
	graphqltest.New(h).WithToken(secret).MustQuery(t, meQuery, nil, &me)
	if me.Me == nil {
		t.Fatal("the fresh token does not authenticate")
	}

	browser.MustQuery(t, revokeMutation,
		map[string]any{"id": created.CreatePersonalAccessToken.Token.ID}, nil)

	// And immediately does not.
	resp := graphqltest.New(h).WithToken(secret).Do(t, meQuery, nil)
	if resp.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("a revoked token still answers %d:\n%s", resp.HTTPStatus, resp.Body)
	}
	if !strings.Contains(strings.Join(resp.Messages(), " "), "widerrufen") {
		t.Errorf("the refusal does not say the token was revoked: %v", resp.Messages())
	}
}

// TestTheSecretIsShownExactlyOnce: the list must never carry it, because a list of tokens
// that included the secrets would be a list of credentials — readable by anybody who reaches
// the account page, and stored in every browser cache that touched it.
func TestTheSecretIsShownExactlyOnce(t *testing.T) {
	t.Parallel()

	h := tokenHandler(t, testdata.Eins)
	browser := graphqltest.New(h).AsUser(testdata.Eins.Mail)

	var created createdTokenResponse
	browser.MustQuery(t, createMutation, map[string]any{"d": "einmalig"}, &created)
	secret := created.CreatePersonalAccessToken.Secret

	// The schema has no field for it on the list type, so asking is a query error rather than
	// an empty answer — which is the strongest form this assertion can take.
	if messages := browser.MustFail(t, `{ myTokens { id secret } }`, nil); len(messages) == 0 {
		t.Fatal("asking for a secret in the token list was accepted")
	}

	// And the plaintext does not appear anywhere in a full listing.
	resp := browser.Do(t, `{ myTokens { id description scopes createdAt expiresAt } }`, nil)
	if strings.Contains(resp.Body, secret) {
		t.Error("the token list contains the secret in its response body")
	}
	// The public half may and must appear — it is what a revocation takes.
	if !strings.Contains(resp.Body, created.CreatePersonalAccessToken.Token.ID) {
		t.Error("the token list does not contain the token id")
	}
}

// TestCreateRefusesWhatTheDomainRefuses checks that the domain's validation reaches the
// caller as something readable, in German, rather than as a generic failure.
func TestCreateRefusesWhatTheDomainRefuses(t *testing.T) {
	t.Parallel()

	h := tokenHandler(t, testdata.Eins)
	browser := graphqltest.New(h).AsUser(testdata.Eins.Mail)

	for _, tc := range []struct {
		name        string
		variables   map[string]any
		wantInError string
	}{
		{
			name:        "no description",
			variables:   map[string]any{"d": "   "},
			wantInError: "Beschreibung",
		},
		{
			name:        "a lifetime nobody will support",
			variables:   map[string]any{"d": "zehn Jahre", "days": 3650},
			wantInError: "Gültigkeit",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			messages := browser.MustFail(t, createMutation, tc.variables)
			if !strings.Contains(strings.Join(messages, " "), tc.wantInError) {
				t.Errorf("got %v, want something mentioning %q", messages, tc.wantInError)
			}
			graphqltest.AssertNoLeak(t, strings.Join(messages, " "), graphqltest.DatabaseNoise()...)
		})
	}
}

// TestRefusalsCarryAMachineReadableCode pins what the GUI will branch on.
//
// The German sentence is the part that gets reworded — by a colleague, after a support
// question, without anybody thinking about the frontend. If tallox.gui matched on the prose,
// that rewording would silently turn "show the user a hint" into "show a generic error". The
// code is the stable half of the contract, so it belongs in a test on this side.
func TestRefusalsCarryAMachineReadableCode(t *testing.T) {
	t.Parallel()

	h := tokenHandler(t, testdata.Eins)
	browser := graphqltest.New(h).AsUser(testdata.Eins.Mail)

	var created createdTokenResponse
	browser.MustQuery(t, createMutation, map[string]any{"d": "Auswertung"}, &created)

	for _, tc := range []struct {
		name     string
		client   *graphqltest.Client
		query    string
		vars     map[string]any
		wantCode string
	}{
		{
			name:     "through a token, token management is closed",
			client:   graphqltest.New(h).WithToken(created.CreatePersonalAccessToken.Secret),
			query:    createMutation,
			vars:     map[string]any{"d": "Zweitschlüssel"},
			wantCode: "INTERACTIVE_ONLY",
		},
		{
			name:     "a description is required",
			client:   browser,
			query:    createMutation,
			vars:     map[string]any{"d": "  "},
			wantCode: "TOKEN_DESCRIPTION_REQUIRED",
		},
		{
			name:     "the lifetime has bounds",
			client:   browser,
			query:    createMutation,
			vars:     map[string]any{"d": "lang", "days": 3650},
			wantCode: "TOKEN_LIFETIME_OUT_OF_RANGE",
		},
		{
			name:     "an unknown token",
			client:   browser,
			query:    revokeMutation,
			vars:     map[string]any{"id": "ZZZZZZZZZZZZZZZZ"},
			wantCode: "TOKEN_NOT_FOUND",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := tc.client.Do(t, tc.query, tc.vars)
			if !resp.Failed() {
				t.Fatalf("expected a refusal:\n%s", resp.Body)
			}

			var codes []string
			for _, e := range resp.Errors {
				if code, ok := e.Extensions["code"]; ok {
					codes = append(codes, fmt.Sprint(code))
				}
			}
			if !slices.Contains(codes, tc.wantCode) {
				t.Errorf("codes are %v, want %s (messages: %v)",
					codes, tc.wantCode, resp.Messages())
			}
		})
	}
}
