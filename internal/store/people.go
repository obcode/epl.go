package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/obcode/tallox.go/internal/domain"
	"github.com/obcode/tallox.go/internal/policy"
)

// People is the persistence behind domain.PeopleService.
//
// It takes a pool rather than a DBTX, unlike Tokens and Directory, because two of its
// operations are not single statements: removing an administrator has to lock, check and
// write in one transaction, and a type that could be handed a DBTX would be a type that
// cannot begin one. Making that visible in the constructor is better than discovering it at
// the call site.
type People struct {
	pool *pgxpool.Pool
}

// NewPeople binds user administration to a pool.
func NewPeople(pool *pgxpool.Pool) *People { return &People{pool: pool} }

var _ domain.PeopleStore = (*People)(nil)

// ListPeople returns the people this installation knows.
func (p *People) ListPeople(ctx context.Context, search string,
	includeInactive bool) ([]domain.Person, error) {
	var searchArg *string
	if search != "" {
		searchArg = &search
	}

	rows, err := New(p.pool).ListPeople(ctx, ListPeopleParams{
		Search:          searchArg,
		IncludeInactive: includeInactive,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot list people: %w", err)
	}

	people := make([]domain.Person, 0, len(rows))
	for _, row := range rows {
		people = append(people, domain.Person{
			ID:     row.ID,
			Mail:   row.Mail,
			Name:   row.Name,
			Active: row.Active,
			Roles:  knownRoles(row.Roles),
		})
	}
	return people, nil
}

// PersonByID resolves one person. "Not found" is (nil, nil), the convention throughout this
// repository.
func (p *People) PersonByID(ctx context.Context, id uuid.UUID) (*domain.Person, error) {
	row, err := New(p.pool).PersonByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read person: %w", err)
	}
	return &domain.Person{
		ID:     row.ID,
		Mail:   row.Mail,
		Name:   row.Name,
		Active: row.Active,
		Roles:  knownRoles(row.Roles),
	}, nil
}

// PersonByMail resolves one person by the address the proxy asserts.
func (p *People) PersonByMail(ctx context.Context, mail string) (*domain.Person, error) {
	row, err := New(p.pool).PersonByMail(ctx, mail)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read person by mail: %w", err)
	}
	return &domain.Person{
		ID:     row.ID,
		Mail:   row.Mail,
		Name:   row.Name,
		Active: row.Active,
		Roles:  knownRoles(row.Roles),
	}, nil
}

// CreatePerson adds somebody, with no roles.
func (p *People) CreatePerson(ctx context.Context, mail, name string) (*domain.Person, error) {
	row, err := New(p.pool).CreatePerson(ctx, CreatePersonParams{
		ID:   uuid.New(),
		Mail: mail,
		Name: name,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot create person: %w", err)
	}
	return &domain.Person{
		ID:     row.ID,
		Mail:   row.Mail,
		Name:   row.Name,
		Active: row.Active,
		Roles:  []policy.Role{},
	}, nil
}

// SetPersonName renames somebody.
func (p *People) SetPersonName(ctx context.Context, id uuid.UUID, name string) error {
	if err := New(p.pool).SetPersonName(ctx, SetPersonNameParams{ID: id, Name: name}); err != nil {
		return fmt.Errorf("cannot rename person: %w", err)
	}
	return nil
}

// SetPersonActive activates or deactivates somebody, refusing to deactivate the last
// administrator.
//
// Deactivating is a permission change even though it writes the person table: an inactive
// person fails authentication on both doors, so deactivating the only administrator is
// exactly as final as revoking the only ADMIN grant. Both paths therefore take the same lock
// and ask the same question.
func (p *People) SetPersonActive(ctx context.Context, id uuid.UUID, active bool) error {
	if active {
		// Activating can only ever add an administrator. No guard, no transaction.
		if err := New(p.pool).SetPersonActive(ctx, SetPersonActiveParams{
			ID: id, Active: true,
		}); err != nil {
			return fmt.Errorf("cannot activate person: %w", err)
		}
		return nil
	}

	return p.guardingAdmins(ctx, id, func(q *Queries) error {
		if err := q.SetPersonActive(ctx, SetPersonActiveParams{ID: id, Active: false}); err != nil {
			return fmt.Errorf("cannot deactivate person: %w", err)
		}
		return nil
	})
}

// GrantRole grants a role, optionally with an expiry.
//
// No guard: a grant can only ever add somebody who may administer the installation.
func (p *People) GrantRole(ctx context.Context, personID uuid.UUID, role policy.Role,
	grantedBy uuid.UUID, expiresAt time.Time) error {
	err := New(p.pool).GrantRole(ctx, GrantRoleParams{
		PersonID:  personID,
		Role:      string(role),
		GrantedBy: uuid.NullUUID{UUID: grantedBy, Valid: grantedBy != uuid.Nil},
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: !expiresAt.IsZero()},
	})
	if err != nil {
		return fmt.Errorf("cannot grant role: %w", err)
	}
	return nil
}

