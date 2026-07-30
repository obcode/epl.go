// Package arch holds architecture tests: rules about the shape of the codebase that are
// cheap to state and expensive to rediscover after they have been violated for six months.
package arch

import (
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/obcode/epl.go"

// dbPackages must not be imported outside the storage layer.
var dbPackages = []string{
	"github.com/jackc/pgx/v5",
	"github.com/jackc/pgx/v5/pgxpool",
	"database/sql",
}

// storeOnly lists the import path prefixes that are allowed to talk to the database.
var storeOnly = []string{
	modulePath + "/internal/store",
	modulePath + "/internal/arch", // this test itself
}

// TestOnlyStoreTouchesTheDatabase is the mechanism that makes "every read goes through the
// policy" true by construction rather than by discipline.
//
// If resolvers, the domain layer or a future CSV/PDF export handler could reach pgx
// directly, each of them would have to remember to apply the wish-visibility filter — and
// one forgotten call site is not a bug here, it is the political failure the whole project
// is meant to prevent. Funnelling everything through internal/store means the filter is
// applied in one place.
//
// If this test fails, the fix is to add a query to internal/store, not to widen storeOnly.
func TestOnlyStoreTouchesTheDatabase(t *testing.T) {
	for _, pkg := range listPackages(t) {
		if isStorage(pkg.ImportPath) {
			continue
		}
		for _, imp := range append(append([]string{}, pkg.Imports...), pkg.TestImports...) {
			if !isDBPackage(imp) {
				continue
			}
			t.Errorf("%s imports %s — only internal/store may talk to the database.\n"+
				"Add a query to internal/store instead; that is where the visibility "+
				"policy is applied.", pkg.ImportPath, imp)
		}
	}
}

type goListPackage struct {
	ImportPath  string   `json:"ImportPath"`
	Imports     []string `json:"Imports"`
	TestImports []string `json:"TestImports"`
}

func listPackages(t *testing.T) []goListPackage {
	t.Helper()

	// `go list` rather than golang.org/x/tools/go/packages: same answer, no dependency.
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = ".."
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("cannot list packages: %v\n%s", err, stderr)
	}

	var pkgs []goListPackage
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var p goListPackage
		if err := dec.Decode(&p); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("cannot decode go list output: %v", err)
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		t.Fatal("go list returned no packages")
	}
	return pkgs
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError) //nolint:errorlint // deliberate: we only want the direct type
	if ok {
		*target = ee
	}
	return ok
}

func isStorage(importPath string) bool {
	for _, prefix := range storeOnly {
		if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
			return true
		}
	}
	return false
}

func isDBPackage(importPath string) bool {
	for _, db := range dbPackages {
		if importPath == db || strings.HasPrefix(importPath, db+"/") {
			return true
		}
	}
	return false
}
