package domain

import "errors"

// Domain errors
var (
	// ErrNotFound indicates the requested entity was not found.
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists indicates an entity with the same unique key already exists.
	ErrAlreadyExists = errors.New("already exists")

	// ErrInvalidStatus indicates an invalid status transition was attempted.
	ErrInvalidStatus = errors.New("invalid status")

	// ErrUnauthorized indicates the request is not authorized.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrAdminRequired indicates admin privileges are required.
	ErrAdminRequired = errors.New("admin privileges required")

	// ErrForbidden indicates the action is forbidden for the current user.
	ErrForbidden = errors.New("forbidden")

	// --- 003-multi-tenant-platform additions ---

	// ErrNoOrgMembership indicates the authenticated user has no active
	// organization membership and no super-admin record (FR-029).
	ErrNoOrgMembership = errors.New("no organization membership")

	// ErrNotInOrg indicates the caller's organization does not match the
	// target resource's organization (FR-028).
	ErrNotInOrg = errors.New("resource belongs to a different organization")

	// ErrACLDenied indicates the caller's attributes did not satisfy the
	// room's access control list (FR-020).
	ErrACLDenied = errors.New("access denied by ACL")

	// ErrRoomArchived indicates a write attempt against an archived room
	// (FR-023).
	ErrRoomArchived = errors.New("room is archived")

	// ErrStaleRoomVersion indicates an optimistic-concurrency conflict on
	// a room mutation (FR-024a). The HTTP layer surfaces the current
	// version in problem instance metadata.
	ErrStaleRoomVersion = errors.New("stale room version")

	// ErrDeviceKeyRevoked indicates an authentication attempt with a
	// revoked device key (FR-017).
	ErrDeviceKeyRevoked = errors.New("device key revoked")

	// ErrSessionInvalidated indicates the caller's session has been
	// revoked (FR-030a).
	ErrSessionInvalidated = errors.New("session invalidated")

	// ErrWorkspaceDomainTaken indicates a different team has already
	// claimed the workspace domain (FR-007).
	ErrWorkspaceDomainTaken = errors.New("workspace domain already taken")

	// ErrLastSuperAdmin indicates the operation would remove the last
	// remaining super-admin (FR-005).
	ErrLastSuperAdmin = errors.New("cannot remove the last super-admin")

	// ErrRetiredEndpoint indicates a v1 endpoint that has been retired in
	// favor of a v2 replacement.
	ErrRetiredEndpoint = errors.New("endpoint retired")

	// ErrOrgSuspended indicates the target organization is suspended
	// (FR-030).
	ErrOrgSuspended = errors.New("organization is suspended")
)
