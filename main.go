// Command epl is the GraphQL backend of the EPL teaching-assignment planning system.
//
// It has no subcommands: the binary starts the server. Everything else is a GraphQL query,
// mutation or subscription, driven either from epl.gui or from a script holding a Personal
// Access Token.
package main

import (
	"time"

	"github.com/obcode/epl.go/bootstrap"
)

// Injected via ldflags at build time; see Dockerfile.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Milestone deadlines, phase transitions and the "how red is this unfilled instance"
	// calculation are all local-time. Pin it here so a container without TZ configuration
	// cannot silently plan in UTC.
	loc, err := time.LoadLocation("Europe/Berlin")
	if err == nil {
		time.Local = loc
	}

	bootstrap.Serve(bootstrap.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	})
}
