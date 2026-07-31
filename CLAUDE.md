# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this
repository. The workspace-level file at `/workspace/CLAUDE.md` (from the private `tallox.dev`
repo) applies as well and covers the domain glossary, the cross-repo workflow and the git
conventions.

## Overview

`tallox.go` is the GraphQL backend for **Tallox** (from *Teacher Allocations*), the
teaching-assignment planning system (*Einsatzplanung*) of faculty 07 at Hochschule München. It models the planning process
defined in FKR 387/378: study-programme leads declare which course instances are needed,
lecturers register interest, subject-group leads assign, and the dean's office evaluates.

It is the backend for `tallox.gui` **and a first-class API**: colleagues use Personal Access
Tokens to write their own evaluations. Every rule the GUI appears to enforce is enforced
here, because the token path bypasses the GUI entirely.

Domain terminology is German; code, identifiers, comments and commit messages are English.
User-facing strings (policy `Reason` texts, validation messages) stay German.

**This repository is public.** No hostnames, no operational detail, no names of colleagues in
fixtures, tests, comments or commit messages. Test data is invented (`prof.eins@example.org`),
not anonymised-real.

## Build, Test, Lint

```bash
go build ./...
go test ./...                      # integration tests need $TALLOX_TEST_DB_URL
go test ./internal/policy/ -run TestVisibilityMatrix
go vet ./...
golangci-lint-v2 run               # note the -v2 binary name
go generate ./...                  # gqlgen, after editing graph/*.graphqls
sqlc generate                      # after editing db/queries/*.sql
goose -dir db/migrations postgres "$TALLOX_DB_URL" up
```

Pre-commit hooks run gofmt, go vet, golangci-lint-v2 and gitleaks. Run `pre-commit install`
once. The gitleaks config carries a rule for the `tallox_` token format — the realistic
leak path is a colleague's evaluation script, not the server.

## Architecture

```
main.go                    version, time.Local = Europe/Berlin, bootstrap.Serve()
bootstrap/                 flags, viper config, wiring, chi router, graceful shutdown
graph/                     gqlgen: *.graphqls (follow-schema) + *.resolvers.go — thin
internal/buildinfo/        the ldflags version stamp. Shared by main, /healthz and `buildInfo`.
internal/principal/        the authenticated Actor in the context. stdlib + uuid only.
internal/auth/             two authenticators, one middleware
internal/policy/           visibility and phase rules. Pure: no DB, no HTTP, no GraphQL.
internal/domain/           business logic
internal/store/            the ONLY owner of pgxpool. sqlc-generated queries.
db/migrations/*.sql        goose
db/queries/*.sql           sqlc
internal/testdata/         invented fixtures
```

**Enforced by a CI test: no package outside `internal/store` imports `pgx`/`pgxpool`.**
That makes "resolvers cannot reach the database" mechanically true rather than aspirational,
and it means every future CSV/PDF export necessarily goes through the policy. This is a
deliberate response to how the sibling project's business-logic package grew — starting with
the boundary costs almost nothing, retrofitting it is a multi-month effort.

Resolvers are thin and delegate to `internal/domain`. Hand-written model types live in
`graph/model/` with both `json:` (GraphQL) and `db:` tags and are autobound by gqlgen.

Generated, do not edit by hand: `graph/generated/`, `graph/model/models_gen.go`,
`internal/store/*.sql.go`.

## Auth: two doors, one authorization model

The backend does **not** authenticate interactively. In production it sits behind Caddy →
oauth2-proxy → OIDC (`sso.hm.edu`), which injects a trusted `X-Remote-User`. Separately, it
authenticates Personal Access Tokens itself on a distinct path.

```
/query, /download/*   ← browser + SvelteKit SSR, X-Remote-User (proxy-verified)
/api/graphql          ← Personal Access Token, Bearer ONLY, no fallback
```

The same gqlgen handler is mounted on both, so there is no second schema and no second
`AroundOperations` to forget.

**The invariant, tested once:**

> effective permission = (what the Role allows) ∩ (what the Scopes grant) ∩ (what the Kind allows)

A token can never exceed its owner's role; demoting a user instantly demotes their tokens.

`auth.mode` is `dev` | `proxy` | `off-token`, not a boolean. In `dev` the browser path injects
an ADMIN dev user **but the token path stays real** — that way the production code path is
exercised daily instead of discovered in October.

