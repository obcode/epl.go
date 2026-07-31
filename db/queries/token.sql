-- Personal Access Tokens: the second door.

-- name: CreateToken :one
-- The secret is generated and hashed by the caller (internal/auth), never here: a secret that
-- travelled through a query is a secret in a log somewhere.
INSERT INTO personal_access_token (
    token_id, owner_id, secret_hash, description, scopes, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING token_id, owner_id, description, scopes, created_at, expires_at,
          last_used_at, revoked_at;

-- name: TokenByID :one
-- The authentication query of the token door, in one round trip: the token, its owner and the
-- owner's roles.
--
-- Permissions come from the person on every request, which is what makes "a token can never
-- exceed its owner's role" true by construction — revoking a role demotes every token that
-- person holds without touching a token row.
--
-- Deliberately returns expired, revoked and inactive rows instead of filtering them out.
-- Authentication has to tell "this token does not exist" from "this token expired" — the
-- caller owns the token and can be told why it stopped working — and a query that filters
-- would collapse both into the same silence. It also keeps the timing of the two cases
-- similar, which a WHERE clause on revoked_at would not.
SELECT
    t.token_id,
    t.owner_id,
    t.secret_hash,
    t.scopes,
    t.expires_at,
    t.revoked_at,
    p.mail,
    p.name,
    p.active,
    COALESCE(
        array_agg(pr.role ORDER BY pr.role) FILTER (WHERE pr.role IS NOT NULL),
        ARRAY[]::text[]
    )::text[] AS roles
FROM personal_access_token t
JOIN person p ON p.id = t.owner_id
LEFT JOIN person_role pr ON pr.person_id = p.id
WHERE t.token_id = $1
GROUP BY t.token_id, p.id;

-- name: ListTokensOfPerson :many
-- The token list in the GUI. The secret hash is not in the projection: nothing outside
-- authentication has a use for it, and a column that is never selected cannot be logged.
SELECT token_id, owner_id, description, scopes, created_at, expires_at,
       last_used_at, revoked_at
FROM personal_access_token
WHERE owner_id = $1
ORDER BY created_at DESC;

-- name: MarkTokenUsed :exec
-- Coarse on purpose. "Last used" is answering "is this token still in use, can I revoke it",
-- and five-minute resolution answers that as well as microsecond resolution would.
--
-- The guard is the point: without it, every API call takes a row lock and writes WAL, which
-- serialises the requests of a single script against itself and makes the busiest tokens the
-- slowest ones.
UPDATE personal_access_token
SET last_used_at = now()
WHERE token_id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '5 minutes');

-- name: RevokeToken :exec
-- A timestamp, not a DELETE: the audit log has to keep resolving this token id afterwards.
-- Idempotent — revoking twice keeps the first moment, which is the one that matters.
UPDATE personal_access_token
SET revoked_at = now()
WHERE token_id = $1 AND revoked_at IS NULL;
