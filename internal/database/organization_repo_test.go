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

// makeUser is a small fixture helper for tests that need a creator row.
func makeUser(t *testing.T, td *testutil.TestDatabase, email string) uuid.UUID {
	t.Helper()
	id, err := database.NewUserRepo(td.Pool).Upsert(context.Background(), domain.UserUpsert{Email: email})
	require.NoError(t, err)
	return id
}

func TestOrganizationRepo_Create_andFind(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	creator := makeUser(t, td, "creator@example.org")
	repo := database.NewOrganizationRepo(td.Pool)

	created, err := repo.Create(ctx, domain.OrganizationCreate{
		Name: "King County SAR", Slug: "kcsara", CreatedByUserID: creator,
	})
	require.NoError(t, err)
	assert.Equal(t, "kcsara", created.Slug)
	assert.Equal(t, domain.OrgStatusActive, created.Status)

	bySlug, err := repo.FindBySlug(ctx, "kcsara")
	require.NoError(t, err)
	assert.Equal(t, created.ID, bySlug.ID)

	byID, err := repo.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "King County SAR", byID.Name)
}

func TestOrganizationRepo_Create_duplicateSlugIsErrAlreadyExists(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	creator := makeUser(t, td, "creator@example.org")
	repo := database.NewOrganizationRepo(td.Pool)

	_, err := repo.Create(ctx, domain.OrganizationCreate{Name: "First", Slug: "shared", CreatedByUserID: creator})
	require.NoError(t, err)

	_, err = repo.Create(ctx, domain.OrganizationCreate{Name: "Second", Slug: "shared", CreatedByUserID: creator})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrAlreadyExists), "expected ErrAlreadyExists for duplicate slug, got: %v", err)
}

func TestOrganizationRepo_FindBySlug_notFound(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	_, err := database.NewOrganizationRepo(td.Pool).FindBySlug(context.Background(), "missing-slug")
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestOrganizationRepo_RenameAndSetStatus(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	creator := makeUser(t, td, "creator@example.org")
	repo := database.NewOrganizationRepo(td.Pool)

	created, err := repo.Create(ctx, domain.OrganizationCreate{Name: "Old", Slug: "rename-test", CreatedByUserID: creator})
	require.NoError(t, err)

	renamed, err := repo.Rename(ctx, created.ID, "New")
	require.NoError(t, err)
	assert.Equal(t, "New", renamed.Name)
	assert.Equal(t, "rename-test", renamed.Slug, "slug must remain immutable")

	require.NoError(t, repo.SetStatus(ctx, created.ID, domain.OrgStatusSuspended))
	suspended, err := repo.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.OrgStatusSuspended, suspended.Status)

	require.NoError(t, repo.SetStatus(ctx, created.ID, domain.OrgStatusActive))
	reactivated, err := repo.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.OrgStatusActive, reactivated.Status)
}

func TestOrganizationRepo_List_searchAndStatusFilter(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	creator := makeUser(t, td, "creator@example.org")
	repo := database.NewOrganizationRepo(td.Pool)

	for _, fx := range []struct {
		name, slug string
		suspend    bool
	}{
		{"Alpha SAR", "alpha", false},
		{"Bravo SAR", "bravo", true},
		{"Charlie Rescue", "charlie", false},
	} {
		o, err := repo.Create(ctx, domain.OrganizationCreate{Name: fx.name, Slug: fx.slug, CreatedByUserID: creator})
		require.NoError(t, err)
		if fx.suspend {
			require.NoError(t, repo.SetStatus(ctx, o.ID, domain.OrgStatusSuspended))
		}
	}

	// Default org from migration 000003 ('default', active) is also present —
	// account for it in expected counts.
	all, total, err := repo.List(ctx, domain.OrganizationListFilter{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, int64(4)) // 3 fixtures + default
	assert.GreaterOrEqual(t, len(all), 4)

	// Search "SAR" should match Alpha + Bravo (case-insensitive name) only.
	sar, sarTotal, err := repo.List(ctx, domain.OrganizationListFilter{Search: "SAR"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), sarTotal)
	assert.Len(t, sar, 2)

	// Search "alpha" matches Alpha by slug.
	alpha, alphaTotal, err := repo.List(ctx, domain.OrganizationListFilter{Search: "alpha"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), alphaTotal)
	assert.Len(t, alpha, 1)
	assert.Equal(t, "alpha", alpha[0].Slug)

	// Status filter: only Bravo is suspended.
	suspended, susTotal, err := repo.List(ctx, domain.OrganizationListFilter{Status: domain.OrgStatusSuspended})
	require.NoError(t, err)
	assert.Equal(t, int64(1), susTotal)
	require.Len(t, suspended, 1)
	assert.Equal(t, "bravo", suspended[0].Slug)
}

func TestOrganizationRepo_List_pagination(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	creator := makeUser(t, td, "creator@example.org")
	repo := database.NewOrganizationRepo(td.Pool)

	for _, slug := range []string{"page-a", "page-b", "page-c", "page-d", "page-e"} {
		_, err := repo.Create(ctx, domain.OrganizationCreate{Name: slug, Slug: slug, CreatedByUserID: creator})
		require.NoError(t, err)
	}

	first, total, err := repo.List(ctx, domain.OrganizationListFilter{Search: "page-", Limit: 2, Offset: 0})
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, first, 2)

	second, _, err := repo.List(ctx, domain.OrganizationListFilter{Search: "page-", Limit: 2, Offset: 2})
	require.NoError(t, err)
	assert.Len(t, second, 2)
	assert.NotEqual(t, first[0].ID, second[0].ID, "pagination must return distinct rows")
}

func TestOrganizationRepo_Delete(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	creator := makeUser(t, td, "creator@example.org")
	repo := database.NewOrganizationRepo(td.Pool)

	created, err := repo.Create(ctx, domain.OrganizationCreate{Name: "Doomed", Slug: "doomed", CreatedByUserID: creator})
	require.NoError(t, err)

	require.NoError(t, repo.Delete(ctx, created.ID))

	_, err = repo.FindByID(ctx, created.ID)
	require.True(t, errors.Is(err, domain.ErrNotFound))

	// Idempotent-style: deleting again returns ErrNotFound.
	require.True(t, errors.Is(repo.Delete(ctx, created.ID), domain.ErrNotFound))
}