### Scopes

Schema-driven via a `@scope(area:, verb:)` directive, evaluated in `AroundOperations`. A root
field without `@scope` is treated as `ADMIN:WRITE` (fail-closed) **and fails a CI test**.

Two things the sibling project's equivalent gets wrong, which must not be repeated:

1. Its root-field walk ignores inline fragments and fragment spreads, so
   `mutation { ... on Mutation { … } }` yields an empty field list and the write check falls
   through to "not data-changing". Here: recurse into fragments **and** set the structural
   rule (`Mutation ∨ Subscription ⇒ WRITE`) independently of the walk.
2. Its default direction is fail-open. Here it is fail-closed.

### `@interactiveOnly`

A real gqlgen field directive, so *generated* code calls it and no resolver can forget it. On
nullable fields it returns `null` rather than erroring the whole operation, so scripts stay
useful and the schema documents the boundary itself.

Not reachable via token: the Deputat/overtime traffic light (personnel data), unpublished
wishes of other people, free-text notes about people, **token management itself** (otherwise a
leaked token mints its successors), user administration, the audit log.

Belt and braces: keep personnel data in its own root fields rather than hanging it off
`Person`, so there is no traversal path in the first place.

## Policy: the rule that carries the project

Wishes are confidential until published. Visible iff owner ∨ role ∈ {Planer, Dekanat} ∨
`semester.wishes_published_at IS NOT NULL`. The purpose is *kein Windhundverfahren* — a leak
here is political damage, not a bug.

`internal/policy` is pure and holds the rule in **two consistent forms**:

- `CanSee…(actor, state, record) bool` — guard, for records already in hand
- `…Visibility(actor, state) …Filter` — query parameters, so the predicate runs in the
  database and indexes apply

A property test over the full cartesian product asserts the two agree. That drift is the
realistic way this design fails.

**Three leak channels row filtering does not cover:**

1. **Aggregates.** "3 colleagues already registered interest" leaks the information
   completely without a single name. Before publication: no counts, no has-wishes flags, no
   sorting by them, no heat-map colouring. Counts go through the same filter.
2. **Error messages.** A verbatim `UNIQUE` violation reveals that someone else already
   registered. Map DB errors generically on the write path.
3. **Exports.** Every CSV/PDF/ICS handler goes through `internal/store`.

`TestVisibilityMatrix` renders the full matrix to
`internal/policy/testdata/visibility_matrix.golden`. Every policy change becomes a reviewable
diff — and that file is the slide for the faculty retreat when someone asks who sees what
when. Keep it committed and keep it readable.

The semester phase lives in the database and is advanced by an audited mutation, **never
derived from the calendar**.

## Configuration

viper, single file `tallox.yaml` (in `.` or `$HOME`), plus `TALLOX_DB_URL` from the environment.
Secrets stay in the file, never in the database.

Rule of thumb: YAML holds bootstrap values and secrets. Everything semester-scoped and
user-editable (semester config, milestones, phases) lives in PostgreSQL and is edited through
the GUI.

Note `server.introspection` defaults to **on**, even in production — unlike the sibling
project. The API is a product here; introspection is what makes editor completion, codegen
and schema exploration work for the colleagues writing evaluations. Only the playground is
disabled in production.

## Conventions

- **Logging:** `zerolog` — `log.Error().Err(err).Str("field", v).Msg("cannot do x")`.
  `log.Fatal()` only in `bootstrap/`.
- **Timezone:** `main.go` forces `time.Local = Europe/Berlin`. Milestone and phase
  calculations depend on it.
- **Errors:** plain `error` wrapped with `fmt.Errorf("...: %w", err)`. No custom hierarchy.
  "Not found" is `(nil, nil)`.
- **Tests:** stdlib `testing`, table-driven with `t.Run`. No testify, no mock library — seams
  are narrow hand-written interfaces (see `auth.UserLookup`, `auth.TokenLookup`).
  Integration tests use `$TALLOX_TEST_DB_URL`, get their own schema and drop it in `t.Cleanup`.
  CI sets `TALLOX_TEST_DB_REQUIRED=1` so a skipped integration test is a failure rather than
  silently green.
- **Migrations:** goose, embedded, applied at startup. Steps must be idempotent; never edit
  or reorder a released migration.
- Version is injected via ldflags into `main.version/commit/date`.
