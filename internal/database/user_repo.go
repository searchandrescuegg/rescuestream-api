package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// UserRepo implements domain.UserRepository on pgxpool.
type UserRepo struct {
	pool *pgxpool.Pool
}

// NewUserRepo constructs a UserRepo bound to the given pool.
func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

// Upsert inserts a new user or updates an existing row, matching first by
// google_subject (when provided), then by email. The Google profile fields
// (display_name, avatar_url) are only overwritten when the upsert payload
// carries non-nil values, so a subsequent silent re-login from a different
// device cannot blank out a previously-recorded display_name.
func (r *UserRepo) Upsert(ctx context.Context, in domain.UserUpsert) (uuid.UUID, error) {
	email := domain.NormalizeEmail(in.Email)
	if email == "" {
		return uuid.Nil, fmt.Errorf("user: email required")
	}

	// Path 1: google_subject provided — try to update an existing row by sub.
	if in.GoogleSubject != nil && *in.GoogleSubject != "" {
		var id uuid.UUID
		err := r.pool.QueryRow(ctx, `
			UPDATE users
			SET email        = $1,
			    display_name = COALESCE($2, display_name),
			    avatar_url   = COALESCE($3, avatar_url),
			    updated_at   = NOW()
			WHERE google_subject = $4
			RETURNING id
		`, email, in.DisplayName, in.AvatarURL, *in.GoogleSubject).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("user: update by google_subject: %w", err)
		}
		// Fall through to email-based path; the row may exist with a NULL
		// google_subject (e.g., backfilled from audit-logs) and we want to
		// upgrade it on this login.
	}

	// Path 2: insert-or-update by email. The ON CONFLICT branch fills in the
	// google_subject when the existing row had it NULL — that's the
	// "audit-actor user later signs in" case from data-model §1.3.
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		INSERT INTO users (id, email, google_subject, display_name, avatar_url)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (email) DO UPDATE SET
			google_subject = COALESCE(users.google_subject, EXCLUDED.google_subject),
			display_name   = COALESCE(EXCLUDED.display_name, users.display_name),
			avatar_url     = COALESCE(EXCLUDED.avatar_url,   users.avatar_url),
			updated_at     = NOW()
		RETURNING id
	`, uuid.New(), email, in.GoogleSubject, in.DisplayName, in.AvatarURL).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("user: upsert by email: %w", err)
	}
	return id, nil
}

// FindByID returns the user with the given id.
func (r *UserRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return r.findOne(ctx, "id = $1", id)
}

// FindByEmail returns the user with the given email (normalized).
func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.findOne(ctx, "email = $1", domain.NormalizeEmail(email))
}

// FindByGoogleSubject returns the user with the given Google sub claim.
func (r *UserRepo) FindByGoogleSubject(ctx context.Context, sub string) (*domain.User, error) {
	return r.findOne(ctx, "google_subject = $1", sub)
}

func (r *UserRepo) findOne(ctx context.Context, where string, arg any) (*domain.User, error) {
	query := `
		SELECT id, email, google_subject, display_name, avatar_url, last_login_at, created_at, updated_at
		FROM users WHERE ` + where
	var u domain.User
	err := r.pool.QueryRow(ctx, query, arg).Scan(
		&u.ID, &u.Email, &u.GoogleSubject, &u.DisplayName, &u.AvatarURL,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user: find: %w", err)
	}
	return &u, nil
}

// TouchLastLogin sets last_login_at to NOW() for the given user.
func (r *UserRepo) TouchLastLogin(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE users SET last_login_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("user: touch last_login: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
