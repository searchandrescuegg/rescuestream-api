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

// orgStack already has the /orgs/{id}/members/{user_id}/revoke-sessions
// route registered (it was added via the same orgHandler that powers
// AddAdmin/RemoveAdmin). These tests reuse newOrgStack and exercise
// the route end-to-end.

// seedSession mints a fresh session for the given user via the test
// stack's existing infrastructure. Returns the persisted session id +
// key id so callers can check revocation state afterward.
func seedSession(t *testing.T, st *orgStack, userID uuid.UUID) (sessionID uuid.UUID, keyID string) {
	t.Helper()
	mr := mintForUser(t, st.td, userID)
	return mr.Session.ID, mr.KeyID
}

func TestRevokeMemberSessions_SuperAdminCanRevokeAcrossOrgs(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Alpha", "rev-alpha")

	// Bob is org-admin of the new org with two active sessions.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob@example.org"},
	)
	require.NoError(t, err)
	seedMembershipRow(t, st.td, bobID, org.ID, nil, domain.MembershipRoleOrgAdmin)
	_, k1 := seedSession(t, st, bobID)
	_, k2 := seedSession(t, st, bobID)

	rec := st.signedCall(t, http.MethodPost,
		"/orgs/"+org.ID.String()+"/members/"+bobID.String()+"/revoke-sessions", nil)
	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp struct {
		RevokedCount int64 `json:"revoked_count"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, int64(2), resp.RevokedCount)

	// Both sessions are revoked with reason admin-force-logout.
	for _, key := range []string{k1, k2} {
		got, getErr := database.NewSessionRepo(st.td.Pool).FindByKeyID(context.Background(), key)
		require.NoError(t, getErr)
		require.NotNil(t, got.RevokedReason)
		assert.Equal(t, domain.SessionRevokeReasonAdminForceLogout, *got.RevokedReason)
		assert.False(t, got.Valid())
	}

	// Bob's membership row is preserved (force-logout doesn't remove
	// the user from the org).
	m, err := database.NewMembershipRepo(st.td.Pool).GetByUser(context.Background(), bobID)
	require.NoError(t, err)
	assert.Equal(t, org.ID, m.OrganizationID)
	assert.Equal(t, domain.MembershipRoleOrgAdmin, m.Role)
}

func TestRevokeMemberSessions_OrgAdminCanRevokeOwnMember(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Bravo", "rev-bravo")

	// Demote alice to org-admin of the new org.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob-promo@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))
	seedMembershipRow(t, st.td, st.userID, org.ID, nil, domain.MembershipRoleOrgAdmin)

	// Carol is a member of the same org.
	carolID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "carol@example.org"},
	)
	require.NoError(t, err)

	// Need a team for the member-role row.
	teamID := uuid.New()
	_, err = st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, org.ID, "Team", "rev-bravo-team.example.org")
	require.NoError(t, err)
	seedMembershipRow(t, st.td, carolID, org.ID, &teamID, domain.MembershipRoleMember)

	carolMint := mintForUser(t, st.td, carolID)

	// Alice (org-admin) revokes carol's sessions.
	rec := st.signedCall(t, http.MethodPost,
		"/orgs/"+org.ID.String()+"/members/"+carolID.String()+"/revoke-sessions", nil)
	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp struct {
		RevokedCount int64 `json:"revoked_count"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, int64(1), resp.RevokedCount)

	got, err := database.NewSessionRepo(st.td.Pool).FindByKeyID(context.Background(), carolMint.KeyID)
	require.NoError(t, err)
	require.NotNil(t, got.RevokedReason)
	assert.Equal(t, domain.SessionRevokeReasonAdminForceLogout, *got.RevokedReason)
}

func TestRevokeMemberSessions_OrgAdminCannotReachDifferentOrg(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	orgA := createOrgAndCapture(t, st, "OrgA", "rev-orga")
	orgB := createOrgAndCapture(t, st, "OrgB", "rev-orgb")

	// Demote alice to org-admin of orgA.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob-cross@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))
	seedMembershipRow(t, st.td, st.userID, orgA.ID, nil, domain.MembershipRoleOrgAdmin)

	// Carol is org-admin of orgB.
	carolID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "carol-cross@example.org"},
	)
	require.NoError(t, err)
	seedMembershipRow(t, st.td, carolID, orgB.ID, nil, domain.MembershipRoleOrgAdmin)
	carolMint := mintForUser(t, st.td, carolID)

	// Alice (org-admin of orgA) tries to revoke carol's sessions via
	// /orgs/{orgA}/members/{carol}/revoke-sessions. Carol isn't a
	// member of orgA, so the response is 404 — the same shape as
	// "user doesn't exist", so alice can't probe across tenants.
	rec := st.signedCall(t, http.MethodPost,
		"/orgs/"+orgA.ID.String()+"/members/"+carolID.String()+"/revoke-sessions", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// And alice can't reach orgB's route at all (403 from authz gate).
	rec = st.signedCall(t, http.MethodPost,
		"/orgs/"+orgB.ID.String()+"/members/"+carolID.String()+"/revoke-sessions", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Carol's session is intact in both cases.
	got, err := database.NewSessionRepo(st.td.Pool).FindByKeyID(context.Background(), carolMint.KeyID)
	require.NoError(t, err)
	assert.True(t, got.Valid())
}

func TestRevokeMemberSessions_NonAdminCallerIs403(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Charlie", "rev-charlie")

	// Demote alice to plain member.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob-other-super@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))

	teamID := uuid.New()
	_, err = st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, org.ID, "Team", "rev-charlie-team.example.org")
	require.NoError(t, err)
	seedMembershipRow(t, st.td, st.userID, org.ID, &teamID, domain.MembershipRoleMember)

	// Alice (now a member) tries to force-logout a hypothetical user.
	rec := st.signedCall(t, http.MethodPost,
		"/orgs/"+org.ID.String()+"/members/"+uuid.New().String()+"/revoke-sessions", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRevokeMemberSessions_UnknownUserIs404(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Delta", "rev-delta")

	rec := st.signedCall(t, http.MethodPost,
		"/orgs/"+org.ID.String()+"/members/"+uuid.New().String()+"/revoke-sessions", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRevokeMemberSessions_NoActiveSessionsReturns0(t *testing.T) {
	// FR-030b idempotency: revoking when the user has no active
	// sessions is a no-op success (0 revoked). The action stays
	// safe to retry from the frontend.
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Echo", "rev-echo")

	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob-no-sess@example.org"},
	)
	require.NoError(t, err)
	seedMembershipRow(t, st.td, bobID, org.ID, nil, domain.MembershipRoleOrgAdmin)
	// Note: no session minted for bob.

	rec := st.signedCall(t, http.MethodPost,
		"/orgs/"+org.ID.String()+"/members/"+bobID.String()+"/revoke-sessions", nil)
	require.Equal(t, http.StatusAccepted, rec.Code)

	var resp struct {
		RevokedCount int64 `json:"revoked_count"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, int64(0), resp.RevokedCount)
}

func TestRevokeMemberSessions_InvalidUserIDIs400(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Foxtrot", "rev-foxtrot")

	rec := st.signedCall(t, http.MethodPost,
		"/orgs/"+org.ID.String()+"/members/not-a-uuid/revoke-sessions", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
