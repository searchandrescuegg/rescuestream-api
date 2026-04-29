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
	"github.com/searchandrescuegg/rescuestream-api/internal/pepper"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

// createOrgAndCapture POSTs /orgs as a super-admin and returns the
// resulting org. Helper for the AddAdmin/RemoveAdmin test setup.
func createOrgAndCapture(t *testing.T, st *orgStack, name, slug string) domain.Organization {
	t.Helper()
	rec := st.signedCall(t, http.MethodPost, "/orgs",
		[]byte(`{"name":"`+name+`","slug":"`+slug+`"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var org domain.Organization
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&org))
	return org
}

func TestOrgAdmins_AddAdmin_HappyPath(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	org := createOrgAndCapture(t, st, "Alpha", "alpha-add")

	body := []byte(`{"email":"NEW@example.org"}`)
	rec := st.signedCall(t, http.MethodPost, "/orgs/"+org.ID.String()+"/admins", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	var m domain.OrganizationMembership
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&m))
	assert.Equal(t, org.ID, m.OrganizationID)
	assert.Equal(t, domain.MembershipRoleOrgAdmin, m.Role)
	assert.Nil(t, m.TeamID, "org-admin without team has nil team_id")

	// User row was upserted by email (normalized).
	user, err := database.NewUserRepo(st.td.Pool).FindByEmail(context.Background(), "new@example.org")
	require.NoError(t, err)
	assert.Equal(t, m.UserID, user.ID)
}

func TestOrgAdmins_AddAdmin_BlankEmailIs400(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Beta", "beta-add")

	rec := st.signedCall(t, http.MethodPost, "/orgs/"+org.ID.String()+"/admins",
		[]byte(`{"email":"   "}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOrgAdmins_AddAdmin_OrgAdminCannotPromote(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Charlie", "charlie-add")

	// Demote alice to org-admin of the new org.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))
	seedMembershipRow(t, st.td, st.userID, org.ID, nil, domain.MembershipRoleOrgAdmin)

	rec := st.signedCall(t, http.MethodPost, "/orgs/"+org.ID.String()+"/admins",
		[]byte(`{"email":"target@example.org"}`))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOrgAdmins_AddAdmin_UnknownOrgIs404(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	rec := st.signedCall(t, http.MethodPost, "/orgs/"+uuid.New().String()+"/admins",
		[]byte(`{"email":"someone@example.org"}`))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOrgAdmins_AddAdmin_ReassignsAcrossOrgsAndRevokesSessions(t *testing.T) {
	// FR-002: a user holds at most one membership at a time. Calling
	// AddAdmin on a user who already belongs to a different org must
	// reassign them — the prior membership is overwritten and any
	// active sessions revoked.
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	orgA := createOrgAndCapture(t, st, "OrgA", "org-a")
	orgB := createOrgAndCapture(t, st, "OrgB", "org-b")

	// Step 1: bob has a session and is org-admin of orgA.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob@example.org"},
	)
	require.NoError(t, err)
	seedMembershipRow(t, st.td, bobID, orgA.ID, nil, domain.MembershipRoleOrgAdmin)

	bobMint := mintForUser(t, st.td, bobID)

	// Step 2: alice (super-admin) reassigns bob to orgB as admin.
	rec := st.signedCall(t, http.MethodPost, "/orgs/"+orgB.ID.String()+"/admins",
		[]byte(`{"email":"bob@example.org"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var m domain.OrganizationMembership
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&m))
	assert.Equal(t, orgB.ID, m.OrganizationID)
	assert.Equal(t, bobID, m.UserID)

	// Step 3: bob's session is revoked.
	sess, err := database.NewSessionRepo(st.td.Pool).FindByKeyID(context.Background(), bobMint.KeyID)
	require.NoError(t, err)
	require.NotNil(t, sess.RevokedAt)
	require.NotNil(t, sess.RevokedReason)
	assert.Equal(t, domain.SessionRevokeReasonRoleChanged, *sess.RevokedReason)
}

func TestOrgAdmins_RemoveAdmin_HappyPath(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Delta", "delta-remove")

	// Add bob as admin via the API.
	rec := st.signedCall(t, http.MethodPost, "/orgs/"+org.ID.String()+"/admins",
		[]byte(`{"email":"bob@example.org"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var m domain.OrganizationMembership
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&m))

	// Mint a session for bob so we can verify revocation.
	bobMint := mintForUser(t, st.td, m.UserID)

	// Remove him.
	rec = st.signedCall(t, http.MethodDelete, "/orgs/"+org.ID.String()+"/admins/"+m.UserID.String(), nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Membership row gone.
	_, err := database.NewMembershipRepo(st.td.Pool).GetByUser(context.Background(), m.UserID)
	require.ErrorIs(t, err, domain.ErrNotFound)

	// Session revoked with reason member-removed.
	sess, err := database.NewSessionRepo(st.td.Pool).FindByKeyID(context.Background(), bobMint.KeyID)
	require.NoError(t, err)
	require.NotNil(t, sess.RevokedReason)
	assert.Equal(t, domain.SessionRevokeReasonMembershipRemoved, *sess.RevokedReason)
}

func TestOrgAdmins_RemoveAdmin_NotAnAdminOfThatOrgIs404(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	orgA := createOrgAndCapture(t, st, "OrgA", "org-x")
	orgB := createOrgAndCapture(t, st, "OrgB", "org-y")

	// Bob is org-admin of orgA, not orgB.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob@example.org"},
	)
	require.NoError(t, err)
	seedMembershipRow(t, st.td, bobID, orgA.ID, nil, domain.MembershipRoleOrgAdmin)

	// DELETE /orgs/{orgB}/admins/{bob} must return 404 because bob
	// isn't an admin of orgB. Crucially, it must NOT actually remove
	// his orgA membership.
	rec := st.signedCall(t, http.MethodDelete, "/orgs/"+orgB.ID.String()+"/admins/"+bobID.String(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	got, err := database.NewMembershipRepo(st.td.Pool).GetByUser(context.Background(), bobID)
	require.NoError(t, err)
	assert.Equal(t, orgA.ID, got.OrganizationID, "bob's orgA admin row must be untouched")
}

func TestOrgAdmins_RemoveAdmin_OnlyRemovesOrgAdminRole(t *testing.T) {
	// A user holding a member-role membership in the target org should
	// also map to ErrNotFound for DELETE /orgs/{id}/admins/{user_id} —
	// they're not an *admin* of the org. This avoids accidental
	// downgrades from a misdirected DELETE.
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Echo", "echo-remove")

	// Create a team in the org so a member-role membership is valid
	// (member rows MUST carry team_id per the CHECK constraint).
	teamID := uuid.New()
	_, err := st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, org.ID, "Team E", "echo-team.example.org")
	require.NoError(t, err)

	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob@example.org"},
	)
	require.NoError(t, err)
	seedMembershipRow(t, st.td, bobID, org.ID, &teamID, domain.MembershipRoleMember)

	rec := st.signedCall(t, http.MethodDelete, "/orgs/"+org.ID.String()+"/admins/"+bobID.String(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Bob's member membership is preserved.
	got, err := database.NewMembershipRepo(st.td.Pool).GetByUser(context.Background(), bobID)
	require.NoError(t, err)
	assert.Equal(t, domain.MembershipRoleMember, got.Role)
}

func TestOrgAdmins_RemoveAdmin_OrgAdminCannotRemoveAnotherAdmin(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Foxtrot", "foxtrot")

	// Demote alice to org-admin of the new org so she can sign in.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))
	seedMembershipRow(t, st.td, st.userID, org.ID, nil, domain.MembershipRoleOrgAdmin)

	// Some other user is also an admin of this org.
	carolID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "carol@example.org"},
	)
	require.NoError(t, err)
	seedMembershipRow(t, st.td, carolID, org.ID, nil, domain.MembershipRoleOrgAdmin)

	// Alice (now just an org-admin) cannot remove carol.
	rec := st.signedCall(t, http.MethodDelete, "/orgs/"+org.ID.String()+"/admins/"+carolID.String(), nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOrgAdmins_RemoveAdmin_InvalidUserIDIs400(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)
	org := createOrgAndCapture(t, st, "Golf", "golf-remove")

	rec := st.signedCall(t, http.MethodDelete, "/orgs/"+org.ID.String()+"/admins/not-a-uuid", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// mintForUser is a small helper used by the AddAdmin/RemoveAdmin tests
// to verify session-revocation side effects. It mints a session for the
// given user using the same pepper the orgStack was built with.
func mintForUser(t *testing.T, td *testutil.TestDatabase, userID uuid.UUID) *service.MintResult {
	t.Helper()
	hasher, err := pepper.New(authTestPepper)
	require.NoError(t, err)
	svc := service.NewSessionService(database.NewSessionRepo(td.Pool), hasher)
	mint, err := svc.Mint(context.Background(), service.MintInput{UserID: userID})
	require.NoError(t, err)
	return mint
}
