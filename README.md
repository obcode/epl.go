# tallox.go

GraphQL backend for **Tallox** (from *Teacher Allocations*), the teaching-assignment
planning system (*Einsatzplanung*) of faculty 07 at Hochschule München.

> **Status: early construction.** Tooling, CI, identity and authorization are in place;
> the planning domain — modules, instances, wishes — is being built. See
> [CLAUDE.md](CLAUDE.md) for the architecture.

## What it does

Faculty 07 plans, every semester, which courses are offered and who teaches them. Tallox
models that process:

1. **Bedarfsplanung** — study-programme leads declare which course instances are needed.
2. **Wunschphase** — lecturers register interest in instances. Those entries stay invisible
   to everyone else until a publication milestone.
3. **Zuteilung** — subject-group leads assign instances to people.
4. **Auswertung** — the dean's office evaluates import/export between programmes and
   faculties.

Courses are planned as **instances**, not modules: a module can run twice in a semester, and
a lecture and its lab can be taught by different people. The assignable unit is the
*instance part*.

## Two ways in

The GraphQL API is a product, not just a contract with the web UI.

| Path | Authentication | For |
| --- | --- | --- |
| `POST /query` | `X-Remote-User`, injected by the auth proxy after OIDC | browser and server-side rendering |
| `POST /api/graphql` | `Authorization: Bearer tallox_…` | scripts and evaluations |

Both mount the **same** GraphQL handler and the same authorization model:

> effective permission = (what the role allows) ∩ (what the token's scopes grant) ∩ (what the access kind allows)

A token can never do more than its owner. Personnel data (the Deputat/overtime traffic light),
other people's unpublished wishes, and token management itself are not reachable with a token
at all.

Create a token in the web UI under `/account/tokens`; the plaintext is shown once.

```console
$ curl -sS https://<host>/api/graphql \
    -H "Authorization: Bearer $TALLOX_TOKEN" \
    -H 'Content-Type: application/json' \
    -d '{"query":"{ me { mail name roles } }"}'
```

## Stack

Go · [gqlgen](https://gqlgen.com) · PostgreSQL 18 · [sqlc](https://sqlc.dev) ·
[goose](https://github.com/pressly/goose) · chi · zerolog · viper

No ORM: SQL is written by hand in `db/queries/`, sqlc turns it into typed Go.

## Development

Everything runs in the DevContainer from the `tallox.dev` repo — Go, Node, PostgreSQL and all
tooling, one *Reopen in Container* away.

```bash
go build ./...
go test ./...
golangci-lint-v2 run
go generate ./...                                     # gqlgen
sqlc generate                                         # db/queries/*.sql -> Go
goose -dir db/migrations postgres "$TALLOX_DB_URL" up
gowatch                                               # live-reloading server on :8080
```

The server needs `TALLOX_DB_URL` and applies the embedded migrations at startup.

Locally `-auth-mode=dev` injects a development user on the browser path, so the GraphQL
playground at `/` works without a login. That user holds every role; to see what a colleague
with one role sees, seed a person and send `X-Remote-User` yourself. The **token path stays
real** even in dev — it needs an actual PAT from the local database, which keeps the
production code path exercised daily instead of discovered in October.

## Layout

```
bootstrap/          entrypoint: flags, config, wiring, router, shutdown
graph/              gqlgen schema and resolvers (one .graphqls per domain)
internal/principal/ the authenticated actor in the context
internal/auth/      proxy-header and bearer-token authenticators
internal/policy/    visibility and phase rules — pure, no I/O
internal/domain/    business logic
internal/store/     the only package that touches the database
db/migrations/      goose
db/queries/         sqlc
```

A CI test asserts that no package outside `internal/store` imports `pgx`. That is what makes
"every read goes through the policy" true by construction rather than by discipline.

## License

BSD 3-Clause. See [LICENSE](LICENSE).
