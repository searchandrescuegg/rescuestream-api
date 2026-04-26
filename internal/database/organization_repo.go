package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// pgErrUniqueViolation is the SQLSTATE for unique_violation. We look at the
// constraint name to map specific UNIQUE conflicts to typed domain errors.
const pgErrUniqueViolation = "23505"

// OrganizationRepo implements domain.OrganizationRepository on pgxpool.
type OrganizationRepo struct {
	pool *pgxpool.Pool
}

// NewOrganizationRepo constructs an OrganizationRepo bound to the given pool.
func NewOrganizationRepo(pool *pgxpool.Pool) *OrganizationRepo {
	return &OrganizationRepo{pool: pool}
}

// Create inserts a new organization. Returns domain.ErrAlreadyExists if the
// slug is taken.
func (r *OrganizationRepo) Create(ctx context.Context, in domain.OrganizationCreate) (*domain.Organization, error) {
	id := uuid.New()
	var org domain.Organization
	err := r.pool.QueryRow(ctx, `
		INSERT INTO organizations (id, name, slug, created_by_user_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, slug, status, created_by_user_id, created_at, updated_at
	`, id, in.Name, in.Slug, in.CreatedByUserID).Scan(
		&org.ID, &org.Name, &org.Slug, &org.Status,
		&org.CreatedByUserID, &org.CreatedAt, &org.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrUniqueViolation {
			return nil, domain.ErrAlreadyExists
		}
		return nil, fmt.Errorf("organization: create: %w", err)
	}
	return &org, nil
}

// FindByID returns the organization or domain.ErrNotFound.
func (r *OrganizationRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Organization, error) {
	return r.findOne(ctx, "id = $1", id)
}

// FindBySlug returns the organization with that slug or domain.ErrNotFound.
func (r *OrganizationRepo) FindBySlug(ctx context.Context, slug string) (*domain.Organization, error) {
	return r.findOne(ctx, "slug = $1", slug)
}

func (r *OrganizationRepo) findOne(ctx context.Context, where string, arg any) (*domain.Organization, error) {
	query := `
		SELECT id, name, slug, status, created_by_user_id, created_at, updated_at
		FROM organizations WHERE ` + where
	var o domain.Organization
	err := r.pool.QueryRow(ctx, query, arg).Scan(
		&o.ID, &o.Name, &o.Slug, &o.Status,
		&o.CreatedByUserID, &o.CreatedAt, &o.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("organization: find: %w", err)
	}
	return &o, nil
}

// List returns organizations matching the filter, plus a total count.
// Search matches name and slug substrings (case-insensitive). Empty filter
// values are skipped. Limit defaults to 50, offset defaults to 0.
func (r *OrganizationRepo) List(ctx context.Context, filter domain.OrganizationListFilter) ([]domain.Organization, int64, error) {
	var (
		conds []string
		args  []any
	)

	if filter.Search != "" {
		conds = append(conds, fmt.Sprintf("(LOWER(name) LIKE $%d OR LOWER(slug) LIKE $%d)", len(args)+1, len(args)+1))
		args = append(args, "%"+strings.ToLower(filter.Search)+"%")
	}
	if filter.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, filter.Status)
	}

	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM organizations"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("organization: count: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, filter.Offset)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT id, name, slug, status, created_by_user_id, created_at, updated_at
		FROM organizations%s
		ORDER BY name ASC
		LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("organization: list: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Organization, 0)
	for rows.Next() {
		var o domain.Organization
		if err := rows.Scan(
			&o.ID, &o.Name, &o.Slug, &o.Status,
			&o.CreatedByUserID, &o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("organization: scan: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("organization: rows: %w", err)
	}
	return out, total, nil
}

// Rename updates the organization's display name.
func (r *OrganizationRepo) Rename(ctx context.Context, id uuid.UUID, name string) (*domain.Organization, error) {
	var o domain.Organization
	err := r.pool.QueryRow(ctx, `
		UPDATE organizations SET name = $1, updated_at = NOW()
		WHERE id = $2
		RETURNING id, name, slug, status, created_by_user_id, created_at, updated_at
	`, name, id).Scan(
		&o.ID, &o.Name, &o.Slug, &o.Status,
		&o.CreatedByUserID, &o.CreatedAt, &o.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("organization: rename: %w", err)
	}
	return &o, nil
}

// SetStatus suspends or reactivates an organization.
func (r *OrganizationRepo) SetStatus(ctx context.Context, id uuid.UUID, status domain.OrganizationStatus) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE organizations SET status = $1, updated_at = NOW() WHERE id = $2
	`, status, id)
	if err != nil {
		return fmt.Errorf("organization: set_status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Delete removes the organization. The DB-level FKs (ON DELETE RESTRICT
// on organizations.created_by_user_id, organization_memberships.team_id, etc.)
// will reject the delete if dependent rows exist; callers must cascade
// soft-archive children first.
func (r *OrganizationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("organization: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
