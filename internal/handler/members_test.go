package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// makeMemberInOrg seeds a (user, team, member-role membership) trio in
// the given org. Returns user_id + team_id for follow-up assertions.
func makeMemberInOrg(t *testing.T, st *orgStack, email, orgSlug string, orgID uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	// Each member needs to live on a team (CHECK: member ⇒ team_id NOT NULL).
	teamID := uuid.New()
	_, err := st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, orgID, "Team-"+orgSlug, "team-"+orgSlug+"-"+email+".example.org")
	require.NoError(t, err)

	uid, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: email},
	)
	require.NoError(t, err)
	seedMembershipRow(t, st.td, uid, orgID, &teamID, domain.MembershipRoleMember)
	return uid, teamID
}

// --- ListMembers tests -----------------------------------------------

func TestListMembers_OrgAdminCanList(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "List", "list-org")

	makeMemberInOrg(t, st, "alpha@example.org", "list-org-1", org.ID)
	makeMemberInOrg(t, st, "bravo@example.org", "list-org-2", org.ID)

	rec := st.signedCall(t, http.MethodGet, "/orgs/"+org.ID.String()+"/members", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Members    []domain.OrganizationMemberView `json:"members"`
		TotalCount int64                           `json:"total_count"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, int64(2), resp.TotalCount)
	require.Len(t, resp.Members, 2)
	// Ordered by display_name (NULL last) then email — both have NULL
	// display_name so alphabetical email order applies.
	assert.Equal(t, "alpha@example.org", resp.Members[0].Email)
	assert.Equal(t, "bravo@example.org", resp.Members[1].Email)
	for _, m := range resp.Members {
		assert.NotNil(t, m.TeamID)
		assert.Equal(t, domain.MembershipRoleMember, m.Role)
		assert.Equal(t, []uuid.UUID{}, m.TagIDs, "tag_ids array must be present (empty until tags ship)")
	}
}

func TestListMembers_TeamFilter(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Filter", "filter-org")

	_, team1 := makeMemberInOrg(t, st, "x@example.org", "f-1", org.ID)
	makeMemberInOrg(t, st, "y@example.org", "f-2", org.ID)

	rec := st.signedCall(t, http.MethodGet,
		"/orgs/"+org.ID.String()+"/members?team_id="+team1.String(), nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Members []domain.OrganizationMemberView `json:"members"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Members, 1)
	assert.Equal(t, "x@example.org", resp.Members[0].Email)
}

