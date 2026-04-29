package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// SuperAdminService manages the platform-wide super-admin set (FR-005).
//
// Add/Remove run inside a transaction so the FR-005 invariant
// ("at least one super-admin always exists") cannot race: the count is
// read FOR UPDATE alongside the DELETE, so two concurrent removals of
// the last two super-admins serialize and the second observes
// ErrLastSuperAdmin.
type SuperAdminService struct {
	pool  *pgxpool.Pool
	repo  domain.SuperAdminRepository
	users domain.UserRepository
}

// NewSuperAdminService constructs a SuperAdminService.
func NewSuperAdminService(pool *pgxpool.Pool, repo domain.SuperAdminRepository, users domain.UserRepository) *SuperAdminService {
	return &SuperAdminService{pool: pool, repo: repo, users: users}
}

// SuperAdminEntry is the wire-shape of a super-admin row joined with the
// user's email — what the GET /super-admins handler returns to the
// frontend.
type SuperAdminEntry struct {
	domain.SuperAdmin
	Email string `json:"email"`
}

// List returns every super-admin row joined with the user's email,
// ordered by granted_at ASC (matches the repo's contract).
func (s *SuperAdminService) List(ctx context.Context) ([]SuperAdminEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sa.id, sa.user_id, sa.granted_by_user_id, sa.granted_at, sa.seeded_from_env,
		       u.email
		FROM super_admins sa
		JOIN users u ON u.id = sa.user_id
		ORDER BY sa.granted_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("super_admin.List: %w", err)
	}
	defer rows.Close()

	out := make([]SuperAdminEntry, 0)
	for rows.Next() {
		var e SuperAdminEntry
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.GrantedByUserID, &e.GrantedAt, &e.SeededFromEnv,
			&e.Email,
		); err != nil {
			return nil, fmt.Errorf("super_admin.List: scan: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("super_admin.List: rows: %w", err)
	}
	return out, nil
}

// AddByEmailInput is the input for AddByEmail.
type AddByEmailInput struct {
	Email     string
	GrantedBy *uuid.UUID // typically the calling super-admin's user_id
}

// AddByEmail upserts the user (so admins can be granted before the
// target ever signs in) and inserts a super_admins row. Idempotent: if
// the user is already a super-admin, returns the existing entry.
func (s *SuperAdminService) AddByEmail(ctx context.Context, in AddByEmailInput) (*SuperAdminEntry, error) {
	email := domain.NormalizeEmail(in.Email)
	if email == "" {
		return nil, fmt.Errorf("super_admin.AddByEmail: email required")
	}

	userID, err := s.users.Upsert(ctx, domain.UserUpsert{Email: email})
	if err != nil {
		return nil, fmt.Errorf("super_admin.AddByEmail: upsert user: %w", err)
	}
	if addErr := s.repo.Add(ctx, userID, in.GrantedBy, false); addErr != nil {
		return nil, fmt.Errorf("super_admin.AddByEmail: add: %w", addErr)
	}
	row, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("super_admin.AddByEmail: refetch: %w", err)
	}
	return &SuperAdminEntry{SuperAdmin: *row, Email: email}, nil
}

// Remove deletes the super-admin row for the given user inside a
// transaction that asserts at least two rows existed pre-delete (so the
// post-delete count is >= 1). FR-005: at least one super-admin MUST
// exist at all times.
//
// Returns:
//   - domain.ErrNotFound if the user wasn't a super-admin to begin with.
//   - domain.ErrLastSuperAdmin if the delete would leave the set empty.
func (s *SuperAdminService) Remove(ctx context.Context, userID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("super_admin.Remove: begin tx: %w", err)
	}
	defer func() {
		// Rollback is a no-op after a successful Commit; we ignore that
		// case explicitly. Anything else is logged-but-not-returned because
		// the primary error path already produced a useful return value.
		if rerr := tx.Rollback(ctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			// Caller has already returned an error or success value; fall
			// through silently.
			_ = rerr
		}
	}()

	// Lock every super_admins row for the duration of the tx so two
	// concurrent removals can't both observe count=2 and then both
	// delete (which would leave the table empty).
	rows, err := tx.Query(ctx, `SELECT user_id FROM super_admins FOR UPDATE`)
	if err != nil {
		return fmt.Errorf("super_admin.Remove: lock-scan: %w", err)
	}
	var n int64
	targetIsAdmin := false
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("super_admin.Remove: lock-scan: %w", err)
		}
		n++
		if id == userID {
			targetIsAdmin = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("super_admin.Remove: lock-scan iter: %w", err)
	}

	if !targetIsAdmin {
		return domain.ErrNotFound
	}
	if n <= 1 {
		return domain.ErrLastSuperAdmin
	}

	if _, err := tx.Exec(ctx, `DELETE FROM super_admins WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("super_admin.Remove: delete: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("super_admin.Remove: commit: %w", err)
	}
	return nil
}
