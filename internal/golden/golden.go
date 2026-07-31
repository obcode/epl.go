// Package golden compares generated text against a committed file.
//
// It exists for one specific job named in CLAUDE.md: rendering the wish-visibility matrix to
// internal/policy/testdata/visibility_matrix.golden. Any change to who sees what, when, then
// shows up as a diff in a pull request instead of as a sentence in a commit message — and the
// same file is the slide for the faculty retreat when someone asks the question directly.
//
// The general principle is what makes it worth a package rather than a helper: some rules are
// too large to assert field by field. A visibility matrix is roles × phases × ownership; an
// export format is a header row plus escaping decisions; a schema is a list of directives. In
// each case the readable artefact is the whole rendering, and the useful review question is
// "did this change, and is the change intended".
//
// Updating is deliberate and explicit:
//
//	go test ./internal/policy/ -update-golden
//
// then read the diff before committing it. A golden file updated without being read is a
// rubber stamp with extra steps.
package golden

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update is registered as a test flag by importing this package. Named -update-golden rather
// than -update so it reads unambiguously in a shell history six months from now.
var update = flag.Bool("update-golden", false,
	"rewrite the .golden files in testdata/ instead of comparing against them")

// Assert compares got against testdata/<name>.golden.
//
// The file lives in the calling package's testdata directory, which is where `go test` already
// looks and what keeps the artefact next to the rule it describes.
func Assert(t *testing.T, name string, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")

	// Trailing-newline discipline: without it, every editor that adds one on save produces a
	// spurious diff, and reviewers learn to ignore golden diffs. That habit is the actual
	// risk here — a mechanism whose alerts get ignored protects nothing.
	got = strings.TrimRight(got, "\n") + "\n"

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("cannot create testdata directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("cannot write %s: %v", path, err)
		}
		t.Logf("updated %s — read the diff before committing it", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("%s does not exist.\n"+
				"Create it with: go test ./%s -update-golden\n"+
				"Then read it: the file is documentation, not just a fixture.",
				path, packageDir(t))
		}
		t.Fatalf("cannot read %s: %v", path, err)
	}

	if bytes.Equal(want, []byte(got)) {
		return
	}

	t.Errorf("%s is out of date.\n\n%s\n\n"+
		"If the change is intended, re-record it with:\n"+
		"    go test ./%s -update-golden\n"+
		"and review the resulting diff — this file is what answers "+
		"\"who sees what, when\".",
		path, diff(string(want), got), packageDir(t))
}

// packageDir is the relative path used in the "how to fix this" hint. Best effort: if the
// working directory cannot be resolved the hint degrades to "...", which is still better than
// no instruction at all.
func packageDir(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		return "..."
	}
	// Tests run in their package directory, so the last two segments are enough to be
	// recognisable without hard-coding the module root.
	parts := strings.Split(filepath.ToSlash(wd), "/")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return filepath.Base(wd)
}

// diff renders a line-oriented comparison.
//
// Hand-written rather than pulled from a diff library: the output only has to be readable in
// a CI log, and a golden-file helper that adds a dependency to the module is a poor trade.
func diff(want, got string) string {
	wantLines := strings.Split(strings.TrimRight(want, "\n"), "\n")
	gotLines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	var b strings.Builder
	b.WriteString("--- want (committed)\n+++ got (this run)\n")

	longest := len(wantLines)
	if len(gotLines) > longest {
		longest = len(gotLines)
	}

	// Line-by-line rather than a real LCS diff. The golden files here are matrices: one row
	// per role × phase combination, in a stable order. Rows change in place, so aligning by
	// index says exactly which combination moved — and when a row is genuinely inserted, the
	// resulting cascade is itself the correct signal that the matrix changed shape.
	shown := 0
	for i := range longest {
		w, g := at(wantLines, i), at(gotLines, i)
		if w == g {
			continue
		}
		// Cap the output: a matrix that gained a phase differs on every line, and a CI log
		// nobody scrolls to the end of is a CI log nobody reads.
		const maxLines = 40
		if shown >= maxLines {
			b.WriteString("… (further differences omitted — compare the file locally)\n")
			break
		}
		if w != "" {
			b.WriteString("- " + w + "\n")
		}
		if g != "" {
			b.WriteString("+ " + g + "\n")
		}
		shown++
	}
	return b.String()
}

func at(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	return lines[i]
}
