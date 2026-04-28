package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// SuperAdminRepo implements domain.SuperAdminRepository on pgxpool.
type SuperAdminRepo struct {
	pool *pgxpool.Pool
}

// NewSuperAdminRepo constructs a SuperAdminRepo bound to the given pool.
func NewSuperAdminRepo(pool *pgxpool.Pool) *SuperAdminRepo {
	return &SuperAdminRepo{pool: pool}
}

// IsSuperAdmin reports whether the user holds a super_admins row.
func (r *SuperAdminRepo) IsSuperAdmin(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM super_admins WHERE user_id = $1)
	`, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("super_admin: is_super: %w", err)
	}
	return exists, nil
}

// Get returns the super-admin row for the given user, or domain.ErrNotFound.
func (r *SuperAdminRepo) Get(ctx context.Context, userID uuid.UUID) (*domain.SuperAdmin, error) {
	var s domain.SuperAdmin
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, granted_by_user_id, granted_at, seeded_from_env
		FROM super_admins WHERE user_id = $1
	`, userID).Scan(&s.ID, &s.UserID, &s.GrantedByUserID, &s.GrantedAt, &s.SeededFromEnv)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("super_admin: get: %w", err)
	}
	return &s, nil
}

// List returns every super-admin row, ordered by granted_at ASC.
func (r *SuperAdminRepo) List(ctx context.Context) ([]domain.SuperAdmin, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, granted_by_user_id, granted_at, seeded_from_env
		FROM super_admins
		ORDER BY granted_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("super_admin: list: %w", err)
	}
	defer rows.Close()

	out := make([]domain.SuperAdmin, 0)
	for rows.Next() {
		var s domain.SuperAdmin
		if err := rows.Scan(&s.ID, &s.UserID, &s.GrantedByUserID, &s.GrantedAt, &s.SeededFromEnv); err != nil {
			return nil, fmt.Errorf("super_admin: list scan: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("super_admin: list iter: %w", err)
	}
	return out, nil
}

// Add inserts a new super-admin row. Idempotent.
func (r *SuperAdminRepo) Add(ctx context.Context, userID uuid.UUID, grantedBy *uuid.UUID, seededFromEnv bool) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO super_admins (id, user_id, granted_by_user_id, seeded_from_env)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO NOTHING
	`, uuid.New(), userID, grantedBy, seededFromEnv)
	if err != nil {
		return fmt.Errorf("super_admin: add: %w", err)
	}
	return nil
}

// Remove deletes the super-admin row for the given user. Returns
// domain.ErrNotFound if no row existed. The last-super-admin guard lives
// in the service layer (it MUST run inside the same transaction that
// performs the delete to avoid TOCTOU races).
func (r *SuperAdminRepo) Remove(ctx context.Context, userID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM super_admins WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("super_admin: remove: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// CountRemaining returns the current super-admin row count.
func (r *SuperAdminRepo) CountRemaining(ctx context.Context) (int64, error) {
	var n int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM super_admins`).Scan(&n); err != nil {
		return 0, fmt.Errorf("super_admin: count: %w", err)
	}
	return n, nil
}
