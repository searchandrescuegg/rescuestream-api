package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/handler"
	"github.com/searchandrescuegg/rescuestream-api/internal/pepper"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

// makeIdentityUser is a small fixture helper for this test file. We can't
// reuse the database_test helper of the same name because it lives in a
// different test package.
func makeIdentityUser(t *testing.T, td *testutil.TestDatabase, email string) uuid.UUID {
	t.Helper()
	id, err := database.NewUserRepo(td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: email},
	)
	require.NoError(t, err)
	return id
}

// identityStack composes every dependency needed to exercise the
// AuthMiddleware's identity-resolution branch end-to-end.
type identityStack struct {
	mw       *handler.AuthMiddleware
	mint     *service.MintResult
	userID   uuid.UUID
	td       *testutil.TestDatabase
	superAdm *database.SuperAdminRepo
	memberR  *database.MembershipRepo
	orgR     *database.OrganizationRepo
}

func newIdentityStack(t *testing.T) *identityStack {
	t.Helper()
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	hasher, err := pepper.New(authTestPepper)
	require.NoError(t, err)

	sessRepo := database.NewSessionRepo(td.Pool)
	superRepo := database.NewSuperAdminRepo(td.Pool)
	memberRepo := database.NewMembershipRepo(td.Pool)
	orgRepo := database.NewOrganizationRepo(td.Pool)

	sessSvc := service.NewSessionService(sessRepo, hasher)
	identityRes := service.NewIdentityResolver(superRepo, memberRepo, orgRepo)

	userID, err := database.NewUserRepo(td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "alice@example.org"},
	)
	require.NoError(t, err)

	mint, err := sessSvc.Mint(context.Background(), service.MintInput{UserID: userID})
	require.NoError(t, err)

	return &identityStack{
		mw:       handler.NewAuthMiddleware(sessSvc, identityRes, nil),
		mint:     mint,
		userID:   userID,
		td:       td,
		superAdm: superRepo,
		memberR:  memberRepo,
		orgR:     orgRepo,
	}
}

// signedAndCall fires a signed GET / request through the middleware and
// returns the recorder + captured CallerIdentity (nil if the inner handler
// never ran).
func signedAndCall(t *testing.T, st *identityStack) (*httptest.ResponseRecorder, *domain.CallerIdentity) {
	t.Helper()
	ts := time.Now().Unix()
	sig := service.SignRequest(st.mint.SigningKey, http.MethodGet, "/", ts, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", st.mint.KeyID)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Timestamp", strconv.FormatInt(ts, 10))

	var captured *domain.CallerIdentity
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = handler.IdentityFromContext(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	})

	rec := httptest.NewRecorder()
	st.mw.Authenticate(inner).ServeHTTP(rec, req)
	return rec, captured
}

func seedMembershipRow(t *testing.T, td *testutil.TestDatabase, userID, orgID uuid.UUID, teamID *uuid.UUID, role domain.MembershipRole) {
	t.Helper()
	_, err := td.Pool.Exec(context.Background(), `
		INSERT INTO organization_memberships (id, user_id, organization_id, team_id, role)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.New(), userID, orgID, teamID, role)
	require.NoError(t, err)
}

func TestAuthMiddleware_Identity_superAdminBypass(t *testing.T) {
	st := newIdentityStack(t)
	require.NoError(t, st.superAdm.Add(context.Background(), st.userID, nil, true))

	rec, ident := signedAndCall(t, st)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, ident, "inner handler should run and observe an identity")
	assert.True(t, ident.IsSuperAdmin())
	assert.Equal(t, st.userID, ident.UserID)
	assert.Equal(t, uuid.Nil, ident.OrgID, "super-admin without membership has zero OrgID")
}

func TestAuthMiddleware_Identity_orgAdminAttached(t *testing.T) {
	st := newIdentityStack(t)
	defaultOrgID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	seedMembershipRow(t, st.td, st.userID, defaultOrgID, nil, domain.MembershipRoleOrgAdmin)

	rec, ident := signedAndCall(t, st)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, ident)
	assert.Equal(t, domain.CallerRoleOrgAdmin, ident.Role)
	assert.Equal(t, defaultOrgID, ident.OrgID)
	assert.Nil(t, ident.TeamID, "org-admin without team carries nil TeamID")
	assert.Equal(t, domain.OrgStatusActive, ident.OrgStatus)
}

func TestAuthMiddleware_Identity_memberAttached(t *testing.T) {
	st := newIdentityStack(t)

	// Build a fresh org + team for the membership.
	creator := makeIdentityUser(t, st.td, "creator@example.org")
	org, err := st.orgR.Create(context.Background(), domain.OrganizationCreate{
		Name: "MemberCo", Slug: "memberco", CreatedByUserID: creator,
	})
	require.NoError(t, err)

	teamID := uuid.New()
	_, err = st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, org.ID, "MemberCo Team", "member.example.org")
	require.NoError(t, err)

	seedMembershipRow(t, st.td, st.userID, org.ID, &teamID, domain.MembershipRoleMember)

	rec, ident := signedAndCall(t, st)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, ident)
	assert.Equal(t, domain.CallerRoleMember, ident.Role)
	assert.Equal(t, org.ID, ident.OrgID)
	require.NotNil(t, ident.TeamID)
	assert.Equal(t, teamID, *ident.TeamID)
	assert.Equal(t, domain.OrgStatusActive, ident.OrgStatus)
}

func TestAuthMiddleware_Identity_noMembershipReturnsNoOrgMembership(t *testing.T) {
	st := newIdentityStack(t)
	// User exists + has a session, but no super-admin row and no membership.

	rec, ident := signedAndCall(t, st)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Nil(t, ident, "inner handler must NOT run when identity resolution rejects")
	assert.Contains(t, rec.Body.String(), "/problems/no-org-membership")
}

func TestAuthMiddleware_Identity_suspendedOrgRejectsMember(t *testing.T) {
	st := newIdentityStack(t)
	defaultOrgID := uuid.MustParse("00000000-0000-4000-8000-000000000002")

	teamID := uuid.New()
	_, err := st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, defaultOrgID, "Default Team", "suspended.example.org")
	require.NoError(t, err)

	seedMembershipRow(t, st.td, st.userID, defaultOrgID, &teamID, domain.MembershipRoleMember)
	require.NoError(t, st.orgR.SetStatus(context.Background(), defaultOrgID, domain.OrgStatusSuspended))

	rec, ident := signedAndCall(t, st)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Nil(t, ident)
	assert.Contains(t, rec.Body.String(), "/problems/org-suspended")
}

func TestAuthMiddleware_Identity_suspendedOrg_superAdminBypasses(t *testing.T) {
	// Super-admins must reach a suspended organization's resources for
	// recovery; the suspension only gates non-super-admin callers.
	st := newIdentityStack(t)
	require.NoError(t, st.superAdm.Add(context.Background(), st.userID, nil, true))

	defaultOrgID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	require.NoError(t, st.orgR.SetStatus(context.Background(), defaultOrgID, domain.OrgStatusSuspended))

	rec, ident := signedAndCall(t, st)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, ident)
	assert.True(t, ident.IsSuperAdmin())
}
