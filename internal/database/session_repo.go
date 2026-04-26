package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// SessionRepo implements domain.SessionRepository on pgxpool.
type SessionRepo struct {
	pool *pgxpool.Pool
}

// NewSessionRepo constructs a SessionRepo bound to the given pool.
func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo {
	return &SessionRepo{pool: pool}
}

// Create inserts a new session row.
func (r *SessionRepo) Create(ctx context.Context, in domain.SessionCreate) (*domain.Session, error) {
	id := uuid.New()
	var s domain.Session
	err := r.pool.QueryRow(ctx, `
		INSERT INTO sessions (id, user_id, hmac_key_id, hmac_secret_hash, expires_at, client_ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6::inet, $7)
		RETURNING id, user_id, hmac_key_id, hmac_secret_hash, created_at, expires_at, last_used_at, revoked_at, revoked_reason, host(client_ip), user_agent
	`, id, in.UserID, in.HMACKeyID, in.HMACSecretHash, in.ExpiresAt, in.ClientIP, in.UserAgent).Scan(
		&s.ID, &s.UserID, &s.HMACKeyID, &s.HMACSecretHash,
		&s.CreatedAt, &s.ExpiresAt, &s.LastUsedAt, &s.RevokedAt, &s.RevokedReason,
		&s.ClientIP, &s.UserAgent,
	)
	if err != nil {
		return nil, fmt.Errorf("session: create: %w", err)
	}
	return &s, nil
}

// FindByKeyID returns the session matching the given X-API-Key, or
// domain.ErrNotFound.
func (r *SessionRepo) FindByKeyID(ctx context.Context, keyID string) (*domain.Session, error) {
	var s domain.Session
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id, hmac_key_id, hmac_secret_hash,
		       created_at, expires_at, last_used_at,
		       revoked_at, revoked_reason,
		       host(client_ip), user_agent
		FROM sessions
		WHERE hmac_key_id = $1
	`, keyID).Scan(
		&s.ID, &s.UserID, &s.HMACKeyID, &s.HMACSecretHash,
		&s.CreatedAt, &s.ExpiresAt, &s.LastUsedAt,
		&s.RevokedAt, &s.RevokedReason,
		&s.ClientIP, &s.UserAgent,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("session: find_by_key_id: %w", err)
	}
	return &s, nil
}

// Touch slides expires_at forward and updates last_used_at. Touching a
// revoked session is a no-op (the WHERE clause filters it out) and returns
// nil — callers should have already validated `Valid()` upstream.
func (r *SessionRepo) Touch(ctx context.Context, id uuid.UUID, slidingExpiry time.Duration) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE sessions
		SET last_used_at = NOW(),
		    expires_at   = NOW() + $1::interval
		WHERE id = $2 AND revoked_at IS NULL
	`, slidingExpiry, id)
	if err != nil {
		return fmt.Errorf("session: touch: %w", err)
	}
	return nil
}

// Revoke marks a session revoked. The COALESCE guards against overwriting
// a previous revocation's reason on duplicate calls.
func (r *SessionRepo) Revoke(ctx context.Context, id uuid.UUID, reason string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE sessions
		SET revoked_at     = COALESCE(revoked_at, NOW()),
		    revoked_reason = COALESCE(revoked_reason, $1)
		WHERE id = $2
	`, reason, id)
	if err != nil {
		return fmt.Errorf("session: revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// RevokeAllForUser bulk-revokes every still-active session for a user.
// Returns the count of rows transitioned (i.e., not previously revoked).
func (r *SessionRepo) RevokeAllForUser(ctx context.Context, userID uuid.UUID, reason string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE sessions
		SET revoked_at     = NOW(),
		    revoked_reason = $1
		WHERE user_id = $2 AND revoked_at IS NULL
	`, reason, userID)
	if err != nil {
		return 0, fmt.Errorf("session: revoke_all_for_user: %w", err)
	}
	return tag.RowsAffected(), nil
}

// EvictExpired deletes session rows whose expires_at preceded
// (now - retentionAfterExpiry). Returns the count of rows deleted.
func (r *SessionRepo) EvictExpired(ctx context.Context, retentionAfterExpiry time.Duration) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM sessions
		WHERE expires_at < NOW() - $1::interval
	`, retentionAfterExpiry)
	if err != nil {
		return 0, fmt.Errorf("session: evict_expired: %w", err)
	}
	return tag.RowsAffected(), nil
}
