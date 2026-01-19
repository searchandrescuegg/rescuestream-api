package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// BroadcasterService handles broadcaster management.
type BroadcasterService struct {
	broadcasterRepo domain.BroadcasterRepository
	logger          *slog.Logger
}

// BroadcasterServiceOption is a functional option for configuring BroadcasterService.
type BroadcasterServiceOption func(*BroadcasterService)

// WithBroadcasterLogger sets the logger for BroadcasterService.
func WithBroadcasterLogger(logger *slog.Logger) BroadcasterServiceOption {
	return func(s *BroadcasterService) {
		s.logger = logger
	}
}

// NewBroadcasterService creates a new BroadcasterService.
func NewBroadcasterService(
	broadcasterRepo domain.BroadcasterRepository,
	opts ...BroadcasterServiceOption,
) *BroadcasterService {
	s := &BroadcasterService{
		broadcasterRepo: broadcasterRepo,
		logger:          slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// CreateBroadcasterRequest represents a request to create a broadcaster.
type CreateBroadcasterRequest struct {
	DisplayName string
	Metadata    map[string]interface{}
}

// UpdateBroadcasterRequest represents a request to update a broadcaster.
type UpdateBroadcasterRequest struct {
	DisplayName *string
	Metadata    map[string]interface{}
}

// Create creates a new broadcaster.
func (s *BroadcasterService) Create(ctx context.Context, req CreateBroadcasterRequest) (*domain.Broadcaster, error) {
	broadcaster := &domain.Broadcaster{
		ID:          uuid.New(),
		DisplayName: req.DisplayName,
		Metadata:    req.Metadata,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if broadcaster.Metadata == nil {
		broadcaster.Metadata = make(map[string]interface{})
	}

	if err := s.broadcasterRepo.Create(ctx, broadcaster); err != nil {
		return nil, err
	}

	s.logger.Info("broadcaster created",
		slog.String("broadcaster_id", broadcaster.ID.String()),
		slog.String("display_name", broadcaster.DisplayName),
	)

	return broadcaster, nil
}

// GetByID retrieves a broadcaster by ID.
func (s *BroadcasterService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Broadcaster, error) {
	return s.broadcasterRepo.GetByID(ctx, id)
}

// List retrieves all broadcasters.
func (s *BroadcasterService) List(ctx context.Context) ([]domain.Broadcaster, error) {
	return s.broadcasterRepo.List(ctx)
}

// Update updates an existing broadcaster.
func (s *BroadcasterService) Update(ctx context.Context, id uuid.UUID, req UpdateBroadcasterRequest) (*domain.Broadcaster, error) {
	broadcaster, err := s.broadcasterRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.DisplayName != nil {
		broadcaster.DisplayName = *req.DisplayName
	}

	if req.Metadata != nil {
		broadcaster.Metadata = req.Metadata
	}

	if err := s.broadcasterRepo.Update(ctx, broadcaster); err != nil {
		return nil, err
	}

	// Refresh to get updated_at
	return s.broadcasterRepo.GetByID(ctx, id)
}

// Delete deletes a broadcaster.
func (s *BroadcasterService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.broadcasterRepo.Delete(ctx, id); err != nil {
		return err
	}

	s.logger.Info("broadcaster deleted",
		slog.String("broadcaster_id", id.String()),
	)

	return nil
}
