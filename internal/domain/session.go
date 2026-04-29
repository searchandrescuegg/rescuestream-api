package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Session is a server-side authentication session backed by a per-session
// HMAC secret (research §3). Replaces the v1 shared-API_SECRET model and
// enables admin-initiated invalidation (FR-030a, FR-030b).
type Session struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	HMACKeyID      string // sent as X-API-Key on every request
	HMACSecretHash string // peppered HMAC-SHA256 of the secret (research §1)
	CreatedAt      time.Time
	ExpiresAt      time.Time
	LastUsedAt     time.Time
	RevokedAt      *time.Time
	RevokedReason  *string // e.g. "self-logout", "admin-force-logout", "role-changed"
	ClientIP       *string // INET — captured at creation for forensics
	UserAgent      *string
}

// Valid reports whether the session is currently usable: not revoked AND
// not expired.
func (s *Session) Valid() bool {
	return s.RevokedAt == nil && s.ExpiresAt.After(time.Now())
}

// SessionCreate carries the inputs for inserting a new session row.
// The HMAC plaintext is generated and hashed by the service layer; only
// the hash reaches the repository.
type SessionCreate struct {
	UserID         uuid.UUID
	HMACKeyID      string
	HMACSecretHash string
	ExpiresAt      time.Time
	ClientIP       *string
	UserAgent      *string
}

// SessionRevokeReason enumerates the canonical reasons recorded on
// session.revoked_reason. Free-form strings are also accepted, but
// callers should prefer these constants for greppability.
const (
	SessionRevokeReasonSelfLogout        = "self-logout"
	SessionRevokeReasonAdminForceLogout  = "admin-force-logout"
	SessionRevokeReasonRoleChanged       = "role-changed"
	SessionRevokeReasonMembershipRemoved = "member-removed"
	SessionRevokeReasonOrgSuspended      = "org-suspended"
	SessionRevokeReasonTeamDeleted       = "team-deleted"
)

// SessionRepository is the persistence boundary for sessions.
type SessionRepository interface {
	// Create inserts a new session row. Caller-supplied id, key_id, and
	// secret_hash; this is so the service layer can deterministically
	// return the plaintext pair to the user once.
	Create(ctx context.Context, in SessionCreate) (*Session, error)

	// FindByKeyID returns the session matching the given X-API-Key, or
	// domain.ErrNotFound. The returned Session carries the hashed secret
	// for verification — the plaintext never leaves the service layer.
	FindByKeyID(ctx context.Context, keyID string) (*Session, error)

	// Touch updates last_used_at to NOW() and slides expires_at to
	// NOW() + slidingExpiry. Safe to call on every successful auth.
	Touch(ctx context.Context, id uuid.UUID, slidingExpiry time.Duration) error

	// Revoke marks a single session revoked. Idempotent: if the row is
	// already revoked, the original revoked_at and revoked_reason are
	// preserved.
	Revoke(ctx context.Context, id uuid.UUID, reason string) error

	// RevokeAllForUser bulk-revokes every active session for a user.
	// Returns the count of rows actually transitioned (sessions already
	// revoked are skipped). Used by force-logout (FR-030b).
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, reason string) (int64, error)

	// EvictExpired deletes session rows whose expires_at preceded
	// (now - retentionAfterExpiry). The grace window keeps recently-
	// expired rows around for post-mortem and audit correlation.
	EvictExpired(ctx context.Context, retentionAfterExpiry time.Duration) (int64, error)
}
