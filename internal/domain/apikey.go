package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// APIKey represents an API key with admin authorization capability.
type APIKey struct {
	ID            uuid.UUID  `json:"id"`
	KeyIdentifier string     `json:"key_identifier"`
	Description   *string    `json:"description,omitempty"`
	IsAdmin       bool       `json:"is_admin"`
	CreatedAt     time.Time  `json:"created_at"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
}

// APIKeyRepository defines the interface for API key persistence.
type APIKeyRepository interface {
	GetByIdentifier(ctx context.Context, keyIdentifier string) (*APIKey, error)
	IsAdmin(ctx context.Context, keyIdentifier string) (bool, error)
	UpdateLastUsed(ctx context.Context, keyIdentifier string) error
}
