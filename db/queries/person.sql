-- People and their role grants.
--
-- Every read that resolves an identity returns the roles with it, in one round trip. Two
-- queries would mean a window in which a person exists and their grants do not, and the
-- authenticator would have to decide what to do with a caller whose permissions it could not
-- read — a decision with only bad answers.

-- name: CreatePerson :one
-- The id is supplied by the caller rather than defaulted, so that a fixture, a seed and an
-- import can all say who they are creating before the insert happens.
INSERT INTO person (id, mail, name)
VALUES ($1, $2, $3)
RETURNING id, mail, name, active, created_at, updated_at;

-- name: PersonByMail :one
-- The authentication query of the browser door. mail is citext, so the comparison is
-- case-insensitive without a lower() that would defeat the unique index.
SELECT
    p.id,
    p.mail,
    p.name,
    p.active,
    COALESCE(
        array_agg(pr.role ORDER BY pr.role) FILTER (WHERE pr.role IS NOT NULL),
        ARRAY[]::text[]
    )::text[] AS roles
FROM person p
LEFT JOIN person_role pr ON pr.person_id = p.id
WHERE p.mail = $1
GROUP BY p.id;

-- name: PersonByID :one
SELECT
    p.id,
    p.mail,
    p.name,
    p.active,
    COALESCE(
        array_agg(pr.role ORDER BY pr.role) FILTER (WHERE pr.role IS NOT NULL),
        ARRAY[]::text[]
    )::text[] AS roles
FROM person p
LEFT JOIN person_role pr ON pr.person_id = p.id
WHERE p.id = $1
GROUP BY p.id;

-- name: SetPersonActive :exec
-- Deactivation is how a leaver loses access to everything at once, tokens included.
UPDATE person
SET active = $2,
    updated_at = now()
WHERE id = $1;

-- name: GrantRole :exec
-- Idempotent: granting a role somebody already holds is not an error, and the alternative
-- would push every caller into a read-then-write race.
INSERT INTO person_role (person_id, role, granted_by)
VALUES ($1, $2, $3)
ON CONFLICT (person_id, role) DO NOTHING;

-- name: RevokeRole :exec
DELETE FROM person_role
WHERE person_id = $1 AND role = $2;
