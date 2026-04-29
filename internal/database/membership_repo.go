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

// MembershipRepo implements domain.MembershipRepository on pgxpool.
//
// Lookup-only at this stage; the mutating methods (Upsert, Remove,
// ListByOrg, etc.) land alongside the US1/US2 service work that needs
// them. Splitting the surface keeps this commit focused on the auth
// pipeline.
type MembershipRepo struct {
	pool *pgxpool.Pool
}

// NewMembershipRepo constructs a MembershipRepo bound to the given pool.
func NewMembershipRepo(pool *pgxpool.Pool) *MembershipRepo {
	return &MembershipRepo{pool: pool}
}

// GetByUser returns the user's active organization membership.
func (r *MembershipRepo) GetByUser(ctx context.Context, userID uuid.UUID) (*domain.OrganizationMembership, error) {
	var m domain.OrganizationMembership
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, organization_id, team_id, role, joined_at
		FROM organization_memberships
		WHERE user_id = $1
	`, userID).Scan(&m.ID, &m.UserID, &m.OrganizationID, &m.TeamID, &m.Role, &m.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("membership: get_by_user: %w", err)
	}
	return &m, nil
}

// Replace atomically inserts or updates the user's membership row.
// The UNIQUE(user_id) index makes this a single ON CONFLICT statement —
// no transaction needed at this level.
//
// `joined_at` is reset to NOW() on every Replace (data-model §1.4
// invariants: a re-join into a different org is a fresh membership).
func (r *MembershipRepo) Replace(ctx context.Context, in domain.MembershipReplace) (*domain.OrganizationMembership, error) {
	var m domain.OrganizationMembership
	err := r.pool.QueryRow(ctx, `
		INSERT INTO organization_memberships (id, user_id, organization_id, team_id, role)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			organization_id = EXCLUDED.organization_id,
			team_id         = EXCLUDED.team_id,
			role            = EXCLUDED.role,
			joined_at       = NOW()
		RETURNING id, user_id, organization_id, team_id, role, joined_at
	`, uuid.New(), in.UserID, in.OrganizationID, in.TeamID, in.Role).Scan(
		&m.ID, &m.UserID, &m.OrganizationID, &m.TeamID, &m.Role, &m.JoinedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("membership: replace: %w", err)
	}
	return &m, nil
}

// DeleteByUser removes the user's membership row.
func (r *MembershipRepo) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM organization_memberships WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("membership: delete_by_user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
