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
