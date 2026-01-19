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

// BroadcasterRepo implements domain.BroadcasterRepository using pgxpool.
type BroadcasterRepo struct {
	pool *pgxpool.Pool
}

// NewBroadcasterRepo creates a new BroadcasterRepo.
func NewBroadcasterRepo(pool *pgxpool.Pool) *BroadcasterRepo {
	return &BroadcasterRepo{pool: pool}
}

// Create creates a new broadcaster.
func (r *BroadcasterRepo) Create(ctx context.Context, broadcaster *domain.Broadcaster) error {
	metadataJSON, err := json.Marshal(broadcaster.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO broadcasters (id, display_name, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	if broadcaster.ID == uuid.Nil {
		broadcaster.ID = uuid.New()
	}

	_, err = r.pool.Exec(ctx, query,
		broadcaster.ID,
		broadcaster.DisplayName,
		metadataJSON,
		broadcaster.CreatedAt,
		broadcaster.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create broadcaster: %w", err)
	}

	return nil
}

// GetByID retrieves a broadcaster by ID.
func (r *BroadcasterRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Broadcaster, error) {
	query := `
		SELECT id, display_name, metadata, created_at, updated_at
		FROM broadcasters
		WHERE id = $1
	`

	var b domain.Broadcaster
	var metadataJSON []byte

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&b.ID,
		&b.DisplayName,
		&metadataJSON,
		&b.CreatedAt,
		&b.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get broadcaster: %w", err)
	}

	if err := json.Unmarshal(metadataJSON, &b.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &b, nil
}

// Update updates an existing broadcaster.
func (r *BroadcasterRepo) Update(ctx context.Context, broadcaster *domain.Broadcaster) error {
	metadataJSON, err := json.Marshal(broadcaster.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		UPDATE broadcasters
		SET display_name = $2, metadata = $3, updated_at = NOW()
		WHERE id = $1
	`

	result, err := r.pool.Exec(ctx, query,
		broadcaster.ID,
		broadcaster.DisplayName,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to update broadcaster: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// Delete deletes a broadcaster by ID.
func (r *BroadcasterRepo) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM broadcasters WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete broadcaster: %w", err)
	}

	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// List retrieves all broadcasters.
func (r *BroadcasterRepo) List(ctx context.Context) ([]domain.Broadcaster, error) {
	query := `
		SELECT id, display_name, metadata, created_at, updated_at
		FROM broadcasters
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list broadcasters: %w", err)
	}
	defer rows.Close()

	var broadcasters []domain.Broadcaster
	for rows.Next() {
		var b domain.Broadcaster
		var metadataJSON []byte

		if err := rows.Scan(
			&b.ID,
			&b.DisplayName,
			&metadataJSON,
			&b.CreatedAt,
			&b.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan broadcaster: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &b.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		broadcasters = append(broadcasters, b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating broadcasters: %w", err)
	}

	return broadcasters, nil
}
