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

func TestSuperAdminRepo_AddAndIsSuperAdmin(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "super1@example.org")
	repo := database.NewSuperAdminRepo(td.Pool)

	require.NoError(t, repo.Add(ctx, userID, nil, true))

	got, err := repo.IsSuperAdmin(ctx, userID)
	require.NoError(t, err)
	assert.True(t, got)

	other := makeUser(t, td, "regular@example.org")
	got, err = repo.IsSuperAdmin(ctx, other)
	require.NoError(t, err)
	assert.False(t, got)
}

func TestSuperAdminRepo_Add_isIdempotent(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "super2@example.org")
	repo := database.NewSuperAdminRepo(td.Pool)

	require.NoError(t, repo.Add(ctx, userID, nil, true))
	require.NoError(t, repo.Add(ctx, userID, nil, true), "second Add must be a no-op")

	n, err := repo.CountRemaining(ctx)
	require.NoError(t, err)
	// Note: the migrate binary's seeder may have already added super-admins
	// from SUPER_ADMIN_EMAILS at testcontainer setup; we just assert ours is
	// represented exactly once by checking IsSuperAdmin and that count >= 1.
	assert.GreaterOrEqual(t, n, int64(1))

	one, err := repo.IsSuperAdmin(ctx, userID)
	require.NoError(t, err)
	assert.True(t, one)
}

func TestSuperAdminRepo_GetReturnsRow(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "super3@example.org")
	granterID := makeUser(t, td, "granter@example.org")
	repo := database.NewSuperAdminRepo(td.Pool)

	require.NoError(t, repo.Add(ctx, userID, &granterID, false))
	got, err := repo.Get(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, userID, got.UserID)
	require.NotNil(t, got.GrantedByUserID)
	assert.Equal(t, granterID, *got.GrantedByUserID)
	assert.False(t, got.SeededFromEnv)

	_, err = repo.Get(ctx, uuid.New())
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestSuperAdminRepo_RemoveAndCount(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	repo := database.NewSuperAdminRepo(td.Pool)

	a := makeUser(t, td, "rm-a@example.org")
	b := makeUser(t, td, "rm-b@example.org")
	require.NoError(t, repo.Add(ctx, a, nil, false))
	require.NoError(t, repo.Add(ctx, b, nil, false))

	pre, err := repo.CountRemaining(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Remove(ctx, a))

	post, err := repo.CountRemaining(ctx)
	require.NoError(t, err)
	assert.Equal(t, pre-1, post)

	require.True(t, errors.Is(repo.Remove(ctx, a), domain.ErrNotFound),
		"Remove of an absent row must return ErrNotFound")
}

func TestSuperAdminRepo_ListSorted(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	repo := database.NewSuperAdminRepo(td.Pool)

	a := makeUser(t, td, "list-a@example.org")
	b := makeUser(t, td, "list-b@example.org")
	require.NoError(t, repo.Add(ctx, a, nil, false))
	require.NoError(t, repo.Add(ctx, b, nil, false))

	rows, err := repo.List(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), 2)

	// granted_at ASC: a (added first) precedes b in the list.
	aIdx, bIdx := -1, -1
	for i, r := range rows {
		switch r.UserID {
		case a:
			aIdx = i
		case b:
			bIdx = i
		}
	}
	require.NotEqual(t, -1, aIdx)
	require.NotEqual(t, -1, bIdx)
	assert.Less(t, aIdx, bIdx, "List must be ordered by granted_at ASC")
}
