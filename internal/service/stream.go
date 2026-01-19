package service

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// StreamService handles stream-related business logic.
type StreamService struct {
	streamRepo     domain.StreamRepository
	mediaMTXClient *MediaMTXClient
	logger         *slog.Logger
}

// StreamServiceOption is a functional option for configuring StreamService.
type StreamServiceOption func(*StreamService)

// WithStreamLogger sets the logger for StreamService.
func WithStreamLogger(logger *slog.Logger) StreamServiceOption {
	return func(s *StreamService) {
		s.logger = logger
	}
}

// NewStreamService creates a new StreamService.
func NewStreamService(
	streamRepo domain.StreamRepository,
	mediaMTXClient *MediaMTXClient,
	opts ...StreamServiceOption,
) *StreamService {
	s := &StreamService{
		streamRepo:     streamRepo,
		mediaMTXClient: mediaMTXClient,
		logger:         slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ListActive returns all active streams with video URLs.
func (s *StreamService) ListActive(ctx context.Context) ([]domain.StreamWithURLs, error) {
	streams, err := s.streamRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]domain.StreamWithURLs, len(streams))
	for i, stream := range streams {
		result[i] = domain.StreamWithURLs{
			Stream: stream,
			URLs: domain.StreamURLs{
				HLS:    s.mediaMTXClient.GetHLSURL(stream.Path),
				WebRTC: s.mediaMTXClient.GetWebRTCURL(stream.Path),
			},
		}
	}

	return result, nil
}

// GetByID returns a stream by ID with video URLs.
func (s *StreamService) GetByID(ctx context.Context, id uuid.UUID) (*domain.StreamWithURLs, error) {
	stream, err := s.streamRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return &domain.StreamWithURLs{
		Stream: *stream,
		URLs: domain.StreamURLs{
			HLS:    s.mediaMTXClient.GetHLSURL(stream.Path),
			WebRTC: s.mediaMTXClient.GetWebRTCURL(stream.Path),
		},
	}, nil
}
