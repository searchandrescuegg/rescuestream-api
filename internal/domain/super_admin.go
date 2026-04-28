package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SuperAdmin is a platform-wide role that transcends any organization
// boundary (FR-005). Stored as a join row to a User. The set MUST never
// shrink to zero; the service layer enforces this on Remove.
type SuperAdmin struct {
	ID              uuid.UUID  `json:"id"`
	UserID          uuid.UUID  `json:"user_id"`
	GrantedByUserID *uuid.UUID `json:"granted_by_user_id,omitempty"`
	GrantedAt       time.Time  `json:"granted_at"`
	SeededFromEnv   bool       `json:"seeded_from_env"`
}

// SuperAdminRepository is the persistence boundary for super_admins.
type SuperAdminRepository interface {
	// IsSuperAdmin reports whether the given user holds a super-admin row.
	// Hot-path: called on every authenticated request.
	IsSuperAdmin(ctx context.Context, userID uuid.UUID) (bool, error)

	// Get returns the super-admin row for the given user, or ErrNotFound.
	Get(ctx context.Context, userID uuid.UUID) (*SuperAdmin, error)

	// List returns every super-admin row, ordered by granted_at ASC.
	List(ctx context.Context) ([]SuperAdmin, error)

	// Add inserts a new super-admin row. Idempotent: if the user already
	// holds a row, returns nil with no changes.
	Add(ctx context.Context, userID uuid.UUID, grantedBy *uuid.UUID, seededFromEnv bool) error

	// Remove deletes the super-admin row for the given user. Returns
	// ErrNotFound if no row existed. Callers MUST gate this with a
	// CountRemaining > 1 check (FR-005 / ErrLastSuperAdmin).
	Remove(ctx context.Context, userID uuid.UUID) error

	// CountRemaining returns the current super-admin row count.
	CountRemaining(ctx context.Context) (int64, error)
}
