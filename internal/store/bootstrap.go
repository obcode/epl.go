package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BootstrapAdmin creates the very first person, with the ADMIN role, if there is no person at
// all. It reports whether it created one.
//
// # Why this exists
//
// Both doors need a person row. The browser door resolves X-Remote-User against the person
// table, and a Personal Access Token belongs to a person — so on a freshly created database
// nobody can sign in, and nobody can be given a token either, because handing out tokens is
// itself something only a signed-in administrator can do. A new installation is locked from
// the outside with the key on the inside.
//
// The alternative is what happened the first time: somebody connects to production with psql
// and writes the row by hand, at the exact moment the system is new and nobody is sure what
// the schema looks like.
//
// # Why it cannot be abused
//
// The insert carries WHERE NOT EXISTS (SELECT 1 FROM person). On any database that has ever
// had a person in it, the statement does nothing — so the flag that calls this cannot promote
// an existing account, cannot re-grant ADMIN to somebody who lost it, and cannot be used
// against a running installation at all. It is a first-boot mechanism and it stops working
// permanently the moment it has done its job.
//
// The role grant runs in the same transaction as the insert. An administrator without the
// ADMIN role would be the one outcome worse than no administrator: the row exists, so the
// bootstrap never runs again, and there is still nobody who can administer anything.
func BootstrapAdmin(ctx context.Context, pool *pgxpool.Pool, mail, name string) (bool, error) {
	if mail == "" {
		return false, errors.New("bootstrap admin: no mail address given")
	}
	if name == "" {
		name = mail
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("cannot begin the bootstrap transaction: %w", err)
	}
	// Rollback after a successful commit is a no-op, so this needs no branching.
	defer func() { _ = tx.Rollback(ctx) }()

	q := New(tx)

	person, err := q.BootstrapAdmin(ctx, BootstrapAdminParams{
		ID:   uuid.New(),
		Mail: mail,
		Name: name,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The table is not empty. Not an error: this is the ordinary case on every restart
		// after the first, and the flag is expected to stay set in the deploy configuration.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("cannot create the first person: %w", err)
	}

	if err := q.GrantRole(ctx, GrantRoleParams{
		PersonID: person.ID,
		Role:     roleAdmin,
	}); err != nil {
		return false, fmt.Errorf("cannot grant ADMIN to the first person: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("cannot commit the bootstrap: %w", err)
	}
	return true, nil
}

// roleAdmin is the one role name this package needs to know.
//
// Spelled out rather than imported from internal/policy, because the dependency would run
// storage → policy for a single string, and the CHECK constraint in the migration is what
// actually validates it: a typo here fails the insert rather than granting something
// meaningless. The three-way agreement between schema, policy and constraint is tested
// elsewhere.
const roleAdmin = "ADMIN"
