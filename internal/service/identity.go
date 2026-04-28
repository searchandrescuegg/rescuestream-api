package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// IdentityResolver derives a CallerIdentity from a user_id. The same
// resolver is shared across the auth pipeline and any out-of-band paths
// that need to gate by tenancy without going through HTTP middleware
// (background sweepers, CLI tools, etc.).
//
// Concretely it answers:
//   - Is the user a super-admin? → CallerRoleSuperAdmin (OrgID = uuid.Nil)
//   - Else, does the user hold an organization_memberships row?
//     → CallerRoleOrgAdmin / CallerRoleMember plus OrgID + TeamID, plus
//     the org's current status (so the suspended-org gate can run
//     without a second round-trip).
//   - Else → domain.ErrNoOrgMembership
type IdentityResolver struct {
	superAdmins   domain.SuperAdminRepository
	memberships   domain.MembershipRepository
	organizations domain.OrganizationRepository
}

// NewIdentityResolver constructs an IdentityResolver wired to the three
// authoritative repositories. There is no functional-options surface
// because the resolver has no tunable behavior — every read is on the
// hot auth path.
func NewIdentityResolver(
	superAdmins domain.SuperAdminRepository,
	memberships domain.MembershipRepository,
	organizations domain.OrganizationRepository,
) *IdentityResolver {
	return &IdentityResolver{
		superAdmins:   superAdmins,
		memberships:   memberships,
		organizations: organizations,
	}
}

// Resolve returns the CallerIdentity for the given user_id.
//
// Super-admin status takes precedence over membership: a user who is
// both a super-admin AND a member of an organization is reported as
// super-admin (their cross-org reach is what matters for gating).
//
// If the caller is neither a super-admin nor a member, returns
// domain.ErrNoOrgMembership; the middleware surfaces this as a
// /problems/no-org-membership 403.
func (r *IdentityResolver) Resolve(ctx context.Context, userID uuid.UUID) (*domain.CallerIdentity, error) {
	isSuper, err := r.superAdmins.IsSuperAdmin(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("identity.Resolve: super_admins lookup: %w", err)
	}
	if isSuper {
		return &domain.CallerIdentity{
			UserID: userID,
			Role:   domain.CallerRoleSuperAdmin,
		}, nil
	}

	m, err := r.memberships.GetByUser(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNoOrgMembership
		}
		return nil, fmt.Errorf("identity.Resolve: membership lookup: %w", err)
	}

	org, err := r.organizations.FindByID(ctx, m.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("identity.Resolve: org lookup: %w", err)
	}

	role := domain.CallerRoleMember
	if m.Role == domain.MembershipRoleOrgAdmin {
		role = domain.CallerRoleOrgAdmin
	}

	return &domain.CallerIdentity{
		UserID:    userID,
		Role:      role,
		OrgID:     m.OrganizationID,
		TeamID:    m.TeamID,
		OrgStatus: org.Status,
	}, nil
}
