package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// StreamStatus represents the status of a stream.
type StreamStatus string

const (
	StreamStatusActive StreamStatus = "active"
	StreamStatusEnded  StreamStatus = "ended"
)

// Stream represents an active or historical video broadcast session.
type Stream struct {
	ID           uuid.UUID              `json:"id"`
	StreamKeyID  uuid.UUID              `json:"stream_key_id"`
	Path         string                 `json:"path"`
	Status       StreamStatus           `json:"status"`
	StartedAt    time.Time              `json:"started_at"`
	EndedAt      *time.Time             `json:"ended_at,omitempty"`
	SourceType   *string                `json:"source_type,omitempty"`
	SourceID     *string                `json:"source_id,omitempty"`
	Metadata     map[string]interface{} `json:"metadata"`
	RecordingRef *string                `json:"recording_ref,omitempty"`
}

// StreamURLs contains video playback URLs for a stream.
type StreamURLs struct {
	HLS    string `json:"hls"`
	WebRTC string `json:"webrtc"`
}

// StreamWithURLs is a stream with computed video playback URLs.
type StreamWithURLs struct {
	Stream
	URLs StreamURLs `json:"urls"`
}

// StreamRepository defines the interface for stream persistence.
type StreamRepository interface {
	Create(ctx context.Context, stream *Stream) error
	GetByID(ctx context.Context, id uuid.UUID) (*Stream, error)
	GetActiveByPath(ctx context.Context, path string) (*Stream, error)
	GetActiveByStreamKeyID(ctx context.Context, keyID uuid.UUID) (*Stream, error)
	ListActive(ctx context.Context) ([]Stream, error)
	EndStream(ctx context.Context, id uuid.UUID) error
	EndStreamByPath(ctx context.Context, path string) error
}
