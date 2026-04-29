package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// teamWorkspaceDomainConstraint is the name of the UNIQUE index on
// teams.workspace_domain (data-model §1.2). Used to disambiguate the
// SQLSTATE 23505 unique-violation between "duplicate domain" and any
// future constraint we might add.
const teamWorkspaceDomainConstraint = "teams_workspace_domain_uniq"

// TeamRepo implements domain.TeamRepository on pgxpool.
type TeamRepo struct {
	pool *pgxpool.Pool
}

// NewTeamRepo constructs a TeamRepo bound to the given pool.
func NewTeamRepo(pool *pgxpool.Pool) *TeamRepo {
	return &TeamRepo{pool: pool}
}

// Create inserts a new team. Maps duplicate-workspace-domain SQLSTATE
// 23505 violations to domain.ErrWorkspaceDomainTaken (FR-007).
func (r *TeamRepo) Create(ctx context.Context, in domain.TeamCreate) (*domain.Team, error) {
	id := uuid.New()
	var t domain.Team
	err := r.pool.QueryRow(ctx, `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
		RETURNING id, organization_id, name, workspace_domain, created_at, updated_at
	`, id, in.OrganizationID, in.Name, in.WorkspaceDomain).Scan(
		&t.ID, &t.OrganizationID, &t.Name, &t.WorkspaceDomain, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if isWorkspaceDomainTaken(err) {
			return nil, domain.ErrWorkspaceDomainTaken
		}
		return nil, fmt.Errorf("team: create: %w", err)
	}
	return &t, nil
}

// FindByID returns the team with the given id.
func (r *TeamRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Team, error) {
	return r.findOne(ctx, "id = $1", id)
}

// FindByWorkspaceDomain returns the team that owns the given domain.
func (r *TeamRepo) FindByWorkspaceDomain(ctx context.Context, dom string) (*domain.Team, error) {
	return r.findOne(ctx, "workspace_domain = $1", dom)
}

func (r *TeamRepo) findOne(ctx context.Context, where string, arg any) (*domain.Team, error) {
	query := `
		SELECT id, organization_id, name, workspace_domain, created_at, updated_at
		FROM teams WHERE ` + where
	var t domain.Team
	err := r.pool.QueryRow(ctx, query, arg).Scan(
		&t.ID, &t.OrganizationID, &t.Name, &t.WorkspaceDomain, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("team: find: %w", err)
	}
	return &t, nil
}

// ListByOrg returns every team in the org, ordered by name ASC.
func (r *TeamRepo) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]domain.Team, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, organization_id, name, workspace_domain, created_at, updated_at
		FROM teams
		WHERE organization_id = $1
		ORDER BY name ASC
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("team: list_by_org: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Team, 0)
	for rows.Next() {
		var t domain.Team
		if err := rows.Scan(
			&t.ID, &t.OrganizationID, &t.Name, &t.WorkspaceDomain, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("team: list_by_org scan: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("team: list_by_org iter: %w", err)
	}
	return out, nil
}

// Update applies a rename and/or a workspace-domain change. Each field
// is independently optional. Workspace-domain conflicts map to
// domain.ErrWorkspaceDomainTaken.
func (r *TeamRepo) Update(ctx context.Context, id uuid.UUID, in domain.TeamUpdate) (*domain.Team, error) {
	if in.Name == nil && in.WorkspaceDomain == nil {
		// Nothing to update; just return the current row.
		return r.FindByID(ctx, id)
	}

	var t domain.Team
	err := r.pool.QueryRow(ctx, `
		UPDATE teams
		SET name             = COALESCE($1, name),
		    workspace_domain = COALESCE($2, workspace_domain),
		    updated_at       = NOW()
		WHERE id = $3
		RETURNING id, organization_id, name, workspace_domain, created_at, updated_at
	`, in.Name, in.WorkspaceDomain, id).Scan(
		&t.ID, &t.OrganizationID, &t.Name, &t.WorkspaceDomain, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		if isWorkspaceDomainTaken(err) {
			return nil, domain.ErrWorkspaceDomainTaken
		}
		return nil, fmt.Errorf("team: update: %w", err)
	}
	return &t, nil
}

// Delete removes the team row. The full team-deletion orchestration
// (member removal + session revocation + org-admin team_id NULL-out)
// is the service layer's responsibility — this method is just the
// final DELETE that orchestration wraps.
func (r *TeamRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM teams WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("team: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// isWorkspaceDomainTaken reports whether err is a unique-violation on
// the teams.workspace_domain index.
func isWorkspaceDomainTaken(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	if pgErr.Code != pgErrUniqueViolation {
		return false
	}
	return pgErr.ConstraintName == teamWorkspaceDomainConstraint
}
