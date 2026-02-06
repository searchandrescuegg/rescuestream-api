package service

import (
	"context"
	"log/slog"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// APIKeyService handles API key operations.
type APIKeyService struct {
	apiKeyRepo domain.APIKeyRepository
	logger     *slog.Logger
}

// APIKeyServiceOption is a functional option for configuring APIKeyService.
type APIKeyServiceOption func(*APIKeyService)

// WithAPIKeyLogger sets the logger for APIKeyService.
func WithAPIKeyLogger(logger *slog.Logger) APIKeyServiceOption {
	return func(s *APIKeyService) {
		s.logger = logger
	}
}

// NewAPIKeyService creates a new APIKeyService.
func NewAPIKeyService(
	apiKeyRepo domain.APIKeyRepository,
	opts ...APIKeyServiceOption,
) *APIKeyService {
	s := &APIKeyService{
		apiKeyRepo: apiKeyRepo,
		logger:     slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// IsAdmin checks if an API key has admin privileges.
func (s *APIKeyService) IsAdmin(ctx context.Context, apiKey string) (bool, error) {
	isAdmin, err := s.apiKeyRepo.IsAdmin(ctx, apiKey)
	if err != nil {
		s.logger.Error("failed to check admin status",
			slog.String("error", err.Error()),
		)
		return false, err
	}

	return isAdmin, nil
}

// GetByIdentifier retrieves an API key by its identifier.
func (s *APIKeyService) GetByIdentifier(ctx context.Context, keyIdentifier string) (*domain.APIKey, error) {
	return s.apiKeyRepo.GetByIdentifier(ctx, keyIdentifier)
}
