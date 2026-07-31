-- Migration 1: the extensions every later migration assumes.
--
-- Deliberately no domain tables. This one exists so that a database created from nothing —
-- a Testcontainers instance in CI, a fresh volume on the host — reaches the same starting
-- line as the DevContainer's Postgres, whose initdb script installs citext once at first
-- start. Without it, the first migration that declares an e-mail column would pass locally
-- and fail in CI, which is the least useful place to discover an extension is missing.
--
-- gen_random_uuid() is in core since PostgreSQL 13, so pgcrypto is not needed.

-- +goose Up

-- Explicitly in `public`, not "wherever search_path happens to point".
--
-- Integration tests run with search_path set to a throwaway schema (see
-- internal/store/storetest). Without WITH SCHEMA public, each test would install its own
-- copy of citext into its own schema and drop it again — slow, and it would make the
-- extension's location depend on who ran the migration.
--
-- The exception handler is not defensive padding. `CREATE EXTENSION IF NOT EXISTS` is *not*
-- atomic: the existence check and the catalog insert are separate steps, so two sessions that
-- run it at the same moment both see "not there" and the loser gets
--
--     duplicate key value violates unique constraint "pg_extension_name_index"  (23505)
--
-- Two things do exactly that. In CI, the integration tests migrate their schemas in parallel
-- against a database created seconds earlier. In production, a deploy that starts two API
-- replicas has both applying migrations at startup. Neither shows up on a developer machine,
-- where the extension has existed since the volume was created and the IF NOT EXISTS
-- short-circuits — which is why this arrived as a red CI job rather than as a local failure.
--
-- +goose StatementBegin
DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA public;
EXCEPTION
    WHEN unique_violation OR duplicate_object THEN
        -- Another session installed it between our check and our insert. That is the outcome
        -- we wanted; only the bookkeeping collided.
        NULL;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- Deliberately NOT dropping citext: other schemas in the same database may be using it, and
-- a Down migration that can take out a colleague's schema is worse than one that leaves a
-- harmless extension behind.
SELECT 1;