// RevokeRole withdraws a role, refusing to withdraw the last ADMIN grant.
func (p *People) RevokeRole(ctx context.Context, personID uuid.UUID, role policy.Role) error {
	if role != policy.RoleAdmin {
		if err := New(p.pool).RevokeRole(ctx, RevokeRoleParams{
			PersonID: personID, Role: string(role),
		}); err != nil {
			return fmt.Errorf("cannot revoke role: %w", err)
		}
		return nil
	}

	return p.guardingAdmins(ctx, personID, func(q *Queries) error {
		if err := q.RevokeRole(ctx, RevokeRoleParams{
			PersonID: personID, Role: string(policy.RoleAdmin),
		}); err != nil {
			return fmt.Errorf("cannot revoke ADMIN: %w", err)
		}
		return nil
	})
}

// RoleGrants returns one person's grants, expired ones included.
func (p *People) RoleGrants(ctx context.Context, personID uuid.UUID) ([]domain.RoleGrant, error) {
	rows, err := New(p.pool).RoleGrantsByPerson(ctx, personID)
	if err != nil {
		return nil, fmt.Errorf("cannot read role grants: %w", err)
	}

	grants := make([]domain.RoleGrant, 0, len(rows))
	for _, row := range rows {
		role, known := policy.ParseRole(row.Role)
		if !known {
			// A grant the policy cannot interpret grants nothing, and showing it in the
			// administration screen as if it did would be the one misleading thing this
			// list can do. It is still in the table, and the drift test in this package is
			// what makes it visible.
			continue
		}
		var grantedBy uuid.UUID
		if row.GrantedBy.Valid {
			grantedBy = row.GrantedBy.UUID
		}
		grants = append(grants, domain.RoleGrant{
			Role:      role,
			GrantedAt: row.GrantedAt,
			GrantedBy: grantedBy,
			ExpiresAt: nullableTime(row.ExpiresAt),
		})
	}
	return grants, nil
}

// guardingAdmins runs write inside a transaction that has first established that it does not
// remove the last administrator.
//
// The order is the whole point: lock, then read, then write. Reading before locking answers a
// question about a state another transaction is allowed to change in the meantime, which is
// exactly how two administrators remove each other simultaneously and both succeed.
//
// One condition serves both callers, because it is the same condition. Revoking somebody's
// ADMIN and deactivating them are both fatal in precisely the case where that person is
// currently an active administrator and there is no other one — and in every other case both
// are harmless. A person who does not hold ADMIN, or who is already inactive, is not the
// thing standing between this installation and nobody being able to get in.
func (p *People) guardingAdmins(ctx context.Context, personID uuid.UUID,
	write func(q *Queries) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("cannot begin the transaction: %w", err)
	}
	// Rollback after a successful commit is a no-op, so this needs no branching.
	defer func() { _ = tx.Rollback(ctx) }()

	q := New(tx)

	if err := q.LockAdminGrants(ctx); err != nil {
		return fmt.Errorf("cannot lock the administrator grants: %w", err)
	}

	others, err := q.CountOtherActiveAdmins(ctx, personID)
	if err != nil {
		return fmt.Errorf("cannot count the other administrators: %w", err)
	}
	if others == 0 {
		person, err := q.PersonByID(ctx, personID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Nobody to protect. The write below will be a no-op too.
		case err != nil:
			return fmt.Errorf("cannot read person: %w", err)
		case person.Active && hasRole(person.Roles, string(policy.RoleAdmin)):
			return domain.ErrLastAdmin
		}
	}

	if err := write(q); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("cannot commit: %w", err)
	}
	return nil
}

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

// knownRoles drops grants internal/policy does not recognise, exactly as policy.RolesOf does
// on the authentication path.
//
// The same rule in both places, and it has to be: a role that grants nothing when a request is
// judged must not appear in the administration screen as though it granted something. That
// would be an interface telling an administrator they have configured access they have not.
func knownRoles(raw []string) []policy.Role {
	out := make([]policy.Role, 0, len(raw))
	for _, s := range raw {
		if r, ok := policy.ParseRole(s); ok {
			out = append(out, r)
		}
	}
	return out
}
