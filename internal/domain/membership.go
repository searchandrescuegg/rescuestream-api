package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// MembershipRole enumerates the per-organization role values.
type MembershipRole string

const (
	MembershipRoleOrgAdmin MembershipRole = "org-admin"
	MembershipRoleMember   MembershipRole = "member"
)

// OrganizationMembership places a user in an organization with a role
// (FR-001/FR-002). Each user has at most one active membership row across
// the platform; this is enforced by the UNIQUE(user_id) index.
type OrganizationMembership struct {
	ID             uuid.UUID      `json:"id"`
	UserID         uuid.UUID      `json:"user_id"`
	OrganizationID uuid.UUID      `json:"organization_id"`
	TeamID         *uuid.UUID     `json:"team_id,omitempty"`
	Role           MembershipRole `json:"role"`
	JoinedAt       time.Time      `json:"joined_at"`
}

// MembershipReplace carries the inputs for atomically replacing a user's
// active organization membership with a new one. Invariant FR-002 (one
// org per user) is enforced by the UNIQUE(user_id) index — a Replace
// either UPDATEs the existing row in place or INSERTs a new one.
type MembershipReplace struct {
	UserID         uuid.UUID
	OrganizationID uuid.UUID
	TeamID         *uuid.UUID // nil for org-admins without a team
	Role           MembershipRole
}

// OrganizationMemberView is the read-model returned by the members
// listing endpoint (api-routes.md §5). Joins organization_memberships
// with users so handlers don't need a per-row lookup. tag_ids stays
// empty until the tag CRUD lands.
type OrganizationMemberView struct {
	UserID         uuid.UUID      `json:"user_id"`
	Email          string         `json:"email"`
	DisplayName    *string        `json:"display_name,omitempty"`
	OrganizationID uuid.UUID      `json:"organization_id"`
	TeamID         *uuid.UUID     `json:"team_id,omitempty"`
	Role           MembershipRole `json:"role"`
	JoinedAt       time.Time      `json:"joined_at"`
	TagIDs         []uuid.UUID    `json:"tag_ids"`
}

// MemberListFilter narrows the org members listing.
type MemberListFilter struct {
	// TeamID restricts results to members of a specific team. Zero =
	// "every team in the org" (including org-admins with NULL team_id).
	TeamID *uuid.UUID
	// Search matches email or display_name substrings (case-insensitive).
	Search string
	Limit  int
	Offset int
}

// MembershipRepository is the persistence boundary for organization_memberships.
type MembershipRepository interface {
	// GetByUser returns the user's active organization membership, or
	// domain.ErrNotFound. Hot-path: every authenticated request resolves
	// this for tenancy gating.
	GetByUser(ctx context.Context, userID uuid.UUID) (*OrganizationMembership, error)

	// GetMemberInOrg returns the member view for a specific (org, user)
	// pair. Returns domain.ErrNotFound when the user has no membership
	// at all OR when their membership is in a different org.
	GetMemberInOrg(ctx context.Context, orgID, userID uuid.UUID) (*OrganizationMemberView, error)

	// ListByOrg returns the org's members joined with users, ordered
	// by display_name (NULLs last) then email, plus a total count for
	// pagination.
	ListByOrg(ctx context.Context, orgID uuid.UUID, filter MemberListFilter) ([]OrganizationMemberView, int64, error)

	// Replace atomically replaces a user's membership with the values in
	// in. If no membership exists, a new row is inserted; if one does,
	// the existing row's organization_id, team_id, role, and joined_at
	// are overwritten. Used by org-admin assignment (FR-004) and by
	// auto-join (FR-008) when a user lands in a different org.
	Replace(ctx context.Context, in MembershipReplace) (*OrganizationMembership, error)

	// DeleteByUser removes the user's membership row. Returns
	// domain.ErrNotFound if no row existed.
	DeleteByUser(ctx context.Context, userID uuid.UUID) error
}
