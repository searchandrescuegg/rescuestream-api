package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// StreamRepo implements domain.StreamRepository using pgxpool.
type StreamRepo struct {
	pool *pgxpool.Pool
}

// NewStreamRepo creates a new StreamRepo.
func NewStreamRepo(pool *pgxpool.Pool) *StreamRepo {
	return &StreamRepo{pool: pool}
}

// Create creates a new stream.
func (r *StreamRepo) Create(ctx context.Context, stream *domain.Stream) error {
	metadataJSON, err := json.Marshal(stream.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO streams (id, stream_key_id, path, status, started_at, source_type, source_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	if stream.ID == uuid.Nil {
		stream.ID = uuid.New()
	}

	_, err = r.pool.Exec(ctx, query,
		stream.ID,
		stream.StreamKeyID,
		stream.Path,
		stream.Status,
		stream.StartedAt,
		stream.SourceType,
		stream.SourceID,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}

	return nil
}

// GetByID retrieves a stream by ID.
func (r *StreamRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Stream, error) {
	query := `
		SELECT id, stream_key_id, path, status, started_at, ended_at, source_type, source_id, metadata, recording_ref
		FROM streams
		WHERE id = $1
	`

	return r.scanStream(r.pool.QueryRow(ctx, query, id))
}

// GetActiveByPath retrieves an active stream by path.
func (r *StreamRepo) GetActiveByPath(ctx context.Context, path string) (*domain.Stream, error) {
	query := `
		SELECT id, stream_key_id, path, status, started_at, ended_at, source_type, source_id, metadata, recording_ref
		FROM streams
		WHERE path = $1 AND status = 'active'
	`

	return r.scanStream(r.pool.QueryRow(ctx, query, path))
}

// GetActiveByStreamKeyID retrieves an active stream by stream key ID.
func (r *StreamRepo) GetActiveByStreamKeyID(ctx context.Context, keyID uuid.UUID) (*domain.Stream, error) {
	query := `
		SELECT id, stream_key_id, path, status, started_at, ended_at, source_type, source_id, metadata, recording_ref
		FROM streams
		WHERE stream_key_id = $1 AND status = 'active'
	`

	return r.scanStream(r.pool.QueryRow(ctx, query, keyID))
}

// ListActive retrieves all active streams.
func (r *StreamRepo) ListActive(ctx context.Context) ([]domain.Stream, error) {
	query := `
		SELECT id, stream_key_id, path, status, started_at, ended_at, source_type, source_id, metadata, recording_ref
		FROM streams
		WHERE status = 'active'
		ORDER BY started_at DESC
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list active streams: %w", err)
	}
	defer rows.Close()

	var streams []domain.Stream
	for rows.Next() {
		stream, err := r.scanStreamFromRows(rows)
		if err != nil {
			return nil, err
		}
		streams = append(streams, *stream)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating streams: %w", err)
	}

	return streams, nil
}

// EndStream ends a stream by ID.
func (r *StreamRepo) EndStream(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE streams
		SET status = 'ended', ended_at = NOW()
		WHERE id = $1 AND status = 'active'
	`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to end stream: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// EndStreamByPath ends a stream by path.
func (r *StreamRepo) EndStreamByPath(ctx context.Context, path string) error {
	query := `
		UPDATE streams
		SET status = 'ended', ended_at = NOW()
		WHERE path = $1 AND status = 'active'
	`

	result, err := r.pool.Exec(ctx, query, path)
	if err != nil {
		return fmt.Errorf("failed to end stream by path: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return nil
}

func (r *StreamRepo) scanStream(row pgx.Row) (*domain.Stream, error) {
	var stream domain.Stream
	var metadataJSON []byte

	err := row.Scan(
		&stream.ID,
		&stream.StreamKeyID,
		&stream.Path,
		&stream.Status,
		&stream.StartedAt,
		&stream.EndedAt,
		&stream.SourceType,
		&stream.SourceID,
		&metadataJSON,
		&stream.RecordingRef,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan stream: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &stream.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &stream, nil
}

func (r *StreamRepo) scanStreamFromRows(rows pgx.Rows) (*domain.Stream, error) {
	var stream domain.Stream
	var metadataJSON []byte

	err := rows.Scan(
		&stream.ID,
		&stream.StreamKeyID,
		&stream.Path,
		&stream.Status,
		&stream.StartedAt,
		&stream.EndedAt,
		&stream.SourceType,
		&stream.SourceID,
		&metadataJSON,
		&stream.RecordingRef,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan stream: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &stream.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &stream, nil
}
