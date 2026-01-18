package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// AuthService handles stream key authentication.
type AuthService struct {
	pool          *pgxpool.Pool
	streamKeyRepo domain.StreamKeyRepository
	streamRepo    domain.StreamRepository
	logger        *slog.Logger
}

// AuthServiceOption is a functional option for configuring AuthService.
type AuthServiceOption func(*AuthService)

// WithAuthLogger sets the logger for AuthService.
func WithAuthLogger(logger *slog.Logger) AuthServiceOption {
	return func(s *AuthService) {
		s.logger = logger
	}
}

// NewAuthService creates a new AuthService.
func NewAuthService(
	pool *pgxpool.Pool,
	streamKeyRepo domain.StreamKeyRepository,
	streamRepo domain.StreamRepository,
	opts ...AuthServiceOption,
) *AuthService {
	s := &AuthService{
		pool:          pool,
		streamKeyRepo: streamKeyRepo,
		streamRepo:    streamRepo,
		logger:        slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// AuthRequest represents an authentication request from MediaMTX.
type AuthRequest struct {
	User     string `json:"user"`
	Password string `json:"password"` // This is the stream key
	IP       string `json:"ip"`
	Action   string `json:"action"`
	Path     string `json:"path"`
	Protocol string `json:"protocol"`
	ID       string `json:"id"`
	Query    string `json:"query"`
}

// AuthResult represents the result of an authentication attempt.
type AuthResult struct {
	Allowed     bool
	StreamKeyID *string
	Reason      string
}

// Authenticate validates a stream key for publishing.
// It uses a transaction with SELECT FOR UPDATE to prevent race conditions.
func (s *AuthService) Authenticate(ctx context.Context, req AuthRequest) (*AuthResult, error) {
	// Only authenticate publish actions
	if req.Action != "publish" {
		return &AuthResult{Allowed: true, Reason: "non-publish action allowed"}, nil
	}

	// The stream key can be sent as password OR as the path
	// RTMP clients typically use: rtmp://host:1935/<stream_key>
	// which puts the key in the path field
	keyValue := req.Password
	if keyValue == "" {
		// Try extracting from path (strip leading slash if present)
		keyValue = req.Path
		if len(keyValue) > 0 && keyValue[0] == '/' {
			keyValue = keyValue[1:]
		}
	}
	if keyValue == "" {
		return &AuthResult{Allowed: false, Reason: "missing stream key"}, nil
	}

	var result *AuthResult
	var authErr error

	// Use a transaction to ensure atomic check-and-update
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Get and lock the stream key
		key, err := s.getAndLockStreamKey(ctx, tx, keyValue)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				result = &AuthResult{Allowed: false, Reason: "invalid stream key"}
				return nil
			}
			return fmt.Errorf("failed to get stream key: %w", err)
		}

		// Check if key is valid
		if key.Status == domain.StreamKeyStatusRevoked {
			result = &AuthResult{Allowed: false, Reason: "stream key revoked"}
			return nil
		}

		if key.Status == domain.StreamKeyStatusExpired {
			result = &AuthResult{Allowed: false, Reason: "stream key expired"}
			return nil
		}

		if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
			// Mark as expired
			_, err = tx.Exec(ctx, "UPDATE stream_keys SET status = 'expired' WHERE id = $1", key.ID)
			if err != nil {
				return fmt.Errorf("failed to update expired status: %w", err)
			}
			result = &AuthResult{Allowed: false, Reason: "stream key expired"}
			return nil
		}

		// Check if key is already in use (has active stream)
		activeStream, err := s.getActiveStreamByKeyID(ctx, tx, key.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("failed to check active stream: %w", err)
		}
		if activeStream != nil {
			result = &AuthResult{Allowed: false, Reason: "stream key already in use"}
			return nil
		}

		// Update last used timestamp
		_, err = tx.Exec(ctx, "UPDATE stream_keys SET last_used_at = NOW() WHERE id = $1", key.ID)
		if err != nil {
			return fmt.Errorf("failed to update last used: %w", err)
		}

		keyIDStr := key.ID.String()
		result = &AuthResult{
			Allowed:     true,
			StreamKeyID: &keyIDStr,
			Reason:      "authentication successful",
		}

		s.logger.Info("stream key authenticated",
			slog.String("key_id", key.ID.String()),
			slog.String("broadcaster_id", key.BroadcasterID.String()),
			slog.String("path", req.Path),
			slog.String("ip", req.IP),
		)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("authentication transaction failed: %w", err)
	}

	if authErr != nil {
		return nil, authErr
	}

	return result, nil
}

func (s *AuthService) getAndLockStreamKey(ctx context.Context, tx pgx.Tx, keyValue string) (*domain.StreamKey, error) {
	query := `
		SELECT id, key_value, broadcaster_id, status, created_at, expires_at, revoked_at, last_used_at
		FROM stream_keys
		WHERE key_value = $1
		FOR UPDATE
	`

	var key domain.StreamKey
	err := tx.QueryRow(ctx, query, keyValue).Scan(
		&key.ID,
		&key.KeyValue,
		&key.BroadcasterID,
		&key.Status,
		&key.CreatedAt,
		&key.ExpiresAt,
		&key.RevokedAt,
		&key.LastUsedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	return &key, nil
}

func (s *AuthService) getActiveStreamByKeyID(ctx context.Context, tx pgx.Tx, keyID interface{}) (*domain.Stream, error) {
	query := `
		SELECT id, stream_key_id, path, status, started_at
		FROM streams
		WHERE stream_key_id = $1 AND status = 'active'
	`

	var stream domain.Stream
	err := tx.QueryRow(ctx, query, keyID).Scan(
		&stream.ID,
		&stream.StreamKeyID,
		&stream.Path,
		&stream.Status,
		&stream.StartedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	return &stream, nil
}
