package bootstrap_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/obcode/tallox.go/internal/auth"
)

// devLoopArg finds the -auth-mode argument gowatch passes to the built binary.
var devLoopArg = regexp.MustCompile(`-auth-mode=([a-z-]+)`)

// TestTheDevLoopAsksForDevMode covers a gap that is not in any package: the wiring between
// the watcher and the server.
//
// The server defaults to -auth-mode=proxy, and that default is right — a server that falls
// back to dev mode when nobody configured it hands out an administrator the day somebody
// forgets a flag. But locally there is no auth proxy, so nothing sets X-Remote-User and every
// request is anonymous: `me` returns null and every authenticated field refuses. The
// application looks broken while the process is perfectly healthy, which is the most
// expensive kind of broken.
//
// It happened exactly once, immediately after the auth middleware landed, and the reason it
// was expensive is that nothing failed: `go test ./...` was green, /healthz answered, and the
// GUI just had no user. This test is what turns that into a red build instead of an evening.
func TestTheDevLoopAsksForDevMode(t *testing.T) {
	t.Parallel()

	// gowatch.yml lives at the module root; tests run in their package directory.
	raw, err := os.ReadFile("../gowatch.yml")
	if err != nil {
		t.Fatalf("cannot read gowatch.yml: %v", err)
	}

	// Comments out, first: the comment above the setting explains why the server's own
	// default is -auth-mode=proxy, and matching that sentence would report the opposite of
	// what the file configures. Caught by this test on its first run, which is a reasonable
	// argument for its existence.
	var settings strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		if code, _, _ := strings.Cut(line, "#"); strings.TrimSpace(code) != "" {
			settings.WriteString(code + "\n")
		}
	}

	match := devLoopArg.FindStringSubmatch(settings.String())
	if match == nil {
		t.Fatal("gowatch.yml passes no -auth-mode to the binary.\n" +
			"Without it the local server runs in proxy mode, nothing sets X-Remote-User, " +
			"and the GUI silently has no user while every process looks healthy.")
	}

	mode, err := auth.ParseMode(match[1])
	if err != nil {
		t.Fatalf("gowatch.yml passes -auth-mode=%s, which the server rejects: %v\n"+
			"The flag and its values live in internal/auth; renaming one without the other "+
			"stops the local server from starting at all.", match[1], err)
	}
	if mode != auth.ModeDev {
		t.Errorf("the dev loop runs in %s mode. That is a decision, not a typo — but it means "+
			"working on the GUI locally needs an X-Remote-User header on every request.", mode)
	}
}
