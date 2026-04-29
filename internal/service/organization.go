package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// slugPattern matches valid organization slugs: lowercase alphanumeric +
// hyphens, no leading/trailing hyphen, 1–64 chars. Same shape as a tag
// key (data-model §1.1, §1.6) — one validator covers both.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// OrganizationService is the business-logic boundary for organization
// CRUD. Wraps the OrganizationRepository with input validation, slug
// normalization, and the lifecycle transitions FR-003 mandates.
//
// Admin-add / admin-remove (FR-004) live in OrgAdminsService — they
// touch memberships and sessions, which is a separate concern.
type OrganizationService struct {
	repo domain.OrganizationRepository
}

// NewOrganizationService constructs an OrganizationService.
func NewOrganizationService(repo domain.OrganizationRepository) *OrganizationService {
	return &OrganizationService{repo: repo}
}

// CreateOrgInput is the input for Create.
type CreateOrgInput struct {
	Name string
	Slug string
	// CreatedByUserID is the super-admin who issued the request.
	// FR-003: only super-admins can create organizations.
	CreatedByUserID uuid.UUID
}

// Create validates inputs, normalizes the slug, and inserts a new
// organization. Returns:
//   - domain.ErrAlreadyExists if the slug is taken;
//   - a wrapped validation error for malformed name/slug.
//
// initial_admin_emails support is intentionally NOT in this method's
// surface — the contract POST /orgs request shape includes it, but the
// orchestration (upsert user → upsert org-admin membership → revoke
// sessions if user already had a different membership) belongs in
// OrgAdminsService and is invoked separately by the handler.
func (s *OrganizationService) Create(ctx context.Context, in CreateOrgInput) (*domain.Organization, error) {
	name := strings.TrimSpace(in.Name)
	slug := strings.ToLower(strings.TrimSpace(in.Slug))

	if name == "" {
		return nil, fmt.Errorf("organization: name is required")
	}
	if !slugPattern.MatchString(slug) {
		return nil, fmt.Errorf("organization: slug must be lowercase alphanumeric with optional hyphens (1-64 chars)")
	}
	if in.CreatedByUserID == uuid.Nil {
		return nil, fmt.Errorf("organization: created_by_user_id is required")
	}

	return s.repo.Create(ctx, domain.OrganizationCreate{
		Name:            name,
		Slug:            slug,
		CreatedByUserID: in.CreatedByUserID,
	})
}

// Get fetches an organization by id, returning domain.ErrNotFound for
// unknown ids.
func (s *OrganizationService) Get(ctx context.Context, id uuid.UUID) (*domain.Organization, error) {
	return s.repo.FindByID(ctx, id)
}

// List returns organizations matching the filter plus a total count.
func (s *OrganizationService) List(ctx context.Context, filter domain.OrganizationListFilter) ([]domain.Organization, int64, error) {
	return s.repo.List(ctx, filter)
}

// UpdateOrgInput carries an optional rename and an optional status
// transition. Either or both may be zero-valued; zero-valued fields are
// ignored. The handler is responsible for matching authorization to the
// requested fields (api-routes.md §2: name-only updates allow org-admin;
// slug or status changes require super-admin — slug changes are NOT
// supported by Update because the slug is immutable post-creation per
// data-model §1.1 and OrganizationRepository.Rename's contract).
type UpdateOrgInput struct {
	Name   *string
	Status *domain.OrganizationStatus
}

// Update applies a rename and/or status transition in a fixed order.
// Returns the resulting organization. ErrNotFound if the id is unknown.
func (s *OrganizationService) Update(ctx context.Context, id uuid.UUID, in UpdateOrgInput) (*domain.Organization, error) {
	org, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("organization: name must not be blank")
		}
		org, err = s.repo.Rename(ctx, id, name)
		if err != nil {
			return nil, fmt.Errorf("organization: rename: %w", err)
		}
	}

	if in.Status != nil {
		switch *in.Status {
		case domain.OrgStatusActive, domain.OrgStatusSuspended:
			// ok
		default:
			return nil, fmt.Errorf("organization: invalid status %q", *in.Status)
		}
		if statusErr := s.repo.SetStatus(ctx, id, *in.Status); statusErr != nil {
			return nil, fmt.Errorf("organization: set_status: %w", statusErr)
		}
		// Refetch to return up-to-date row.
		org, err = s.repo.FindByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("organization: refetch after set_status: %w", err)
		}
	}

	return org, nil
}

// Suspend transitions the organization to status='suspended' (FR-003,
// FR-030). Member access is gated by the suspended-org check in the
// auth pipeline; super-admins still reach the org for recovery.
func (s *OrganizationService) Suspend(ctx context.Context, id uuid.UUID) error {
	return s.repo.SetStatus(ctx, id, domain.OrgStatusSuspended)
}

// Unsuspend transitions the organization back to status='active'.
func (s *OrganizationService) Unsuspend(ctx context.Context, id uuid.UUID) error {
	return s.repo.SetStatus(ctx, id, domain.OrgStatusActive)
}

// Delete removes the organization. The DB-level FK cascades CASCADE on
// children (teams, tags, devices, rooms, memberships) so a Delete here
// will hard-delete all dependent rows. Note: the contract's "force-
// revoke all sessions, archive rooms first" cascade is NOT implemented
// here — the current FK CASCADE simply drops the rows. A richer
// orchestrated delete (with session revocation + audit trail) lands
// alongside the org-admins service in the next commit.
//
// Returns domain.ErrNotFound if the id is unknown.
func (s *OrganizationService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}
