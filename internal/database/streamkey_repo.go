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

// StreamKeyRepo implements domain.StreamKeyRepository using pgxpool.
type StreamKeyRepo struct {
	pool *pgxpool.Pool
}

// NewStreamKeyRepo creates a new StreamKeyRepo.
func NewStreamKeyRepo(pool *pgxpool.Pool) *StreamKeyRepo {
	return &StreamKeyRepo{pool: pool}
}

// Create creates a new stream key.
func (r *StreamKeyRepo) Create(ctx context.Context, key *domain.StreamKey) error {
	query := `
		INSERT INTO stream_keys (id, key_value, broadcaster_id, status, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	if key.ID == uuid.Nil {
		key.ID = uuid.New()
	}

	_, err := r.pool.Exec(ctx, query,
		key.ID,
		key.KeyValue,
		key.BroadcasterID,
		key.Status,
		key.CreatedAt,
		key.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create stream key: %w", err)
	}

	return nil
}

// GetByID retrieves a stream key by ID.
func (r *StreamKeyRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.StreamKey, error) {
	query := `
		SELECT id, key_value, broadcaster_id, status, created_at, expires_at, revoked_at, last_used_at
		FROM stream_keys
		WHERE id = $1
	`

	return r.scanStreamKey(r.pool.QueryRow(ctx, query, id))
}

// GetByKeyValue retrieves a stream key by its key value.
func (r *StreamKeyRepo) GetByKeyValue(ctx context.Context, keyValue string) (*domain.StreamKey, error) {
	query := `
		SELECT id, key_value, broadcaster_id, status, created_at, expires_at, revoked_at, last_used_at
		FROM stream_keys
		WHERE key_value = $1
	`

	return r.scanStreamKey(r.pool.QueryRow(ctx, query, keyValue))
}

// GetAndLockByKeyValue atomically retrieves and locks a stream key for update.
// This must be called within a transaction.
func (r *StreamKeyRepo) GetAndLockByKeyValue(ctx context.Context, keyValue string) (*domain.StreamKey, error) {
	query := `
		SELECT id, key_value, broadcaster_id, status, created_at, expires_at, revoked_at, last_used_at
		FROM stream_keys
		WHERE key_value = $1
		FOR UPDATE
	`

	return r.scanStreamKey(r.pool.QueryRow(ctx, query, keyValue))
}

// ListByBroadcaster retrieves all stream keys for a broadcaster.
func (r *StreamKeyRepo) ListByBroadcaster(ctx context.Context, broadcasterID uuid.UUID) ([]domain.StreamKey, error) {
	query := `
		SELECT id, key_value, broadcaster_id, status, created_at, expires_at, revoked_at, last_used_at
		FROM stream_keys
		WHERE broadcaster_id = $1
		ORDER BY created_at DESC
	`

	return r.queryStreamKeys(ctx, query, broadcasterID)
}

// ListAll retrieves all stream keys.
func (r *StreamKeyRepo) ListAll(ctx context.Context) ([]domain.StreamKey, error) {
	query := `
		SELECT id, key_value, broadcaster_id, status, created_at, expires_at, revoked_at, last_used_at
		FROM stream_keys
		ORDER BY created_at DESC
	`

	return r.queryStreamKeys(ctx, query)
}

// UpdateStatus updates the status of a stream key.
func (r *StreamKeyRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.StreamKeyStatus, revokedAt *time.Time) error {
	query := `
		UPDATE stream_keys
		SET status = $2, revoked_at = $3
		WHERE id = $1
	`

	result, err := r.pool.Exec(ctx, query, id, status, revokedAt)
	if err != nil {
		return fmt.Errorf("failed to update stream key status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// UpdateLastUsed updates the last used timestamp of a stream key.
func (r *StreamKeyRepo) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE stream_keys SET last_used_at = NOW() WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to update last used: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return nil
}

func (r *StreamKeyRepo) scanStreamKey(row pgx.Row) (*domain.StreamKey, error) {
	var key domain.StreamKey

	err := row.Scan(
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
		return nil, fmt.Errorf("failed to scan stream key: %w", err)
	}

	return &key, nil
}

func (r *StreamKeyRepo) queryStreamKeys(ctx context.Context, query string, args ...interface{}) ([]domain.StreamKey, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query stream keys: %w", err)
	}
	defer rows.Close()

	var keys []domain.StreamKey
	for rows.Next() {
		var key domain.StreamKey
		if err := rows.Scan(
			&key.ID,
			&key.KeyValue,
			&key.BroadcasterID,
			&key.Status,
			&key.CreatedAt,
			&key.ExpiresAt,
			&key.RevokedAt,
			&key.LastUsedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan stream key: %w", err)
		}
		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating stream keys: %w", err)
	}

	return keys, nil
}
