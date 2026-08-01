-- Migration 3: a role grant may end by itself.
--
-- The reason is a rule rather than a convenience. ADMIN is deliberately not a reader of
-- unpublished wishes — running the system is a different job from planning with it — and the
-- documented way for an administrator who genuinely has to look is to grant themselves
-- DEANS_OFFICE, visibly, where granted_at and granted_by record that they did.
--
-- That threshold only works if stepping back over it is easy. A grant that has to be taken
-- back by hand is a grant that is still there in February, and a threshold nobody undoes is a
-- threshold everybody routes around. An expiry makes "let me look at this for an hour" a
-- thing the database ends on its own.
--
-- +goose Up

ALTER TABLE person_role
    ADD COLUMN expires_at timestamptz;

COMMENT ON COLUMN person_role.expires_at IS
    'When this grant stops taking effect. NULL means it does not expire. An expired row is '
    'kept rather than deleted: it is the record that the grant was held, which is exactly '
    'what the audit question "who could see this in October" needs.';

-- Every lookup that resolves permissions filters on this column, so the index is on the shape
-- those queries have: the grants of one person, the live ones first.
--
-- Partial on the expiring rows only. The overwhelming majority of grants never expire, and an
-- index that carried them all would be a second copy of the primary key for no gain.
CREATE INDEX person_role_expiring_idx ON person_role (person_id, expires_at)
    WHERE expires_at IS NOT NULL;

-- A grant that expires before it is made is a typo, not a state.
ALTER TABLE person_role
    ADD CONSTRAINT person_role_expires_after_grant
    CHECK (expires_at IS NULL OR expires_at > granted_at);

-- +goose Down

ALTER TABLE person_role DROP CONSTRAINT person_role_expires_after_grant;
DROP INDEX person_role_expiring_idx;
ALTER TABLE person_role DROP COLUMN expires_at;
