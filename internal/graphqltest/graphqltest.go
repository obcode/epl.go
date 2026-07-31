// Package graphqltest drives the real GraphQL handler from a test, through whichever door
// the test cares about.
//
// The invariant this exists to protect is the one in CLAUDE.md:
//
//	effective permission = (what the Role allows) ∩ (what the Scopes grant) ∩ (what the Kind allows)
//
// Two doors, one authorization model. That claim is only worth anything if tests can assert
// it cheaply, in one line, for every rule — because the way it will actually fail is not a
// wrong answer on some exotic query. It is a rule that someone adds for the browser and
// forgets on the token path, six months from now, under time pressure. EachDoor turns that
// omission into a red test rather than an audit finding.
//
// Everything here goes through httptest and the handler in-process: no port, no listener, no
// ordering problem, and a test binary that stays usable in a container with no network. The
// end-to-end variant with a real HTTP hop lives in tallox.gui's Playwright suite, which is
// where the auth proxy exists to be tested.
package graphqltest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Door is one of the two mounts the server exposes.
type Door struct {
	// Name appears as the subtest name, so a failure says which door broke.
	Name string
	// Path is the mount point.
	Path string
}

var (
	// Browser is the SvelteKit/interactive path. Identity arrives as X-Remote-User, set
	// authoritatively by the auth proxy.
	Browser = Door{Name: "browser", Path: "/query"}
	// Token is the Personal Access Token path. Bearer only, no header fallback — that
	// separation is a route in production, not a header check, so it is a route here too.
	Token = Door{Name: "token", Path: "/api/graphql"}
)

// Client issues operations against one handler, through one door, as one principal.
//
// The With* methods return copies, so a client built once in a test setup can be specialised
// per subtest without the subtests interfering.
type Client struct {
	handler http.Handler
	door    Door
	headers http.Header
}

// New returns an anonymous client on the browser door.
func New(h http.Handler) *Client {
	return &Client{handler: h, door: Browser, headers: http.Header{}}
}

func (c *Client) clone() *Client {
	headers := make(http.Header, len(c.headers))
	for k, v := range c.headers {
		headers[k] = append([]string(nil), v...)
	}
	return &Client{handler: c.handler, door: c.door, headers: headers}
}

// AsUser authenticates through the browser door as the given principal.
//
// The header is set the way the proxy sets it. Note that in production Caddy *strips* any
// incoming X-Remote-* before adding its own — a test that sets this header is standing in for
// the proxy, not for a client that could send it.
func (c *Client) AsUser(user string) *Client {
	n := c.clone()
	n.door = Browser
	n.headers.Set("X-Remote-User", user)
	return n
}

// WithToken authenticates through the token door with a Personal Access Token.
func (c *Client) WithToken(token string) *Client {
	n := c.clone()
	n.door = Token
	n.headers.Set("Authorization", "Bearer "+token)
	return n
}

// Anonymous strips every credential, keeping the door.
//
// For the "what does an unauthenticated caller see" tests, which matter more than they look:
// buildInfo has to answer (the footer renders before a session exists) and nothing else may.
func (c *Client) Anonymous() *Client {
	n := c.clone()
	n.headers.Del("X-Remote-User")
	n.headers.Del("X-Remote-Displayname")
	n.headers.Del("Authorization")
	return n
}

// On switches doors, keeping the credentials.
func (c *Client) On(d Door) *Client {
	n := c.clone()
	n.door = d
	return n
}

// WithHeader sets an arbitrary header, for tests about the header handling itself.
func (c *Client) WithHeader(key, value string) *Client {
	n := c.clone()
	n.headers.Set(key, value)
	return n
}

// Door reports which mount this client uses.
func (c *Client) Door() Door { return c.door }

// Response is one GraphQL result, kept in its raw form.
//
// Errors are a first-class field rather than something the harness turns into t.Fatal,
// because in this codebase the error *is* often the thing under test: "an unauthorised caller
// gets a refusal", "a duplicate wish produces a generic message and not the SQLSTATE".
type Response struct {
	// HTTPStatus of the response. A GraphQL refusal is a 200 with an errors array; a 4xx here
	// means the request never reached the schema.
	HTTPStatus int
	// Data as returned. Nil when the operation failed outright.
	Data json.RawMessage
	// Errors as returned, in order.
	Errors []Error
	// Body is the untouched response body, for failure messages worth reading.
	Body string
}

// Error is one entry of the GraphQL errors array.
type Error struct {
	Message    string         `json:"message"`
	Path       []any          `json:"path"`
	Extensions map[string]any `json:"extensions"`
}

// Failed reports whether the response carries any GraphQL error.
func (r Response) Failed() bool { return len(r.Errors) > 0 }

// Messages returns the error messages in order.
func (r Response) Messages() []string {
	out := make([]string, 0, len(r.Errors))
	for _, e := range r.Errors {
		out = append(out, e.Message)
	}
	return out
}

// Decode unmarshals the data section into out.
func (r Response) Decode(t *testing.T, out any) {
	t.Helper()

	if len(r.Data) == 0 {
		t.Fatalf("response carries no data: %s", r.Body)
	}
	if err := json.Unmarshal(r.Data, out); err != nil {
		t.Fatalf("cannot decode data: %v\n%s", err, r.Body)
	}
}

