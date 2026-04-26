package domain

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

// User is a human identity backed by a Google account. One user holds at
// most one organization membership at any given time (FR-002).
type User struct {
	ID            uuid.UUID  `json:"id"`
	Email         string     `json:"email"`
	GoogleSubject *string    `json:"google_subject,omitempty"`
	DisplayName   *string    `json:"display_name,omitempty"`
	AvatarURL     *string    `json:"avatar_url,omitempty"`
	LastLoginAt   *time.Time `json:"last_login_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// NormalizeEmail lowercases and trims an email address. Used at every
// write boundary so the UNIQUE(email) index is case-insensitive in
// practice without requiring a citext extension.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// UserUpsert carries the inputs for an upsert-on-login operation. When a
// matching google_subject row exists, fields are updated; otherwise a row
// is inserted (matching by email if google_subject is unknown). See
// data-model §1.3 invariants.
type UserUpsert struct {
	Email         string
	GoogleSubject *string
	DisplayName   *string
	AvatarURL     *string
}

// UserRepository is the persistence boundary for users.
type UserRepository interface {
	// Upsert inserts a new user or updates an existing row matched first by
	// google_subject (when provided), then by email. Returns the resulting
	// row's id. The implementation MUST be safe to call concurrently for
	// the same email.
	Upsert(ctx context.Context, in UserUpsert) (uuid.UUID, error)

	// FindByID returns the user with the given id, or domain.ErrNotFound.
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)

	// FindByEmail returns the user with the given (normalized) email, or
	// domain.ErrNotFound.
	FindByEmail(ctx context.Context, email string) (*User, error)

	// FindByGoogleSubject returns the user with the given Google sub
	// claim, or domain.ErrNotFound.
	FindByGoogleSubject(ctx context.Context, sub string) (*User, error)

	// TouchLastLogin updates the user's last_login_at timestamp to now.
	TouchLastLogin(ctx context.Context, id uuid.UUID) error
}
