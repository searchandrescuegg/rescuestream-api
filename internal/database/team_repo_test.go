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

func makeOrg(t *testing.T, td *testutil.TestDatabase, slug string) *domain.Organization {
	t.Helper()
	creator := makeUser(t, td, "team-creator-"+slug+"@example.org")
	org, err := database.NewOrganizationRepo(td.Pool).Create(context.Background(), domain.OrganizationCreate{
		Name: "Org " + slug, Slug: slug, CreatedByUserID: creator,
	})
	require.NoError(t, err)
	return org
}

func TestTeamRepo_CreateAndFind(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	org := makeOrg(t, td, "team-create")
	repo := database.NewTeamRepo(td.Pool)

	created, err := repo.Create(ctx, domain.TeamCreate{
		OrganizationID:  org.ID,
		Name:            "KCESAR",
		WorkspaceDomain: "kcesar.example.org",
	})
	require.NoError(t, err)
	assert.Equal(t, "KCESAR", created.Name)
	assert.Equal(t, "kcesar.example.org", created.WorkspaceDomain)

	byID, err := repo.FindByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, byID.ID)

	byDomain, err := repo.FindByWorkspaceDomain(ctx, "kcesar.example.org")
	require.NoError(t, err)
	assert.Equal(t, created.ID, byDomain.ID)
}

func TestTeamRepo_Create_DuplicateWorkspaceDomainIsErrTaken(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	orgA := makeOrg(t, td, "org-a-dup")
	orgB := makeOrg(t, td, "org-b-dup")
	repo := database.NewTeamRepo(td.Pool)

	_, err := repo.Create(ctx, domain.TeamCreate{
		OrganizationID:  orgA.ID,
		Name:            "First",
		WorkspaceDomain: "shared.example.org",
	})
	require.NoError(t, err)

	// Even in a *different* org, the workspace_domain UNIQUE is platform-wide.
	_, err = repo.Create(ctx, domain.TeamCreate{
		OrganizationID:  orgB.ID,
		Name:            "Second",
		WorkspaceDomain: "shared.example.org",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrWorkspaceDomainTaken),
		"expected ErrWorkspaceDomainTaken, got: %v", err)
}

func TestTeamRepo_FindByID_NotFound(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	_, err := database.NewTeamRepo(td.Pool).FindByID(context.Background(), uuid.New())
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestTeamRepo_FindByWorkspaceDomain_NotFound(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	_, err := database.NewTeamRepo(td.Pool).FindByWorkspaceDomain(context.Background(), "missing.example.org")
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestTeamRepo_ListByOrg(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	orgA := makeOrg(t, td, "list-a")
	orgB := makeOrg(t, td, "list-b")
	repo := database.NewTeamRepo(td.Pool)

	for _, fx := range []struct {
		org    uuid.UUID
		name   string
		domain string
	}{
		{orgA.ID, "Charlie", "charlie.example.org"},
		{orgA.ID, "Alpha", "alpha.example.org"},
		{orgA.ID, "Bravo", "bravo.example.org"},
		{orgB.ID, "Delta", "delta.example.org"},
	} {
		_, err := repo.Create(ctx, domain.TeamCreate{
			OrganizationID:  fx.org,
			Name:            fx.name,
			WorkspaceDomain: fx.domain,
		})
		require.NoError(t, err)
	}

	got, err := repo.ListByOrg(ctx, orgA.ID)
	require.NoError(t, err)
	require.Len(t, got, 3)
	// Ordered by name ASC
	assert.Equal(t, "Alpha", got[0].Name)
	assert.Equal(t, "Bravo", got[1].Name)
	assert.Equal(t, "Charlie", got[2].Name)

	// orgB shows only Delta
	got, err = repo.ListByOrg(ctx, orgB.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Delta", got[0].Name)
}

func TestTeamRepo_Update(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	org := makeOrg(t, td, "update-org")
	repo := database.NewTeamRepo(td.Pool)

	team, err := repo.Create(ctx, domain.TeamCreate{
		OrganizationID: org.ID, Name: "Old", WorkspaceDomain: "old-team.example.org",
	})
	require.NoError(t, err)

	// Rename only.
	newName := "New"
	got, err := repo.Update(ctx, team.ID, domain.TeamUpdate{Name: &newName})
	require.NoError(t, err)
	assert.Equal(t, "New", got.Name)
	assert.Equal(t, "old-team.example.org", got.WorkspaceDomain)

	// Change workspace_domain only.
	newDom := "new-team.example.org"
	got, err = repo.Update(ctx, team.ID, domain.TeamUpdate{WorkspaceDomain: &newDom})
	require.NoError(t, err)
	assert.Equal(t, "New", got.Name)
	assert.Equal(t, "new-team.example.org", got.WorkspaceDomain)

	// Empty update returns current row unchanged.
	got, err = repo.Update(ctx, team.ID, domain.TeamUpdate{})
	require.NoError(t, err)
	assert.Equal(t, "New", got.Name)
}

func TestTeamRepo_Update_DuplicateDomainIsErrTaken(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	org := makeOrg(t, td, "update-dup")
	repo := database.NewTeamRepo(td.Pool)

	_, err := repo.Create(ctx, domain.TeamCreate{
		OrganizationID: org.ID, Name: "First", WorkspaceDomain: "first.example.org",
	})
	require.NoError(t, err)
	second, err := repo.Create(ctx, domain.TeamCreate{
		OrganizationID: org.ID, Name: "Second", WorkspaceDomain: "second.example.org",
	})
	require.NoError(t, err)

	clash := "first.example.org"
	_, err = repo.Update(ctx, second.ID, domain.TeamUpdate{WorkspaceDomain: &clash})
	assert.True(t, errors.Is(err, domain.ErrWorkspaceDomainTaken))
}

func TestTeamRepo_Update_NotFound(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	name := "X"
	_, err := database.NewTeamRepo(td.Pool).Update(context.Background(), uuid.New(),
		domain.TeamUpdate{Name: &name})
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestTeamRepo_Delete(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	org := makeOrg(t, td, "delete-org")
	repo := database.NewTeamRepo(td.Pool)

	team, err := repo.Create(ctx, domain.TeamCreate{
		OrganizationID: org.ID, Name: "Doomed", WorkspaceDomain: "doomed.example.org",
	})
	require.NoError(t, err)

	require.NoError(t, repo.Delete(ctx, team.ID))

	_, err = repo.FindByID(ctx, team.ID)
	require.True(t, errors.Is(err, domain.ErrNotFound))

	// Idempotent-style: deleting again returns ErrNotFound.
	require.True(t, errors.Is(repo.Delete(ctx, team.ID), domain.ErrNotFound))
}
