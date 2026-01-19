package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// StreamKeyService handles stream key management.
type StreamKeyService struct {
	streamKeyRepo  domain.StreamKeyRepository
	streamRepo     domain.StreamRepository
	mediaMTXClient *MediaMTXClient
	logger         *slog.Logger
}

// StreamKeyServiceOption is a functional option for configuring StreamKeyService.
type StreamKeyServiceOption func(*StreamKeyService)

// WithStreamKeyLogger sets the logger for StreamKeyService.
func WithStreamKeyLogger(logger *slog.Logger) StreamKeyServiceOption {
	return func(s *StreamKeyService) {
		s.logger = logger
	}
}

// NewStreamKeyService creates a new StreamKeyService.
func NewStreamKeyService(
	streamKeyRepo domain.StreamKeyRepository,
	streamRepo domain.StreamRepository,
	mediaMTXClient *MediaMTXClient,
	opts ...StreamKeyServiceOption,
) *StreamKeyService {
	s := &StreamKeyService{
		streamKeyRepo:  streamKeyRepo,
		streamRepo:     streamRepo,
		mediaMTXClient: mediaMTXClient,
		logger:         slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// CreateRequest represents a request to create a stream key.
type CreateRequest struct {
	BroadcasterID uuid.UUID
	ExpiresAt     *time.Time
}

// Create creates a new stream key for a broadcaster.
func (s *StreamKeyService) Create(ctx context.Context, req CreateRequest) (*domain.StreamKey, error) {
	keyValue, err := generateStreamKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate stream key: %w", err)
	}

	key := &domain.StreamKey{
		ID:            uuid.New(),
		KeyValue:      keyValue,
		BroadcasterID: req.BroadcasterID,
		Status:        domain.StreamKeyStatusActive,
		CreatedAt:     time.Now(),
		ExpiresAt:     req.ExpiresAt,
	}

	if err := s.streamKeyRepo.Create(ctx, key); err != nil {
		return nil, err
	}

	s.logger.Info("stream key created",
		slog.String("key_id", key.ID.String()),
		slog.String("broadcaster_id", key.BroadcasterID.String()),
	)

	return key, nil
}

// GetByID retrieves a stream key by ID.
func (s *StreamKeyService) GetByID(ctx context.Context, id uuid.UUID) (*domain.StreamKey, error) {
	return s.streamKeyRepo.GetByID(ctx, id)
}

// List retrieves all stream keys.
func (s *StreamKeyService) List(ctx context.Context) ([]domain.StreamKey, error) {
	keys, err := s.streamKeyRepo.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	// Clear key values for security
	for i := range keys {
		keys[i].KeyValue = ""
	}

	return keys, nil
}

// ListByBroadcaster retrieves all stream keys for a broadcaster.
func (s *StreamKeyService) ListByBroadcaster(ctx context.Context, broadcasterID uuid.UUID) ([]domain.StreamKey, error) {
	keys, err := s.streamKeyRepo.ListByBroadcaster(ctx, broadcasterID)
	if err != nil {
		return nil, err
	}

	// Clear key values for security
	for i := range keys {
		keys[i].KeyValue = ""
	}

	return keys, nil
}

// Revoke revokes a stream key and terminates any active stream.
func (s *StreamKeyService) Revoke(ctx context.Context, id uuid.UUID) error {
	key, err := s.streamKeyRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if key.Status != domain.StreamKeyStatusActive {
		return domain.ErrInvalidStatus
	}

	// Check for active stream
	activeStream, err := s.streamRepo.GetActiveByStreamKeyID(ctx, id)
	if err != nil && err != domain.ErrNotFound {
		return fmt.Errorf("failed to check active stream: %w", err)
	}

	// Terminate active stream if exists
	if activeStream != nil {
		s.logger.Info("terminating active stream due to key revocation",
			slog.String("stream_id", activeStream.ID.String()),
			slog.String("key_id", id.String()),
		)

		// Kick from MediaMTX
		if err := s.mediaMTXClient.KickPath(ctx, activeStream.Path); err != nil {
			s.logger.Warn("failed to kick path from MediaMTX",
				slog.String("error", err.Error()),
				slog.String("path", activeStream.Path),
			)
		}

		// End stream in database
		if err := s.streamRepo.EndStream(ctx, activeStream.ID); err != nil {
			s.logger.Warn("failed to end stream",
				slog.String("error", err.Error()),
				slog.String("stream_id", activeStream.ID.String()),
			)
		}
	}

	// Revoke the key
	now := time.Now()
	if err := s.streamKeyRepo.UpdateStatus(ctx, id, domain.StreamKeyStatusRevoked, &now); err != nil {
		return err
	}

	s.logger.Info("stream key revoked",
		slog.String("key_id", id.String()),
	)

	return nil
}

// generateStreamKey generates a cryptographically secure stream key.
// Format: sk_ + 43 characters of base64url (32 bytes = 256 bits of entropy)
func generateStreamKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "sk_" + base64.RawURLEncoding.EncodeToString(bytes), nil
}
