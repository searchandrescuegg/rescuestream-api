package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/pepper"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

const teamDeleteTestPepper = "team-del-pepper-eeeeeeeeeeeeeeeeeeeeee"

// teamDeleteStack assembles every dependency the team-deletion
// orchestration touches. Reuses real Postgres via testcontainers so
// the FK + CHECK constraint behavior is exercised, not mocked.
type teamDeleteStack struct {
	td       *testutil.TestDatabase
	svc      *service.TeamService
	users    *database.UserRepo
	teams    *database.TeamRepo
	members  *database.MembershipRepo
	orgs     *database.OrganizationRepo
	sessions *service.SessionService
	sessRepo *database.SessionRepo
}

func newTeamDeleteStack(t *testing.T) *teamDeleteStack {
	t.Helper()
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	hasher, err := pepper.New(teamDeleteTestPepper)
	require.NoError(t, err)

	users := database.NewUserRepo(td.Pool)
	teams := database.NewTeamRepo(td.Pool)
	members := database.NewMembershipRepo(td.Pool)
	orgs := database.NewOrganizationRepo(td.Pool)
	sessRepo := database.NewSessionRepo(td.Pool)
	sessions := service.NewSessionService(sessRepo, hasher)

	svc := service.NewTeamService(td.Pool, teams, members, sessions)

	return &teamDeleteStack{
		td: td, svc: svc,
		users: users, teams: teams, members: members, orgs: orgs,
		sessions: sessions, sessRepo: sessRepo,
	}
}

func (st *teamDeleteStack) makeOrg(t *testing.T, slug string) uuid.UUID {
	t.Helper()
	creator, err := st.users.Upsert(context.Background(), domain.UserUpsert{
		Email: "creator-" + slug + "@example.org",
	})
	require.NoError(t, err)
	org, err := st.orgs.Create(context.Background(), domain.OrganizationCreate{
		Name: "Org " + slug, Slug: slug, CreatedByUserID: creator,
	})
	require.NoError(t, err)
	return org.ID
}

func (st *teamDeleteStack) makeTeam(t *testing.T, orgID uuid.UUID, name, dom string) *domain.Team {
	t.Helper()
	team, err := st.teams.Create(context.Background(), domain.TeamCreate{
		OrganizationID: orgID, Name: name, WorkspaceDomain: dom,
	})
	require.NoError(t, err)
	return team
}

func (st *teamDeleteStack) makeUserAndMembership(t *testing.T, email string, orgID uuid.UUID, teamID *uuid.UUID, role domain.MembershipRole) uuid.UUID {
	t.Helper()
	uid, err := st.users.Upsert(context.Background(), domain.UserUpsert{Email: email})
	require.NoError(t, err)
	_, err = st.members.Replace(context.Background(), domain.MembershipReplace{
		UserID:         uid,
		OrganizationID: orgID,
		TeamID:         teamID,
		Role:           role,
	})
	require.NoError(t, err)
	return uid
}

func (st *teamDeleteStack) mintSession(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	mint, err := st.sessions.Mint(context.Background(), service.MintInput{UserID: userID})
	require.NoError(t, err)
	return mint.KeyID
}

// --- Tests -----------------------------------------------------------

