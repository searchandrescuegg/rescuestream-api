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
