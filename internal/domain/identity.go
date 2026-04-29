package domain

import "github.com/google/uuid"

// CallerRole tags the resolved role of the caller for identity-routing
// decisions. Distinct from MembershipRole because super-admins do not
// have a membership row but are still callers.
type CallerRole string

const (
	CallerRoleSuperAdmin CallerRole = "super-admin"
	CallerRoleOrgAdmin   CallerRole = "org-admin"
	CallerRoleMember     CallerRole = "member"
)

// CallerIdentity is the resolved tenancy + role triple for an
// authenticated request. Populated by the auth pipeline and consumed by
// every protected handler that needs to gate resource access by org.
//
// Super-admins are admitted with OrgID == uuid.Nil and TeamID == nil;
// they bypass tenancy at the service-layer entry points.
type CallerIdentity struct {
	UserID    uuid.UUID
	Role      CallerRole
	OrgID     uuid.UUID  // zero for super-admins without a membership
	TeamID    *uuid.UUID // nil for super-admins or org-admins without a team
	OrgStatus OrganizationStatus
}

// IsSuperAdmin reports whether the caller is a super-admin.
func (c *CallerIdentity) IsSuperAdmin() bool {
	return c != nil && c.Role == CallerRoleSuperAdmin
}
