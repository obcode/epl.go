package bootstrap_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obcode/tallox.go/bootstrap"
)

// TestNoConfigFileIsNotAnError.
//
// The development loop runs entirely on flags, and `go run .` in a fresh checkout has no
// tallox.yaml anywhere. A server that refused to start without one would make the first run of
// every new clone a support question.
func TestNoConfigFileIsNotAnError(t *testing.T) {
	t.Parallel()

	inEmptyDir(t)

	cfg, file, err := bootstrap.LoadConfig("")
	if err != nil {
		t.Fatalf("loading without a file failed: %v", err)
	}
	if file != "" {
		t.Errorf("reported reading %q, want none", file)
	}
	if cfg.Auth.Mode != "proxy" {
		t.Errorf("auth mode defaulted to %q, want proxy — a server that falls back to "+
			"something more convenient when nobody configured it hands out an administrator "+
			"on the day somebody forgets a file", cfg.Auth.Mode)
	}
}

// TestAnExplicitPathThatDoesNotExistIsAnError: somebody asked for that file. Silently running
// on defaults would be a container that ignores its mount and looks healthy doing it.
func TestAnExplicitPathThatDoesNotExistIsAnError(t *testing.T) {
	t.Parallel()

	if _, _, err := bootstrap.LoadConfig(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("a missing explicit configuration file was accepted")
	}
}

// TestAnUnknownKeyIsAStartupFailure.
//
// The alternative is the mode this whole exercise exists to end: a file that documents a
// setting, a program that ignores it, and an operator who is wrong about what their server is
// doing. A typo in production has to be loud on the restart that introduces it.
func TestAnUnknownKeyIsAStartupFailure(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `
auth:
  protectedadmin:
    - mail: admin@example.org
`)

	_, _, err := bootstrap.LoadConfig(path)
	if err == nil {
		t.Fatal("a misspelled key was accepted — the protected administrators would silently " +
			"be empty, and nobody would find out until the day they are needed")
	}
	if !strings.Contains(err.Error(), "protectedadmin") {
		t.Errorf("the error does not name the offending key: %v", err)
	}
}

// TestAFileFillsInOnlyWhatItMentions: a file that sets one thing must not zero everything else.
func TestAFileFillsInOnlyWhatItMentions(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `
auth:
  protectedadmins:
    - mail: admin@example.org
      name: Admin
`)

	cfg, _, err := bootstrap.LoadConfig(path)
	if err != nil {
		t.Fatalf("loading failed: %v", err)
	}

	if len(cfg.Auth.ProtectedAdmins) != 1 || cfg.Auth.ProtectedAdmins[0].Mail != "admin@example.org" {
		t.Fatalf("protected admins are %+v", cfg.Auth.ProtectedAdmins)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("port is %d, want the default 8080 rather than a zero", cfg.Server.Port)
	}
	if cfg.Auth.Mode != "proxy" {
		t.Errorf("auth mode is %q, want the default", cfg.Auth.Mode)
	}
	if !cfg.Server.Introspection {
		t.Error("introspection defaulted to off — the API is a product here, and " +
			"introspection is what makes editor completion and codegen work for colleagues")
	}
}

// TestOnlyFlagsThatWereActuallySetOverrideTheFile is the whole basis of the precedence rule,
// and the usual way a configuration layer is quietly wrong.
//
// Without the "was it set" distinction there is no way to tell `-playground=false` from "the
// flag defaulted to false", so every flag default would override the file and the file would
// be decorative. It stays invisible until somebody sets a value that happens to equal a
// default.
func TestOnlyFlagsThatWereActuallySetOverrideTheFile(t *testing.T) {
	t.Parallel()

	fromFile := bootstrap.Config{
		Server: bootstrap.ServerConfig{Port: 9999, Playground: false, Introspection: true},
		Auth:   bootstrap.AuthConfig{Mode: "proxy", DevUser: "from-file@example.org"},
		Log:    bootstrap.LogConfig{Level: "warn"},
	}
	flags := bootstrap.FlagOverrides{
		Addr:       ":8080",
		Playground: true,
		AuthMode:   "dev",
		DevUser:    "from-flag@example.org",
		Verbose:    true,
	}

	t.Run("no flag set: the file wins entirely", func(t *testing.T) {
		got, err := bootstrap.ApplyFlagOverrides(fromFile, map[string]bool{}, flags)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Server != fromFile.Server || got.Log != fromFile.Log ||
			got.Auth.Mode != fromFile.Auth.Mode || got.Auth.DevUser != fromFile.Auth.DevUser {
			t.Errorf("flag defaults leaked into the configuration:\n got %+v\nwant %+v",
				got, fromFile)
		}
	})

	t.Run("one flag set: only that one wins", func(t *testing.T) {
		got, err := bootstrap.ApplyFlagOverrides(fromFile, map[string]bool{"auth-mode": true}, flags)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Auth.Mode != "dev" {
			t.Errorf("auth mode is %q, want the flag to win", got.Auth.Mode)
		}
		if got.Server.Port != 9999 {
			t.Errorf("port is %d, want the file's value untouched", got.Server.Port)
		}
		if got.Auth.DevUser != "from-file@example.org" {
			t.Errorf("dev user is %q, want the file's value untouched", got.Auth.DevUser)
		}
	})

	t.Run("-v raises the level but never lowers it", func(t *testing.T) {
		quiet := flags
		quiet.Verbose = false
		got, err := bootstrap.ApplyFlagOverrides(fromFile, map[string]bool{"v": true}, quiet)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Log.Level != "warn" {
			t.Errorf("log level is %q — -v is a switch meaning \"show me everything\", and "+
				"there is no -v=false that could mean \"be quieter than the file says\"",
				got.Log.Level)
		}
	})
}

// TestAddrOverridesThePort keeps the flag working against a file that speaks in ports.
func TestAddrOverridesThePort(t *testing.T) {
	t.Parallel()

	base := bootstrap.DefaultConfig()

	got, err := bootstrap.ApplyFlagOverrides(base, map[string]bool{"addr": true},
		bootstrap.FlagOverrides{Addr: ":9000"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Server.Port != 9000 || got.Server.Addr() != ":9000" {
		t.Errorf("port is %d (%q), want 9000", got.Server.Port, got.Server.Addr())
	}
}

// TestAddrWithAHostIsRefusedRatherThanIgnored.
//
// This server always listens on all interfaces and is reached through the reverse proxy, so
// there is nothing to do with the host part. Dropping it silently would make
// `-addr=127.0.0.1:8080` mean something other than what it says, which is worth an error at
// startup rather than a surprise during a deploy.
func TestAddrWithAHostIsRefusedRatherThanIgnored(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{"127.0.0.1:8080", "8080", ":0", ":notaport"} {
		if _, err := bootstrap.ApplyFlagOverrides(bootstrap.DefaultConfig(),
			map[string]bool{"addr": true}, bootstrap.FlagOverrides{Addr: addr}); err == nil {
			t.Errorf("-addr %q was accepted", addr)
		}
	}
}

// writeConfig puts a configuration file in a temporary directory and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tallox.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("cannot write the test configuration: %v", err)
	}
	return path
}

// inEmptyDir moves the process into a directory with no tallox.yaml, so that the search path
// finds nothing.
//
// Not parallel-safe with anything else that changes the working directory — which is why the
// only test using it does nothing else.
func inEmptyDir(t *testing.T) {
	t.Helper()
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot read the working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("cannot change directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(before) })
}
