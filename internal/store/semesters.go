package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/obcode/tallox.go/internal/domain"
	"github.com/obcode/tallox.go/internal/policy"
)

// uniqueViolation is PostgreSQL's SQLSTATE for a duplicate key.
//
// Mapped rather than passed through, and the habit matters more than this one call site: the
// wish workflow will hit a uniqueness violation whose verbatim text reveals that somebody else
// has already registered interest, which is exactly what the confidentiality rule withholds.
// Here the fact is harmless — which semesters exist is not a secret — but the shape of the
// code is the same one that has to be right there.
const uniqueViolation = "23505"

// Semesters is the persistence behind domain.SemesterService.
type Semesters struct {
	q *Queries
}

// NewSemesters binds the semester queries to a pool or transaction.
func NewSemesters(db DBTX) *Semesters { return &Semesters{q: New(db)} }

var _ domain.SemesterStore = (*Semesters)(nil)

// semesterFrom reshapes a generated row into the domain type.
//
// The phase becomes a policy.Phase here and nowhere else. It is not validated on the way
// through: a phase this build does not know is a fact about the row, and the service is the
// layer that decides what to do about it. Swallowing it here — substituting a default —
// would land on DEMAND_PLANNING, which is the most permissive phase for writes.
func semesterFrom(row Semester) domain.Semester {
	return domain.Semester{
		ID:                row.ID,
		Code:              row.Code,
		Phase:             policy.Phase(row.Phase),
		WishesPublishedAt: nullableTime(row.WishesPublishedAt),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

// CreateSemester inserts a semester at the start of the process.
func (s *Semesters) CreateSemester(ctx context.Context, code string) (domain.Semester, error) {
	row, err := s.q.CreateSemester(ctx, code)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return domain.Semester{}, domain.ErrSemesterExists
		}
		return domain.Semester{}, fmt.Errorf("cannot insert semester: %w", err)
	}
	return semesterFrom(row), nil
}

// SemesterByID returns one semester, or domain.ErrNoSuchSemester.
//
// An error rather than the (nil, nil) that "not found" gets elsewhere in this codebase: every
// caller of this one has an id in hand that came from somewhere, so a missing row is a
// mistaken reference and not an empty result. The convention is worth breaking exactly where
// the caller would otherwise have to invent the error itself.
func (s *Semesters) SemesterByID(ctx context.Context, id uuid.UUID) (domain.Semester, error) {
	row, err := s.q.SemesterByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Semester{}, domain.ErrNoSuchSemester
		}
		return domain.Semester{}, fmt.Errorf("cannot read semester: %w", err)
	}
	return semesterFrom(row), nil
}

// Semesters lists them all, newest first.
func (s *Semesters) Semesters(ctx context.Context) ([]domain.Semester, error) {
	rows, err := s.q.Semesters(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot list semesters: %w", err)
	}

	out := make([]domain.Semester, 0, len(rows))
	for _, row := range rows {
		out = append(out, semesterFrom(row))
	}
	return out, nil
}

// AdvanceSemesterPhase moves a semester, but only if it is still where the caller thinks.
//
// No rows back means the phase changed under the caller. That is reported as
// domain.ErrPhaseMovedOn and not as "no such semester", because the two need different answers
// from the interface: one is a stale page that should be reloaded, the other is a broken link.
// Telling them apart costs a second query only on the failing path.
func (s *Semesters) AdvanceSemesterPhase(ctx context.Context, id uuid.UUID,
	from, to policy.Phase,
) (domain.Semester, error) {
	row, err := s.q.AdvanceSemesterPhase(ctx, AdvanceSemesterPhaseParams{
		ID:      id,
		Phase:   string(to),
		Phase_2: string(from),
	})
	if err == nil {
		return semesterFrom(row), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Semester{}, fmt.Errorf("cannot switch phase: %w", err)
	}

	if _, missing := s.SemesterByID(ctx, id); errors.Is(missing, domain.ErrNoSuchSemester) {
		return domain.Semester{}, domain.ErrNoSuchSemester
	}
	return domain.Semester{}, domain.ErrPhaseMovedOn
}

// PublishSemesterWishes ends the confidentiality window. Idempotent — see the query.
func (s *Semesters) PublishSemesterWishes(ctx context.Context, id uuid.UUID,
) (domain.Semester, error) {
	row, err := s.q.PublishSemesterWishes(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Semester{}, domain.ErrNoSuchSemester
		}
		return domain.Semester{}, fmt.Errorf("cannot publish wishes: %w", err)
	}
	return semesterFrom(row), nil
}
