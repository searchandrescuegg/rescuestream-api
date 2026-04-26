package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// APIKeyRepo implements domain.APIKeyRepository using pgxpool.
type APIKeyRepo struct {
	pool *pgxpool.Pool
}

// NewAPIKeyRepo creates a new APIKeyRepo.
func NewAPIKeyRepo(pool *pgxpool.Pool) *APIKeyRepo {
	return &APIKeyRepo{pool: pool}
}

// GetByIdentifier retrieves an API key by its identifier.
func (r *APIKeyRepo) GetByIdentifier(ctx context.Context, keyIdentifier string) (*domain.APIKey, error) {
	query := `
		SELECT id, key_identifier, description, is_admin, created_at, last_used_at
		FROM api_keys
		WHERE key_identifier = $1
	`

	var k domain.APIKey
	err := r.pool.QueryRow(ctx, query, keyIdentifier).Scan(
		&k.ID,
		&k.KeyIdentifier,
		&k.Description,
		&k.IsAdmin,
		&k.CreatedAt,
		&k.LastUsedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get API key: %w", err)
	}

	return &k, nil
}

// IsAdmin checks if an API key has admin privileges.
func (r *APIKeyRepo) IsAdmin(ctx context.Context, keyIdentifier string) (bool, error) {
	query := `SELECT is_admin FROM api_keys WHERE key_identifier = $1`

	var isAdmin bool
	err := r.pool.QueryRow(ctx, query, keyIdentifier).Scan(&isAdmin)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Key not in database - not an admin
			return false, nil
		}
		return false, fmt.Errorf("failed to check admin status: %w", err)
	}

	return isAdmin, nil
}

// UpdateLastUsed updates the last_used_at timestamp for an API key.
func (r *APIKeyRepo) UpdateLastUsed(ctx context.Context, keyIdentifier string) error {
	query := `UPDATE api_keys SET last_used_at = NOW() WHERE key_identifier = $1`

	_, err := r.pool.Exec(ctx, query, keyIdentifier)
	if err != nil {
		return fmt.Errorf("failed to update last used: %w", err)
	}

	return nil
}
