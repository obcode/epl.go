package bootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/obcode/tallox.go/internal/buildinfo"
)

// TestBothDoorsServeTheSameSchema is the test that keeps "two doors, one authorization
// model" honest at the routing level.
//
// The realistic way this design fails is not a wrong answer but a second handler: someone
// needs a tweak on the token path, mounts a separately constructed server there, and from
// that moment every rule has to be added twice. Asserting that both mounts answer the same
// query with the same payload makes that divergence a failing test rather than a discovery
// during an audit.
func TestBothDoorsServeTheSameSchema(t *testing.T) {
	build := buildinfo.Info{
		Version: "1.2.3",
		Commit:  "0123456",
		BuiltAt: "2026-07-31T09:00:00Z",
	}
	h := router(build, false)

	for _, path := range []string{"/query", "/api/graphql"} {
		t.Run(path, func(t *testing.T) {
			got := queryBuildInfo(t, h, path)
			if got != build {
				t.Errorf("%s returned %+v, want %+v", path, got, build)
			}
		})
	}
}

// TestPlaygroundIsOptional guards the flag rather than the page: the playground is the only
// route that answers on "/", so a default flip would silently expose it wherever "/" is
// routed to this container.
func TestPlaygroundIsOptional(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		want    int
	}{
		{enabled: true, want: http.StatusOK},
		{enabled: false, want: http.StatusNotFound},
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		router(buildinfo.Info{}, tc.enabled).ServeHTTP(rec, req)

		if rec.Code != tc.want {
			t.Errorf("playground=%v: GET / returned %d, want %d", tc.enabled, rec.Code, tc.want)
		}
	}
}

func queryBuildInfo(t *testing.T, h http.Handler, path string) buildinfo.Info {
	t.Helper()

	const query = `{"query":"{ buildInfo { version commit builtAt } }"}`

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(query))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s returned %d: %s", path, rec.Code, rec.Body.String())
	}

	var resp struct {
		Data struct {
			BuildInfo buildinfo.Info `json:"buildInfo"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("cannot decode response from %s: %v\n%s", path, err, rec.Body.String())
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("POST %s returned GraphQL errors: %v", path, resp.Errors)
	}
	return resp.Data.BuildInfo
}
