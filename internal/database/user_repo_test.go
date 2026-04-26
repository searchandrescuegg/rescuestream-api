package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

func ptr[T any](v T) *T { return &v }

func TestUserRepo_Upsert_insertsNewByEmail(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	repo := database.NewUserRepo(td.Pool)
	id, err := repo.Upsert(context.Background(), domain.UserUpsert{
		Email:       "Alice@Example.ORG ", // tests normalization
		DisplayName: ptr("Alice"),
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, id)

	got, err := repo.FindByEmail(context.Background(), "alice@example.org")
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "alice@example.org", got.Email, "email must be normalized to lowercase")
	require.NotNil(t, got.DisplayName)
	assert.Equal(t, "Alice", *got.DisplayName)
}

func TestUserRepo_Upsert_upgradesExistingRowOnFirstGoogleLogin(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	repo := database.NewUserRepo(td.Pool)

	// Step 1: a backfilled row exists with no google_subject (e.g., from
	// audit_logs.actor) — same shape as the 000003 backfill produces.
	preID, err := repo.Upsert(ctx, domain.UserUpsert{Email: "bob@example.org"})
	require.NoError(t, err)

	// Step 2: Bob signs in via Google for the first time. The upsert
	// should match by email and fill in google_subject + display_name.
	postID, err := repo.Upsert(ctx, domain.UserUpsert{
		Email:         "bob@example.org",
		GoogleSubject: ptr("google-sub-bob-001"),
		DisplayName:   ptr("Bob R."),
	})
	require.NoError(t, err)
	assert.Equal(t, preID, postID, "must reuse the existing row, not create a new one")

	got, err := repo.FindByGoogleSubject(ctx, "google-sub-bob-001")
	require.NoError(t, err)
	require.NotNil(t, got.GoogleSubject)
	assert.Equal(t, "google-sub-bob-001", *got.GoogleSubject)
	require.NotNil(t, got.DisplayName)
	assert.Equal(t, "Bob R.", *got.DisplayName)
}

func TestUserRepo_Upsert_doesNotOverwriteDisplayNameWithNil(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	repo := database.NewUserRepo(td.Pool)

	id, err := repo.Upsert(ctx, domain.UserUpsert{
		Email:         "carol@example.org",
		GoogleSubject: ptr("google-sub-carol"),
		DisplayName:   ptr("Carol"),
	})
	require.NoError(t, err)

	// Re-upsert without a display_name — must preserve the prior value.
	_, err = repo.Upsert(ctx, domain.UserUpsert{
		Email:         "carol@example.org",
		GoogleSubject: ptr("google-sub-carol"),
	})
	require.NoError(t, err)

	got, err := repo.FindByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got.DisplayName)
	assert.Equal(t, "Carol", *got.DisplayName)
}

func TestUserRepo_FindByID_notFound(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	_, err := database.NewUserRepo(td.Pool).FindByID(context.Background(), uuid.New())
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestUserRepo_TouchLastLogin(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	repo := database.NewUserRepo(td.Pool)
	id, err := repo.Upsert(ctx, domain.UserUpsert{Email: "dave@example.org"})
	require.NoError(t, err)

	require.NoError(t, repo.TouchLastLogin(ctx, id))

	got, err := repo.FindByID(ctx, id)
	require.NoError(t, err)
	assert.NotNil(t, got.LastLoginAt, "last_login_at must be set after Touch")

	require.True(t, errors.Is(repo.TouchLastLogin(ctx, uuid.New()), domain.ErrNotFound))
}

func TestUserRepo_Upsert_emailRequired(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	_, err := database.NewUserRepo(td.Pool).Upsert(context.Background(), domain.UserUpsert{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email required")
}