func TestListMembers_SearchByEmail(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Search", "search-org")

	makeMemberInOrg(t, st, "alice.smith@example.org", "s-1", org.ID)
	makeMemberInOrg(t, st, "bob.jones@example.org", "s-2", org.ID)

	rec := st.signedCall(t, http.MethodGet,
		"/orgs/"+org.ID.String()+"/members?q=smith", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Members    []domain.OrganizationMemberView `json:"members"`
		TotalCount int64                           `json:"total_count"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, int64(1), resp.TotalCount)
	require.Len(t, resp.Members, 1)
	assert.Equal(t, "alice.smith@example.org", resp.Members[0].Email)
}

func TestListMembers_OutsiderRejected(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	orgA := createOrgAndCapture(t, st, "OrgA-list-out", "list-out-a")
	orgB := createOrgAndCapture(t, st, "OrgB-list-out", "list-out-b")

	// Demote alice to org-admin of orgA only.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob-list@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))
	seedMembershipRow(t, st.td, st.userID, orgA.ID, nil, domain.MembershipRoleOrgAdmin)

	rec := st.signedCall(t, http.MethodGet, "/orgs/"+orgB.ID.String()+"/members", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/not-in-org")
}

func TestListMembers_BadTeamIDIs400(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "BadFilter", "bad-filter")

	rec := st.signedCall(t, http.MethodGet,
		"/orgs/"+org.ID.String()+"/members?team_id=not-a-uuid", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// --- GetMember tests -------------------------------------------------

func TestGetMember_OrgAdminCanReadAnyMember(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "GetOrgAdmin", "get-oa")

	uid, _ := makeMemberInOrg(t, st, "target@example.org", "g-oa", org.ID)

	rec := st.signedCall(t, http.MethodGet,
		"/orgs/"+org.ID.String()+"/members/"+uid.String(), nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var got domain.OrganizationMemberView
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "target@example.org", got.Email)
	assert.Equal(t, uid, got.UserID)
}

func TestGetMember_NotFoundForCrossOrg(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	orgA := createOrgAndCapture(t, st, "Cross-A", "get-x-a")
	orgB := createOrgAndCapture(t, st, "Cross-B", "get-x-b")

	uid, _ := makeMemberInOrg(t, st, "cross@example.org", "x", orgA.ID)

	// Super-admin queries via orgB even though the user is in orgA.
	rec := st.signedCall(t, http.MethodGet,
		"/orgs/"+orgB.ID.String()+"/members/"+uid.String(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetMember_PlainMemberCanSelfView(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "SelfView", "self-view")

	// Demote alice to a plain member of the new org.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob-sv@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))

	teamID := uuid.New()
	_, err = st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, org.ID, "Team", "self-view-team.example.org")
	require.NoError(t, err)
	seedMembershipRow(t, st.td, st.userID, org.ID, &teamID, domain.MembershipRoleMember)

	rec := st.signedCall(t, http.MethodGet,
		"/orgs/"+org.ID.String()+"/members/"+st.userID.String(), nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var got domain.OrganizationMemberView
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, st.userID, got.UserID)
}

func TestGetMember_PlainMemberCannotViewPeers(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Peers", "peers")

	// alice is a plain member.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob-peer@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))

	teamID := uuid.New()
	_, err = st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, org.ID, "Team", "peers-team.example.org")
	require.NoError(t, err)
	seedMembershipRow(t, st.td, st.userID, org.ID, &teamID, domain.MembershipRoleMember)

	// A peer in the same org.
	peerID, _ := makeMemberInOrg(t, st, "peer@example.org", "peer", org.ID)

	rec := st.signedCall(t, http.MethodGet,
		"/orgs/"+org.ID.String()+"/members/"+peerID.String(), nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// --- RemoveMember tests ----------------------------------------------

func TestRemoveMember_OrgAdminCanRemove(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Remove", "rem-org")

	uid, _ := makeMemberInOrg(t, st, "doomed@example.org", "rem", org.ID)
	doomedKey := mintForUser(t, st.td, uid).KeyID

	rec := st.signedCall(t, http.MethodDelete,
		"/orgs/"+org.ID.String()+"/members/"+uid.String(), nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Membership row gone.
	_, err := database.NewMembershipRepo(st.td.Pool).GetByUser(context.Background(), uid)
	assert.ErrorIs(t, err, domain.ErrNotFound)

	// Sessions revoked with reason member-removed.
	sess, err := database.NewSessionRepo(st.td.Pool).FindByKeyID(context.Background(), doomedKey)
	require.NoError(t, err)
	require.NotNil(t, sess.RevokedReason)
	assert.Equal(t, domain.SessionRevokeReasonMembershipRemoved, *sess.RevokedReason)
}

func TestRemoveMember_OrgAdminTargetIs404(t *testing.T) {
	// Removing an org-admin via /members must return 404 (caller has
	// to use /admins instead). This stops org-admins from quietly
	// demoting their peers via the members endpoint.
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "AdminTarget", "admin-target")

	uid, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "victim-admin@example.org"},
	)
	require.NoError(t, err)
	seedMembershipRow(t, st.td, uid, org.ID, nil, domain.MembershipRoleOrgAdmin)

	rec := st.signedCall(t, http.MethodDelete,
		"/orgs/"+org.ID.String()+"/members/"+uid.String(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Org-admin row preserved.
	got, err := database.NewMembershipRepo(st.td.Pool).GetByUser(context.Background(), uid)
	require.NoError(t, err)
	assert.Equal(t, domain.MembershipRoleOrgAdmin, got.Role)
}

func TestRemoveMember_NonAdminIs403(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "NonAdmin", "non-adm-org")

	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob-na@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))
	teamID := uuid.New()
	_, err = st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, org.ID, "Team", "non-adm-team.example.org")
	require.NoError(t, err)
	seedMembershipRow(t, st.td, st.userID, org.ID, &teamID, domain.MembershipRoleMember)

	// Plain-member alice tries to remove a peer.
	target, _ := makeMemberInOrg(t, st, "target-na@example.org", "na", org.ID)
	rec := st.signedCall(t, http.MethodDelete,
		"/orgs/"+org.ID.String()+"/members/"+target.String(), nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRemoveMember_UnknownUserIs404(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Unknown", "rem-unk")

	rec := st.signedCall(t, http.MethodDelete,
		"/orgs/"+org.ID.String()+"/members/"+uuid.New().String(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