// Do executes an operation and returns whatever came back, errors included.
func (c *Client) Do(t *testing.T, query string, variables map[string]any) Response {
	t.Helper()

	payload, err := json.Marshal(struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables,omitempty"`
	}{Query: query, Variables: variables})
	if err != nil {
		t.Fatalf("cannot encode operation: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, c.door.Path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header[k] = v
	}

	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	out := Response{HTTPStatus: rec.Code, Body: body}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []Error         `json:"errors"`
	}
	// A non-JSON body is itself the finding — most often the playground's HTML, which means
	// the request went to "/" instead of the door, or an auth proxy answered instead of the
	// server. Reporting the body verbatim is what makes that diagnosable.
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("POST %s returned a body that is not GraphQL JSON (HTTP %d): %v\n%s",
			c.door.Path, rec.Code, err, body)
	}
	out.Data, out.Errors = envelope.Data, envelope.Errors
	return out
}

// MustQuery executes an operation that is expected to succeed and decodes it into out.
func (c *Client) MustQuery(t *testing.T, query string, variables map[string]any, out any) {
	t.Helper()

	resp := c.Do(t, query, variables)
	if resp.HTTPStatus != http.StatusOK {
		t.Fatalf("POST %s returned HTTP %d:\n%s", c.door.Path, resp.HTTPStatus, resp.Body)
	}
	if resp.Failed() {
		t.Fatalf("POST %s returned GraphQL errors: %v", c.door.Path, resp.Messages())
	}
	if out != nil {
		resp.Decode(t, out)
	}
}

// MustFail executes an operation that is expected to be refused and returns the messages.
//
// A refusal that arrives as an empty result rather than an error is the failure mode worth
// naming: it looks like "no data" to a caller and like "working as intended" to a reviewer,
// so it has to be a test failure here.
func (c *Client) MustFail(t *testing.T, query string, variables map[string]any) []string {
	t.Helper()

	resp := c.Do(t, query, variables)
	if !resp.Failed() {
		t.Fatalf("POST %s was expected to be refused but succeeded:\n%s",
			c.door.Path, resp.Body)
	}
	return resp.Messages()
}

// EachDoor runs fn once per door, as a subtest, with a client pointed at that door.
//
// The pair of credentials belongs to the *same* principal: user is the proxy identity, token
// is a Personal Access Token owned by that user. That is what makes the assertion inside fn
// meaningful — same person, same expected answer, different route in.
//
// Where the two doors are supposed to differ (@interactiveOnly), the test says so explicitly
// by asserting per door, rather than by quietly not covering one of them.
func EachDoor(t *testing.T, h http.Handler, user, token string, fn func(t *testing.T, c *Client)) {
	t.Helper()

	base := New(h)
	for _, tc := range []struct {
		door   Door
		client *Client
	}{
		{Browser, base.AsUser(user)},
		{Token, base.WithToken(token)},
	} {
		t.Run(tc.door.Name, func(t *testing.T) {
			fn(t, tc.client)
		})
	}
}

// Leaks reports which of the forbidden substrings occur in a user-facing message.
//
// Separate from the assertion so that the rule itself is testable without a *testing.T that
// is expected to fail — and so callers who want to inspect rather than assert can.
//
// Matching is case-insensitive: PostgreSQL's wording and ours rarely agree on case, and a
// detector that misses "Prof.Zwei@Example.ORG" because the fixture is lowercase is a detector
// that will be trusted and wrong. Empty needles are ignored rather than matching everything,
// because forbidden lists are routinely built from optional fields.
func Leaks(message string, forbidden []string) []string {
	lower := strings.ToLower(message)

	var found []string
	for _, f := range forbidden {
		if f == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(f)) {
			found = append(found, f)
		}
	}
	return found
}

// AssertNoLeak fails when a user-facing string contains anything it must not.
//
// This is leak channel 2 from CLAUDE.md, made checkable. A verbatim uniqueness violation on
// the write path reveals that somebody else has already registered interest — which is the
// exact information the confidentiality rule exists to withhold, delivered by an error
// message rather than by a query.
//
// Call it with whatever the test knows must not appear: the other person's mail address, a
// table name, and DatabaseNoise() for the driver's own vocabulary.
func AssertNoLeak(t *testing.T, message string, forbidden ...string) {
	t.Helper()

	for _, f := range Leaks(message, forbidden) {
		t.Errorf("user-facing message leaks %q:\n\t%s\n"+
			"Map database errors generically on the write path — the verbatim text "+
			"reveals that someone else is already registered.", f, message)
	}
}

// DatabaseNoise lists the substrings that mean a raw driver or PostgreSQL error reached the
// caller. Handy as the tail of an AssertNoLeak call.
func DatabaseNoise() []string {
	return []string{
		"SQLSTATE",
		"duplicate key",
		"violates unique constraint",
		"violates foreign key",
		"pq:",
		"pgx",
		"ERROR:",
	}
}
