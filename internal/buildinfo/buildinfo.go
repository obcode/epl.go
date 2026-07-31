// Package buildinfo carries the version stamp of the running binary.
//
// It lives in its own package rather than in bootstrap or graph because three unrelated
// consumers need the same three strings: main (which receives them via ldflags), the
// /healthz handler, and the GraphQL `buildInfo` field. Giving each its own struct would mean
// two conversions and two chances for the field names to drift apart.
package buildinfo

// Info is the build stamp. gqlgen binds the GraphQL type `BuildInfo` to this struct, so the
// field names here are part of the public API — renaming one is a breaking schema change.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"builtAt"`
}
