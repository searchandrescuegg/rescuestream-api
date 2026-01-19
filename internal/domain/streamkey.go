package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// StreamKeyStatus represents the status of a stream key.
type StreamKeyStatus string

const (
	StreamKeyStatusActive  StreamKeyStatus = "active"
	StreamKeyStatusRevoked StreamKeyStatus = "revoked"
	StreamKeyStatusExpired StreamKeyStatus = "expired"
)

// StreamKey is a credential that authorizes a broadcaster to start a stream.
type StreamKey struct {
	ID            uuid.UUID       `json:"id"`
	KeyValue      string          `json:"key_value,omitempty"`
	BroadcasterID uuid.UUID       `json:"broadcaster_id"`
	Status        StreamKeyStatus `json:"status"`
	CreatedAt     time.Time       `json:"created_at"`
	ExpiresAt     *time.Time      `json:"expires_at,omitempty"`
	RevokedAt     *time.Time      `json:"revoked_at,omitempty"`
	LastUsedAt    *time.Time      `json:"last_used_at,omitempty"`
}

// IsValid checks if the stream key is currently valid for use.
func (sk *StreamKey) IsValid() bool {
	if sk.Status != StreamKeyStatusActive {
		return false
	}
	if sk.ExpiresAt != nil && time.Now().After(*sk.ExpiresAt) {
		return false
	}
	return true
}

// StreamKeyRepository defines the interface for stream key persistence.
type StreamKeyRepository interface {
	Create(ctx context.Context, key *StreamKey) error
	GetByID(ctx context.Context, id uuid.UUID) (*StreamKey, error)
	GetByKeyValue(ctx context.Context, keyValue string) (*StreamKey, error)
	ListByBroadcaster(ctx context.Context, broadcasterID uuid.UUID) ([]StreamKey, error)
	ListAll(ctx context.Context) ([]StreamKey, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status StreamKeyStatus, revokedAt *time.Time) error
	UpdateLastUsed(ctx context.Context, id uuid.UUID) error

	// GetAndLockByKeyValue atomically retrieves and locks a stream key for update.
	// This is used for authentication to prevent race conditions.
	GetAndLockByKeyValue(ctx context.Context, keyValue string) (*StreamKey, error)
}