func TestTeamServiceDelete_NoDependents_SimpleDelete(t *testing.T) {
	st := newTeamDeleteStack(t)
	orgID := st.makeOrg(t, "td-simple")
	team := st.makeTeam(t, orgID, "Empty", "td-simple.example.org")

	require.NoError(t, st.svc.Delete(context.Background(), team.ID))

	_, err := st.teams.FindByID(context.Background(), team.ID)
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestTeamServiceDelete_RemovesMemberMembershipsAndRevokesSessions(t *testing.T) {
	st := newTeamDeleteStack(t)
	orgID := st.makeOrg(t, "td-members")
	team := st.makeTeam(t, orgID, "WithMembers", "td-members.example.org")
	tid := team.ID

	// Two members of the team, each with an active session.
	bob := st.makeUserAndMembership(t, "bob-td@example.org", orgID, &tid, domain.MembershipRoleMember)
	carol := st.makeUserAndMembership(t, "carol-td@example.org", orgID, &tid, domain.MembershipRoleMember)
	bobKey := st.mintSession(t, bob)
	carolKey := st.mintSession(t, carol)

	require.NoError(t, st.svc.Delete(context.Background(), team.ID))

	// Team is gone.
	_, err := st.teams.FindByID(context.Background(), team.ID)
	require.True(t, errors.Is(err, domain.ErrNotFound))

	// Both member memberships are gone.
	for _, uid := range []uuid.UUID{bob, carol} {
		_, err := st.members.GetByUser(context.Background(), uid)
		assert.True(t, errors.Is(err, domain.ErrNotFound),
			"member %s membership row must be removed", uid)
	}

	// Both members' sessions are revoked with reason team-deleted.
	for _, key := range []string{bobKey, carolKey} {
		sess, err := st.sessRepo.FindByKeyID(context.Background(), key)
		require.NoError(t, err)
		require.NotNil(t, sess.RevokedReason)
		assert.Equal(t, domain.SessionRevokeReasonTeamDeleted, *sess.RevokedReason)
		assert.False(t, sess.Valid())
	}
}

func TestTeamServiceDelete_PreservesOrgAdminWithNullTeamID(t *testing.T) {
	// An org-admin whose team_id happens to point at the deleted team
	// must NOT lose their org-admin row — only the team_id is cleared
	// (FR-009: org-admins may carry NULL team_id).
	st := newTeamDeleteStack(t)
	orgID := st.makeOrg(t, "td-orgadmin")
	team := st.makeTeam(t, orgID, "AdminTeam", "td-orgadmin.example.org")
	tid := team.ID

	dave := st.makeUserAndMembership(t, "dave-orgadm-td@example.org", orgID, &tid, domain.MembershipRoleOrgAdmin)
	daveKey := st.mintSession(t, dave)

	require.NoError(t, st.svc.Delete(context.Background(), team.ID))

	// Team gone.
	_, err := st.teams.FindByID(context.Background(), team.ID)
	require.True(t, errors.Is(err, domain.ErrNotFound))

	// Dave's org-admin row is preserved with team_id = NULL.
	got, err := st.members.GetByUser(context.Background(), dave)
	require.NoError(t, err)
	assert.Equal(t, orgID, got.OrganizationID)
	assert.Equal(t, domain.MembershipRoleOrgAdmin, got.Role)
	assert.Nil(t, got.TeamID, "org-admin's team_id must be NULL after team deletion")

	// Org-admin sessions are NOT revoked — only members lose theirs.
	sess, err := st.sessRepo.FindByKeyID(context.Background(), daveKey)
	require.NoError(t, err)
	assert.True(t, sess.Valid(), "org-admin sessions must NOT be revoked on team deletion")
}

func TestTeamServiceDelete_MixedMembersAndOrgAdmin(t *testing.T) {
	// One team has both: a member-role row AND an org-admin row whose
	// team_id happens to match. The orchestration must DELETE the
	// member row, NULL the org-admin row, then DELETE the team.
	st := newTeamDeleteStack(t)
	orgID := st.makeOrg(t, "td-mixed")
	team := st.makeTeam(t, orgID, "Mixed", "td-mixed.example.org")
	tid := team.ID

	memberID := st.makeUserAndMembership(t, "mixed-member@example.org", orgID, &tid, domain.MembershipRoleMember)
	adminID := st.makeUserAndMembership(t, "mixed-admin@example.org", orgID, &tid, domain.MembershipRoleOrgAdmin)

	require.NoError(t, st.svc.Delete(context.Background(), team.ID))

	// Member: row gone.
	_, err := st.members.GetByUser(context.Background(), memberID)
	assert.True(t, errors.Is(err, domain.ErrNotFound))

	// Admin: row preserved with NULL team.
	got, err := st.members.GetByUser(context.Background(), adminID)
	require.NoError(t, err)
	assert.Nil(t, got.TeamID)
	assert.Equal(t, domain.MembershipRoleOrgAdmin, got.Role)
}

func TestTeamServiceDelete_UnknownTeamReturnsErrNotFound(t *testing.T) {
	st := newTeamDeleteStack(t)
	err := st.svc.Delete(context.Background(), uuid.New())
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestTeamServiceDelete_BlankIDReturnsValidationError(t *testing.T) {
	st := newTeamDeleteStack(t)
	err := st.svc.Delete(context.Background(), uuid.Nil)
	require.Error(t, err)
	assert.False(t, errors.Is(err, domain.ErrNotFound),
		"uuid.Nil is a caller bug, not a missing row")
	assert.Contains(t, err.Error(), "id required")
}

func TestTeamServiceDelete_OnlyAffectsTargetTeam(t *testing.T) {
	// Defensive: the orchestration must scope its DELETE/UPDATE to
	// team_id = $1. Other teams' members and admins must remain
	// untouched.
	st := newTeamDeleteStack(t)
	orgID := st.makeOrg(t, "td-isolate")
	keep := st.makeTeam(t, orgID, "Keep", "td-keep.example.org")
	gone := st.makeTeam(t, orgID, "Gone", "td-gone.example.org")
	keepID, goneID := keep.ID, gone.ID

	keepMember := st.makeUserAndMembership(t, "keep-mem@example.org", orgID, &keepID, domain.MembershipRoleMember)
	keepAdmin := st.makeUserAndMembership(t, "keep-admin@example.org", orgID, &keepID, domain.MembershipRoleOrgAdmin)
	goneMember := st.makeUserAndMembership(t, "gone-mem@example.org", orgID, &goneID, domain.MembershipRoleMember)

	require.NoError(t, st.svc.Delete(context.Background(), gone.ID))

	// Keep team's rows survive untouched.
	keepMemberRow, err := st.members.GetByUser(context.Background(), keepMember)
	require.NoError(t, err)
	require.NotNil(t, keepMemberRow.TeamID)
	assert.Equal(t, keepID, *keepMemberRow.TeamID)

	keepAdminRow, err := st.members.GetByUser(context.Background(), keepAdmin)
	require.NoError(t, err)
	require.NotNil(t, keepAdminRow.TeamID, "keep-team's org-admin team_id must NOT be touched")
	assert.Equal(t, keepID, *keepAdminRow.TeamID)

	// Gone team's member is gone.
	_, err = st.members.GetByUser(context.Background(), goneMember)
	assert.True(t, errors.Is(err, domain.ErrNotFound))

	// Keep team itself is intact.
	_, err = st.teams.FindByID(context.Background(), keep.ID)
	assert.NoError(t, err)
}
