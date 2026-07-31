-- Migration 2: who may use this system, and by what credential.
--
-- Deliberately identity only. No modules, no instances, no wishes: the primary key of an
-- instance — module, module version, module + programme, module + programme + course type — is
-- the most expensive open question in the project and it is not answered yet. A released
-- migration is never edited, so writing those tables now would freeze a guess. Identity does
-- not depend on that answer, and everything above it (the auth middleware, the policy, both
-- doors) does depend on identity.
--
-- +goose Up

-- People
-- ------
--
-- One row per person who appears in the planning, whether they ever log in or not: the
-- programme lead needs to say "this instance is Prof. X's" before Prof. X has been near
-- the tool.
CREATE TABLE person (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The identity the auth proxy asserts in X-Remote-User, and the natural key of a person.
    --
    -- citext, so that Prof.Eins@example.org and prof.eins@example.org are the same person.
    -- The proxy's casing comes from the identity provider and is not ours to rely on; a
    -- case-sensitive column would silently create a second account on the day it changes,
    -- and the symptom would be "my wishes are gone".
    mail       citext NOT NULL UNIQUE,

    name       text NOT NULL,

    -- Leavers are deactivated, never deleted: their assignments stay in the history, and the
    -- audit log has to keep resolving who did what. Authentication refuses an inactive
    -- person, so deactivating is the revocation of everything at once, including every token
    -- they hold.
    active     boolean NOT NULL DEFAULT true,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- The nil UUID is what an unauthenticated caller and an unset owner column both carry, and
    -- principal.Actor.Owns refuses to match it on either side. A person who actually held it
    -- would turn that guard into a bug report instead of a defence.
    CONSTRAINT person_id_not_nil CHECK (id <> '00000000-0000-0000-0000-000000000000'::uuid),
    CONSTRAINT person_mail_not_blank CHECK (length(trim(mail::text)) > 0)
);

-- Role grants
-- -----------
--
-- Roles as rows rather than columns, because a person holds several: the same colleague leads
-- a subject group and a study programme, and every rule in internal/policy therefore asks about the
-- set rather than about "the" role.
--
-- The grant is unscoped for now. Which group a subject group lead leads becomes a question
-- the moment subject groups exist as rows, and the migration that creates them is where
-- the scoped grant belongs — as its own table, so this one never has to be edited. Nothing
-- shipped so far depends on the answer: wish confidentiality is faculty-wide.
CREATE TABLE person_role (
    person_id  uuid NOT NULL REFERENCES person (id) ON DELETE CASCADE,
    role       text NOT NULL,
    granted_at timestamptz NOT NULL DEFAULT now(),

    -- Who granted it. NULL for grants that predate a human decision — an import, a seed.
    -- ON DELETE SET NULL rather than CASCADE: losing the grantee is not a reason to lose the
    -- grant.
    granted_by uuid REFERENCES person (id) ON DELETE SET NULL,

    PRIMARY KEY (person_id, role),

    -- text with a CHECK rather than a PostgreSQL enum. Roles will grow — the kickoff already
    -- names read access for FK10, MUC.DAI, MUC.HEALTH and DPH — and widening an enum is a
    -- migration that fights the transaction goose runs it in, while widening a CHECK is one
    -- statement.
    --
    -- The list has to stay in step with internal/policy, which is the only place that gives
    -- these strings meaning. It cannot drift silently in the dangerous direction: a role the
    -- policy does not recognise grants nothing. It can drift in the annoying direction — a
    -- role the policy knows that the database rejects on write — so internal/store carries a
    -- test that reads this constraint and compares it with policy.AllRoles().
    CONSTRAINT person_role_role_known CHECK (role IN (
        'LECTURER',
        'SUBJECT_GROUP_LEAD',
        'PROGRAMME_LEAD',
        'DEANS_OFFICE',
        'ADMIN'
    ))
);

-- Personal Access Tokens
-- ----------------------
--
-- The second door. A token can never exceed its owner's role — permissions are resolved from
-- the person on every request, so revoking a role instantly demotes every token that person
-- holds, without touching a token row.
CREATE TABLE personal_access_token (
    -- The public half of the token, tallox_<token_id>_<secret>: 10 bytes of crypto/rand in
    -- Crockford base32. It is the primary key, so authentication is one indexed lookup rather
    -- than a scan that hashes every candidate row — which is what makes a constant-time
    -- comparison of the secret affordable.
    token_id     text PRIMARY KEY,

    -- RESTRICT, not CASCADE: people are deactivated rather than deleted (see person.active),
    -- and a DELETE that silently took tokens with it would remove the audit trail of what
    -- those tokens did.
    owner_id     uuid NOT NULL REFERENCES person (id) ON DELETE RESTRICT,

    -- SHA-256 of the secret half. Not argon2id, and that is a considered choice rather than a
    -- shortcut: a KDF defends weak secrets against offline brute force, and this secret is 32
    -- bytes of crypto/rand — there is nothing to brute force. What a KDF would cost is ~50 ms
    -- of CPU and tens of megabytes of RAM on *every* API call, which turns a colleague's
    -- 500-query evaluation script into 25 seconds of server CPU and a trivial memory
    -- exhaustion vector from inside the VPN.
    --
    -- The precondition this rests on: the server always generates the secret. There is no
    -- "bring your own token" path. If that ever changes, this choice has to change with it.
    secret_hash  bytea NOT NULL,

    -- What the token is for, in the owner's words. Shown in the token list so that revoking
    -- the right one does not require guessing.
    description  text NOT NULL DEFAULT '',

    -- area:verb grants. Enforcement is schema-driven and arrives with the @scope directive;
    -- the column exists now so that issuing tokens does not have to be revisited then.
    scopes       text[] NOT NULL DEFAULT '{}',

    created_at   timestamptz NOT NULL DEFAULT now(),

    -- NOT NULL on purpose: there is no such thing as a token that never expires. The default
    -- is 90 days and the ceiling 365, both enforced where tokens are minted rather than here,
    -- because a CHECK against now() is not immutable.
    expires_at   timestamptz NOT NULL,

    -- Written asynchronously and at most every few minutes — a row lock plus a WAL write per
    -- API call would serialise the requests of a single script.
    last_used_at timestamptz,

    -- Revocation is a timestamp, not a DELETE. The audit log has to keep resolving the token
    -- id long after the token stops working.
    revoked_at   timestamptz,

    CONSTRAINT personal_access_token_expires_after_creation CHECK (expires_at > created_at),
    CONSTRAINT personal_access_token_secret_hash_is_sha256 CHECK (length(secret_hash) = 32)
);

-- "Which tokens do I hold" — the token list in the GUI, and the query a revocation starts
-- from. The primary key covers authentication itself.
CREATE INDEX personal_access_token_owner_idx ON personal_access_token (owner_id);

-- +goose Down

DROP TABLE personal_access_token;
DROP TABLE person_role;
DROP TABLE person;
