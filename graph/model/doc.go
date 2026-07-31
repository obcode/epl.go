// Package model holds the types the GraphQL schema is built from.
//
// Two kinds of type live here. Generated ones (models_gen.go — inputs, enums, anything
// gqlgen has to invent) are rewritten by `go generate` and must not be edited. Hand-written
// ones carry both `json:` tags for GraphQL and `db:` tags for sqlc, so a row read in
// internal/store can be returned by a resolver without a translation layer in between.
//
// What does *not* belong here is anything the domain does not own: the build stamp lives in
// internal/buildinfo, and personnel data deliberately hangs off its own root fields rather
// than off a Person type, so that no traversal path to it exists in the first place.
package model
