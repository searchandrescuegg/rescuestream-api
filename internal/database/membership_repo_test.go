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

// seedMembership inserts an organization_memberships row directly. Used to
// stand up fixtures for the lookup-only repo at this stage; the full
// service-layer Upsert lands with US2.
func seedMembership(t *testing.T, td *testutil.TestDatabase, userID, orgID uuid.UUID, teamID *uuid.UUID, role domain.MembershipRole) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := td.Pool.Exec(context.Background(), `
		INSERT INTO organization_memberships (id, user_id, organization_id, team_id, role)
		VALUES ($1, $2, $3, $4, $5)
	`, id, userID, orgID, teamID, role)
	require.NoError(t, err)
	return id
}

func TestMembershipRepo_GetByUser_orgAdminWithoutTeam(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "admin@example.org")
	defaultOrgID := uuid.MustParse("00000000-0000-4000-8000-000000000002")

	id := seedMembership(t, td, userID, defaultOrgID, nil, domain.MembershipRoleOrgAdmin)

	repo := database.NewMembershipRepo(td.Pool)
	got, err := repo.GetByUser(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, defaultOrgID, got.OrganizationID)
	assert.Nil(t, got.TeamID)
	assert.Equal(t, domain.MembershipRoleOrgAdmin, got.Role)
}

func TestMembershipRepo_GetByUser_notFound(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	repo := database.NewMembershipRepo(td.Pool)
	_, err := repo.GetByUser(context.Background(), uuid.New())
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestMembershipRepo_Replace_insertsWhenAbsent(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "newadmin@example.org")
	defaultOrgID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	repo := database.NewMembershipRepo(td.Pool)

	got, err := repo.Replace(ctx, domain.MembershipReplace{
		UserID:         userID,
		OrganizationID: defaultOrgID,
		TeamID:         nil,
		Role:           domain.MembershipRoleOrgAdmin,
	})
	require.NoError(t, err)
	assert.Equal(t, userID, got.UserID)
	assert.Equal(t, defaultOrgID, got.OrganizationID)
	assert.Equal(t, domain.MembershipRoleOrgAdmin, got.Role)
	assert.Nil(t, got.TeamID)
}

func TestMembershipRepo_Replace_updatesWhenPresent(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "switcher@example.org")
	defaultOrgID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	repo := database.NewMembershipRepo(td.Pool)
	orgRepo := database.NewOrganizationRepo(td.Pool)

	// Seed an initial member-with-team membership.
	teamID := uuid.New()
	_, err := td.Pool.Exec(ctx, `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, defaultOrgID, "Team A", "switcher.example.org")
	require.NoError(t, err)
	first := seedMembership(t, td, userID, defaultOrgID, &teamID, domain.MembershipRoleMember)

	// Build a second org and Replace into it as org-admin (no team).
	creator := makeUser(t, td, "creator2@example.org")
	otherOrg, err := orgRepo.Create(ctx, domain.OrganizationCreate{
		Name: "Other", Slug: "other", CreatedByUserID: creator,
	})
	require.NoError(t, err)

	got, err := repo.Replace(ctx, domain.MembershipReplace{
		UserID:         userID,
		OrganizationID: otherOrg.ID,
		TeamID:         nil,
		Role:           domain.MembershipRoleOrgAdmin,
	})
	require.NoError(t, err)
	assert.Equal(t, first, got.ID, "Replace must reuse the existing row id (UNIQUE user_id)")
	assert.Equal(t, otherOrg.ID, got.OrganizationID)
	assert.Equal(t, domain.MembershipRoleOrgAdmin, got.Role)
	assert.Nil(t, got.TeamID)

	// Subsequent GetByUser returns the new membership.
	now, err := repo.GetByUser(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, otherOrg.ID, now.OrganizationID)
}

func TestMembershipRepo_DeleteByUser(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "removable@example.org")
	defaultOrgID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	repo := database.NewMembershipRepo(td.Pool)

	seedMembership(t, td, userID, defaultOrgID, nil, domain.MembershipRoleOrgAdmin)
	require.NoError(t, repo.DeleteByUser(ctx, userID))

	_, err := repo.GetByUser(ctx, userID)
	require.True(t, errors.Is(err, domain.ErrNotFound))

	// Idempotent-ish: second delete returns ErrNotFound.
	require.True(t, errors.Is(repo.DeleteByUser(ctx, userID), domain.ErrNotFound))
}
