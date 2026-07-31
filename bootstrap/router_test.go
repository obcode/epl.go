package bootstrap_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/obcode/tallox.go/bootstrap"
	"github.com/obcode/tallox.go/internal/buildinfo"
	"github.com/obcode/tallox.go/internal/graphqltest"
	"github.com/obcode/tallox.go/internal/testdata"
)

// TestBothDoorsServeTheSameSchema is the test that keeps "two doors, one authorization
// model" honest at the routing level.
//
// The realistic way this design fails is not a wrong answer but a second handler: someone
// needs a tweak on the token path, mounts a separately constructed server there, and from
// that moment every rule has to be added twice. Asserting that both mounts answer the same
// query with the same payload makes that divergence a failing test rather than a discovery
// during an audit.
//
// Written through graphqltest.EachDoor rather than by hand, because that is the shape every
// future rule test should have — and a harness nobody uses in the repository's own tests is
// a harness that will not work when someone finally reaches for it.
func TestBothDoorsServeTheSameSchema(t *testing.T) {
	t.Parallel()

	build := buildinfo.Info{
		Version: "1.2.3",
		Commit:  "0123456",
		BuiltAt: "2026-07-31T09:00:00Z",
	}
	h := bootstrap.Handler(build, false)

	const query = `{ buildInfo { version commit builtAt } }`

	graphqltest.EachDoor(t, h, testdata.Eins.Mail, testdata.Eins.Token,
		func(t *testing.T, c *graphqltest.Client) {
			var got struct {
				BuildInfo buildinfo.Info `json:"buildInfo"`
			}
			c.MustQuery(t, query, nil, &got)

			if got.BuildInfo != build {
				t.Errorf("%s door returned %+v, want %+v", c.Door().Name, got.BuildInfo, build)
			}
		})
}

// TestBuildInfoAnswersWithoutCredentials pins a deliberate exception rather than an oversight.
//
// buildInfo is unauthenticated on purpose: the GUI footer renders before a session exists,
// and a smoke test that fails on authorization cannot distinguish a broken proxy from a
// broken server. Once the @scope directive lands and the fail-closed default applies, this
// test is what will notice if buildInfo is swept up with everything else.
func TestBuildInfoAnswersWithoutCredentials(t *testing.T) {
	t.Parallel()

	h := bootstrap.Handler(buildinfo.Info{Version: "dev"}, false)

	for _, door := range []graphqltest.Door{graphqltest.Browser, graphqltest.Token} {
		t.Run(door.Name, func(t *testing.T) {
			t.Parallel()

			c := graphqltest.New(h).On(door).Anonymous()

			var got struct {
				BuildInfo struct {
					Version string `json:"version"`
				} `json:"buildInfo"`
			}
			c.MustQuery(t, `{ buildInfo { version } }`, nil, &got)

			if got.BuildInfo.Version != "dev" {
				t.Errorf("anonymous caller got version %q, want %q", got.BuildInfo.Version, "dev")
			}
		})
	}
}

// TestPlaygroundIsOptional guards the flag rather than the page: the playground is the only
// route that answers on "/", so a default flip would silently expose it wherever "/" is
// routed to this container.
func TestPlaygroundIsOptional(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		enabled bool
		want    int
	}{
		{enabled: true, want: http.StatusOK},
		{enabled: false, want: http.StatusNotFound},
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		bootstrap.Handler(buildinfo.Info{}, tc.enabled).ServeHTTP(rec, req)

		if rec.Code != tc.want {
			t.Errorf("playground=%v: GET / returned %d, want %d", tc.enabled, rec.Code, tc.want)
		}
	}
}

// TestHealthzNeedsNothing covers what the container healthcheck and deploy/smoketest depend
// on: /healthz has to answer before the database, the auth proxy or a session exist. If it
// ever ended up behind the auth middleware, every deploy would roll itself back on a server
// that is in fact healthy.
func TestHealthzNeedsNothing(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	bootstrap.Handler(buildinfo.Info{Version: "1.2.3"}, false).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz returned %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("GET /healthz returned Content-Type %q, want application/json", ct)
	}
}
