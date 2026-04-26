package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// OrganizationStatus enumerates the lifecycle states of an organization.
type OrganizationStatus string

const (
	OrgStatusActive    OrganizationStatus = "active"
	OrgStatusSuspended OrganizationStatus = "suspended"
)

// Organization is the top-level multi-tenant unit. Owns teams, tags,
// devices, rooms, and memberships (data-model §1.1).
type Organization struct {
	ID              uuid.UUID          `json:"id"`
	Name            string             `json:"name"`
	Slug            string             `json:"slug"`
	Status          OrganizationStatus `json:"status"`
	CreatedByUserID uuid.UUID          `json:"created_by_user_id"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// OrganizationCreate carries the inputs for creating a new organization.
type OrganizationCreate struct {
	Name            string
	Slug            string
	CreatedByUserID uuid.UUID
}

// OrganizationListFilter narrows the org list — used by super-admin
// dashboards and the typeahead search per FR-003.
type OrganizationListFilter struct {
	// Search matches name or slug substrings (case-insensitive). Empty matches all.
	Search string
	// Status filters by lifecycle state. Empty matches all.
	Status OrganizationStatus
	Limit  int
	Offset int
}

// OrganizationRepository is the persistence boundary for organizations.
type OrganizationRepository interface {
	// Create inserts a new organization. Returns ErrAlreadyExists if the
	// slug is taken.
	Create(ctx context.Context, in OrganizationCreate) (*Organization, error)

	// FindByID returns the organization or domain.ErrNotFound.
	FindByID(ctx context.Context, id uuid.UUID) (*Organization, error)

	// FindBySlug returns the organization with that slug or domain.ErrNotFound.
	FindBySlug(ctx context.Context, slug string) (*Organization, error)

	// List returns organizations matching the filter, plus a total count
	// for pagination.
	List(ctx context.Context, filter OrganizationListFilter) ([]Organization, int64, error)

	// Rename updates the display name only. Slug is immutable post-creation.
	Rename(ctx context.Context, id uuid.UUID, name string) (*Organization, error)

	// SetStatus suspends or reactivates an organization (FR-003).
	SetStatus(ctx context.Context, id uuid.UUID, status OrganizationStatus) error

	// Delete removes the organization. Returns an error if dependent rows
	// (teams, devices, rooms, members) still exist; callers must cascade
	// soft-archive first per data-model §1.1 invariants.
	Delete(ctx context.Context, id uuid.UUID) error
}
