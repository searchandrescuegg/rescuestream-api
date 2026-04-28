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

// MembershipRepository is the persistence boundary for organization_memberships.
//
// The interface intentionally exposes ONLY the lookup needed by the auth
// pipeline at this stage; mutating operations (auto-join, removal,
// org-admin promotion) land alongside US1/US2 with their fuller surface.
type MembershipRepository interface {
	// GetByUser returns the user's active organization membership, or
	// domain.ErrNotFound. Hot-path: every authenticated request resolves
	// this for tenancy gating.
	GetByUser(ctx context.Context, userID uuid.UUID) (*OrganizationMembership, error)
}
