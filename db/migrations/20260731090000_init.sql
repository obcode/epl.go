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
CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA public;

-- +goose Down

-- Deliberately NOT dropping citext: other schemas in the same database may be using it, and
-- a Down migration that can take out a colleague's schema is worse than one that leaves a
-- harmless extension behind.
SELECT 1;
