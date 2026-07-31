// Package db carries the SQL assets as an embedded filesystem: the goose migrations that
// build the schema, and (once queries exist) the sqlc inputs next to them.
//
// Embedding rather than shipping a directory is what makes "migrations are applied at
// startup" safe to promise. A container that has the binary has the migrations, by
// construction — there is no deploy step that can copy the binary and forget the .sql files,
// and no way for the running server to be one migration ahead of the files on disk.
//
// It is also what lets an integration test migrate a throwaway schema without knowing where
// the repository root is.
package db

import "embed"

// Migrations holds db/migrations/*.sql for goose.
//
// The FS root is this package, so goose has to be pointed at the "migrations" subdirectory —
// see store.Migrate, which is the only caller and does exactly that.
//
//go:embed migrations/*.sql
var Migrations embed.FS
