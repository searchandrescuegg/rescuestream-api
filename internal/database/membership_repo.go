package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// GetMemberInOrg returns the member view for a specific (org, user)
// pair. Returns domain.ErrNotFound when no row matches.
func (r *MembershipRepo) GetMemberInOrg(ctx context.Context, orgID, userID uuid.UUID) (*domain.OrganizationMemberView, error) {
	var m domain.OrganizationMemberView
	err := r.pool.QueryRow(ctx, `
		SELECT om.user_id, u.email, u.display_name,
		       om.organization_id, om.team_id, om.role, om.joined_at
		FROM organization_memberships om
		JOIN users u ON u.id = om.user_id
		WHERE om.organization_id = $1 AND om.user_id = $2
	`, orgID, userID).Scan(
		&m.UserID, &m.Email, &m.DisplayName,
		&m.OrganizationID, &m.TeamID, &m.Role, &m.JoinedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("membership: get_member_in_org: %w", err)
	}
	// Tags stay empty until the tag schema lands; the JSON field is
	// always present so the wire shape doesn't shift.
	m.TagIDs = []uuid.UUID{}
	return &m, nil
}

// ListByOrg returns the org's members with optional team_id and
// substring filters, plus a total row count.
func (r *MembershipRepo) ListByOrg(ctx context.Context, orgID uuid.UUID, filter domain.MemberListFilter) ([]domain.OrganizationMemberView, int64, error) {
	conds := []string{"om.organization_id = $1"}
	args := []any{orgID}

	if filter.TeamID != nil {
		conds = append(conds, fmt.Sprintf("om.team_id = $%d", len(args)+1))
		args = append(args, *filter.TeamID)
	}
	if filter.Search != "" {
		args = append(args, "%"+strings.ToLower(filter.Search)+"%")
		conds = append(conds, fmt.Sprintf("(LOWER(u.email) LIKE $%d OR LOWER(COALESCE(u.display_name, '')) LIKE $%d)", len(args), len(args)))
	}
	whereClause := "WHERE " + strings.Join(conds, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM organization_memberships om
		JOIN users u ON u.id = om.user_id `+whereClause,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("membership: list_by_org count: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, filter.Offset)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT om.user_id, u.email, u.display_name,
		       om.organization_id, om.team_id, om.role, om.joined_at
		FROM organization_memberships om
		JOIN users u ON u.id = om.user_id
		%s
		ORDER BY u.display_name NULLS LAST, u.email ASC
		LIMIT $%d OFFSET $%d
	`, whereClause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("membership: list_by_org: %w", err)
	}
	defer rows.Close()

	out := make([]domain.OrganizationMemberView, 0)
	for rows.Next() {
		var m domain.OrganizationMemberView
		if err := rows.Scan(
			&m.UserID, &m.Email, &m.DisplayName,
			&m.OrganizationID, &m.TeamID, &m.Role, &m.JoinedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("membership: list_by_org scan: %w", err)
		}
		m.TagIDs = []uuid.UUID{}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("membership: list_by_org iter: %w", err)
	}
	return out, total, nil
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
