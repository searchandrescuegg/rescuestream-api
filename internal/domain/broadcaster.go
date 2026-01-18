package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Broadcaster represents an entity authorized to create streams.
type Broadcaster struct {
	ID          uuid.UUID              `json:"id"`
	DisplayName string                 `json:"display_name"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// BroadcasterRepository defines the interface for broadcaster persistence.
type BroadcasterRepository interface {
	Create(ctx context.Context, broadcaster *Broadcaster) error
	GetByID(ctx context.Context, id uuid.UUID) (*Broadcaster, error)
	Update(ctx context.Context, broadcaster *Broadcaster) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context) ([]Broadcaster, error)
}
