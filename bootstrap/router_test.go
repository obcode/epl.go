package bootstrap_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/obcode/tallox.go/bootstrap"
	"github.com/obcode/tallox.go/internal/auth"
	"github.com/obcode/tallox.go/internal/buildinfo"
	"github.com/obcode/tallox.go/internal/graphqltest"
)

// The tests in this file need no database: they are about the routes that answer before
// anybody has authenticated, and about which routes exist at all. Everything that requires a
// real identity lives in auth_test.go, against real PostgreSQL.

// TestBuildInfoAnswersWithoutCredentials pins a deliberate exception rather than an oversight.
//
// buildInfo is unauthenticated on purpose: the GUI footer renders before a session exists,
// and a smoke test that fails on authorization cannot distinguish a broken proxy from a
// broken server. Once the @scope directive lands and the fail-closed default applies, this
// test is what will notice if buildInfo is swept up with everything else.
func TestBuildInfoAnswersWithoutCredentials(t *testing.T) {
	t.Parallel()

	h := bootstrap.Handler(bootstrap.Options{
		Build: buildinfo.Info{Version: "dev"},
		Auth:  auth.Config{Mode: auth.ModeProxy},
	})

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

// TestMeIsNullWithoutASession is the other half of that exception, and the reason the
// middleware lets an unauthenticated request through instead of refusing it.
//
// Callers without a session get null rather than an error, because "nobody is logged in" is a
// state the GUI renders. What must never happen is a person coming back.
func TestMeIsNullWithoutASession(t *testing.T) {
	t.Parallel()

	h := bootstrap.Handler(bootstrap.Options{
		Build: buildinfo.Info{Version: "dev"},
		Auth:  auth.Config{Mode: auth.ModeProxy},
	})

	for _, door := range []graphqltest.Door{graphqltest.Browser, graphqltest.Token} {
		t.Run(door.Name, func(t *testing.T) {
			t.Parallel()

			var got struct {
				Me *struct {
					Mail string `json:"mail"`
				} `json:"me"`
			}
			graphqltest.New(h).On(door).Anonymous().
				MustQuery(t, `{ me { mail } }`, nil, &got)

			if got.Me != nil {
				t.Errorf("an anonymous caller is %q", got.Me.Mail)
			}
		})
	}
}

// TestOffTokenModeRemovesTheMachineDoor covers the emergency stop.
//
// A 404 rather than a 401: the route is not mounted, so no code path is left that could be
// wrong about whether the stop is engaged. Switching a surface off by omission rather than by
// a guard is what makes it trustworthy at three in the morning.
func TestOffTokenModeRemovesTheMachineDoor(t *testing.T) {
	t.Parallel()

	h := bootstrap.Handler(bootstrap.Options{
		Build: buildinfo.Info{Version: "dev"},
		Auth:  auth.Config{Mode: auth.ModeOffToken},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/graphql", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /api/graphql answered %d in off-token mode, want 404: %s",
			rec.Code, rec.Body)
	}

	// The stop closes one door, not the building.
	var got struct {
		BuildInfo struct {
			Version string `json:"version"`
		} `json:"buildInfo"`
	}
	graphqltest.New(h).Anonymous().MustQuery(t, `{ buildInfo { version } }`, nil, &got)
	if got.BuildInfo.Version != "dev" {
		t.Error("the browser door stopped working in off-token mode")
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
		bootstrap.Handler(bootstrap.Options{Playground: tc.enabled}).ServeHTTP(rec, req)

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
	bootstrap.Handler(bootstrap.Options{Build: buildinfo.Info{Version: "1.2.3"}}).
		ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz returned %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("GET /healthz returned Content-Type %q, want application/json", ct)
	}
}
