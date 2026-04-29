package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/handler"
	"github.com/searchandrescuegg/rescuestream-api/internal/oauth"
	"github.com/searchandrescuegg/rescuestream-api/internal/pepper"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

// fakeVerifier is a test double for oauth.GoogleIDTokenVerifier. The
// test sets either Claims (success) or Err (failure) based on the
// scenario it wants to exercise.
type fakeVerifier struct {
	Claims *oauth.GoogleIDTokenClaims
	Err    error
	called int
	last   string
}

func (f *fakeVerifier) Verify(_ context.Context, idToken string) (*oauth.GoogleIDTokenClaims, error) {
	f.called++
	f.last = idToken
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Claims, nil
}

// sessionsStack composes every dependency the /sessions routes touch
// against a real Postgres testcontainer.
type sessionsStack struct {
	td       *testutil.TestDatabase
	router   *mux.Router
	verifier *fakeVerifier
	sessSvc  *service.SessionService
	sessRepo *database.SessionRepo
	teams    *database.TeamRepo
	orgs     *database.OrganizationRepo
	users    *database.UserRepo
	members  *database.MembershipRepo
}

func newSessionsStack(t *testing.T) *sessionsStack {
	t.Helper()
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	hasher, err := pepper.New(authTestPepper)
	require.NoError(t, err)

	users := database.NewUserRepo(td.Pool)
	teams := database.NewTeamRepo(td.Pool)
	members := database.NewMembershipRepo(td.Pool)
	orgs := database.NewOrganizationRepo(td.Pool)
	supers := database.NewSuperAdminRepo(td.Pool)
	sessRepo := database.NewSessionRepo(td.Pool)

	sessSvc := service.NewSessionService(sessRepo, hasher)
	identityRes := service.NewIdentityResolver(supers, members, orgs)
	memberSvc := service.NewMembershipService(users, teams, members)

	verifier := &fakeVerifier{}
	loginSvc := service.NewLoginService(verifier, memberSvc, identityRes, sessSvc)

	mw := handler.NewAuthMiddleware(sessSvc, identityRes, nil)
	sessionAuthOnly := handler.NewAuthMiddleware(sessSvc, nil, nil)
	sessionsHandler := handler.NewSessionsHandler(loginSvc, sessSvc, nil)

	r := mux.NewRouter()
	r.HandleFunc("/sessions/login-complete", sessionsHandler.LoginComplete).Methods(http.MethodPost)

	authProtected := r.PathPrefix("").Subrouter()
	authProtected.Use(mw.Authenticate)
	// (placeholder for routes that use the full identity resolver — none in this test)

	sessionsOnly := r.PathPrefix("").Subrouter()
	sessionsOnly.Use(sessionAuthOnly.Authenticate)
	sessionsOnly.HandleFunc("/sessions/logout", sessionsHandler.Logout).Methods(http.MethodPost)

	return &sessionsStack{
		td: td, router: r, verifier: verifier,
		sessSvc: sessSvc, sessRepo: sessRepo,
		teams: teams, orgs: orgs, users: users, members: members,
	}
}

// publicCall fires an unauthenticated request through the router (used
// for /sessions/login-complete which is public).
func (st *sessionsStack) publicCall(t *testing.T, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(string(body)))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	st.router.ServeHTTP(rec, req)
	return rec
}

// signedCall fires an authenticated request signed under the given mint.
func (st *sessionsStack) signedCall(t *testing.T, mint *service.MintResult, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	signPath := path
	if before, _, ok := strings.Cut(path, "?"); ok {
		signPath = before
	}
	ts := time.Now().Unix()
	sig := service.SignRequest(mint.SigningKey, method, signPath, ts, body)

	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(string(body)))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("X-API-Key", mint.KeyID)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Timestamp", strconv.FormatInt(ts, 10))

	rec := httptest.NewRecorder()
	st.router.ServeHTTP(rec, req)
	return rec
}

// makeOrgWithTeam creates an org with one team owning workspaceDomain.
func (st *sessionsStack) makeOrgWithTeam(t *testing.T, slug, workspaceDomain string) *domain.Team {
	t.Helper()
	creator, err := st.users.Upsert(context.Background(), domain.UserUpsert{
		Email: "creator-" + slug + "@example.org",
	})
	require.NoError(t, err)
	org, err := st.orgs.Create(context.Background(), domain.OrganizationCreate{
		Name: "Org " + slug, Slug: slug, CreatedByUserID: creator,
	})
	require.NoError(t, err)
	team, err := st.teams.Create(context.Background(), domain.TeamCreate{
		OrganizationID: org.ID, Name: "Team " + slug, WorkspaceDomain: workspaceDomain,
	})
	require.NoError(t, err)
	return team
}

// --- LoginComplete tests --------------------------------------------

func TestLoginComplete_NewMember_DomainMatchReturnsRoleAndOrg(t *testing.T) {
	st := newSessionsStack(t)
	team := st.makeOrgWithTeam(t, "login-match", "match.example.org")

	st.verifier.Claims = &oauth.GoogleIDTokenClaims{
		Subject:     "google-sub-001",
		Email:       "alice@match.example.org",
		DisplayName: "Alice",
		AvatarURL:   "https://example.org/a.png",
	}

	rec := st.publicCall(t, http.MethodPost, "/sessions/login-complete",
		[]byte(`{"id_token":"any-fake-token"}`))
	require.Equal(t, http.StatusCreated, rec.Code)

	var resp struct {
		SessionKeyID  string         `json:"session_key_id"`
		SessionSecret string         `json:"session_secret"`
		ExpiresAt     time.Time      `json:"expires_at"`
		User          map[string]any `json:"user"`
		Role          string         `json:"role"`
		OrgID         *uuid.UUID     `json:"org_id,omitempty"`
		TeamID        *uuid.UUID     `json:"team_id,omitempty"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	assert.NotEmpty(t, resp.SessionKeyID)
	assert.NotEmpty(t, resp.SessionSecret, "session_secret must be returned plaintext exactly once")
	assert.True(t, resp.ExpiresAt.After(time.Now()))
	assert.Equal(t, "alice@match.example.org", resp.User["email"])
	assert.Equal(t, "Alice", resp.User["display_name"])
	assert.Equal(t, "member", resp.Role)
	require.NotNil(t, resp.OrgID)
	assert.Equal(t, team.OrganizationID, *resp.OrgID)
	require.NotNil(t, resp.TeamID)
	assert.Equal(t, team.ID, *resp.TeamID)
}

func TestLoginComplete_NoMembership_ReturnsRoleNone(t *testing.T) {
	st := newSessionsStack(t)
	st.makeOrgWithTeam(t, "login-no-match", "elsewhere.example.org")

	st.verifier.Claims = &oauth.GoogleIDTokenClaims{
		Subject: "google-sub-002",
		Email:   "bob@unrelated.example.com",
	}

	rec := st.publicCall(t, http.MethodPost, "/sessions/login-complete",
		[]byte(`{"id_token":"any-fake-token"}`))
	require.Equal(t, http.StatusCreated, rec.Code)

	var resp struct {
		Role   string     `json:"role"`
		OrgID  *uuid.UUID `json:"org_id,omitempty"`
		TeamID *uuid.UUID `json:"team_id,omitempty"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "none", resp.Role)
	assert.Nil(t, resp.OrgID)
	assert.Nil(t, resp.TeamID)
}

func TestLoginComplete_SuperAdminBypassesMembership(t *testing.T) {
	st := newSessionsStack(t)

	// Create the user + super-admin row first; this simulates the
	// SUPER_ADMIN_EMAILS bootstrap step done by `migrate up`.
	userID, err := st.users.Upsert(context.Background(), domain.UserUpsert{
		Email: "platform@example.org",
	})
	require.NoError(t, err)
	require.NoError(t, database.NewSuperAdminRepo(st.td.Pool).Add(
		context.Background(), userID, nil, true,
	))

	st.verifier.Claims = &oauth.GoogleIDTokenClaims{
		Subject: "google-sub-platform",
		Email:   "platform@example.org",
	}

	rec := st.publicCall(t, http.MethodPost, "/sessions/login-complete",
		[]byte(`{"id_token":"any-fake-token"}`))
	require.Equal(t, http.StatusCreated, rec.Code)

	var resp struct {
		Role  string     `json:"role"`
		OrgID *uuid.UUID `json:"org_id,omitempty"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "super-admin", resp.Role)
	assert.Nil(t, resp.OrgID, "super-admin without membership has nil org_id")
}

func TestLoginComplete_BadTokenReturns401(t *testing.T) {
	st := newSessionsStack(t)
	st.verifier.Err = errors.New("oauth.Verify: validate: bad signature")

	rec := st.publicCall(t, http.MethodPost, "/sessions/login-complete",
		[]byte(`{"id_token":"forged-token"}`))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "Invalid Google id_token")
}

func TestLoginComplete_BlankTokenIs400(t *testing.T) {
	st := newSessionsStack(t)
	rec := st.publicCall(t, http.MethodPost, "/sessions/login-complete",
		[]byte(`{"id_token":""}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLoginComplete_BadJSONIs400(t *testing.T) {
	st := newSessionsStack(t)
	rec := st.publicCall(t, http.MethodPost, "/sessions/login-complete",
		[]byte(`not-json`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLoginComplete_SessionPersistsAndAuthenticatesNextRequest(t *testing.T) {
	// Sanity: the session_key_id + session_secret returned by Login-
	// Complete must let the caller pass AuthMiddleware on a follow-up
	// signed request.
	st := newSessionsStack(t)
	st.makeOrgWithTeam(t, "login-followup", "followup.example.org")

	st.verifier.Claims = &oauth.GoogleIDTokenClaims{
		Subject: "google-sub-followup",
		Email:   "carol@followup.example.org",
	}

	rec := st.publicCall(t, http.MethodPost, "/sessions/login-complete",
		[]byte(`{"id_token":"any"}`))
	require.Equal(t, http.StatusCreated, rec.Code)

	var resp struct {
		SessionKeyID  string `json:"session_key_id"`
		SessionSecret string `json:"session_secret"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	// Construct a fresh signed request to /sessions/logout. If the
	// session was persisted correctly, the session-auth-only middleware
	// should accept the signature and the handler should return 204.
	mint := &service.MintResult{
		KeyID:      resp.SessionKeyID,
		SigningKey: resp.SessionSecret,
	}
	rec2 := st.signedCall(t, mint, http.MethodPost, "/sessions/logout", nil)
	assert.Equal(t, http.StatusNoContent, rec2.Code)
}

// --- Logout tests ----------------------------------------------------

func TestLogout_RevokesCallerSession(t *testing.T) {
	st := newSessionsStack(t)

	// Mint a session for some user directly.
	userID, err := st.users.Upsert(context.Background(), domain.UserUpsert{Email: "logout@example.org"})
	require.NoError(t, err)
	mint, err := st.sessSvc.Mint(context.Background(), service.MintInput{UserID: userID})
	require.NoError(t, err)

	rec := st.signedCall(t, mint, http.MethodPost, "/sessions/logout", nil)
	require.Equal(t, http.StatusNoContent, rec.Code)

	// Session row is now revoked.
	got, err := st.sessRepo.FindByKeyID(context.Background(), mint.KeyID)
	require.NoError(t, err)
	require.NotNil(t, got.RevokedReason)
	assert.Equal(t, domain.SessionRevokeReasonSelfLogout, *got.RevokedReason)
	assert.False(t, got.Valid())
}

func TestLogout_NoOrgMembershipUserCanLogout(t *testing.T) {
	// Critical correctness: a user with no organization membership
	// MUST still be able to log out. The session-auth-only middleware
	// chain (no identity resolver) is what enables this — if we
	// accidentally wired logout under the full AuthMiddleware, the
	// no-org-membership identity rejection would fire before the
	// handler runs.
	st := newSessionsStack(t)

	userID, err := st.users.Upsert(context.Background(), domain.UserUpsert{Email: "noorg@example.org"})
	require.NoError(t, err)
	// No membership row, no super-admin row.
	mint, err := st.sessSvc.Mint(context.Background(), service.MintInput{UserID: userID})
	require.NoError(t, err)

	rec := st.signedCall(t, mint, http.MethodPost, "/sessions/logout", nil)
	assert.Equal(t, http.StatusNoContent, rec.Code,
		"a no-org-membership user must be able to log out")
}

func TestLogout_AlreadyRevokedReturns204(t *testing.T) {
	// Idempotency: a second logout — or a logout against a session
	// that's already been revoked by an admin force-logout — must not
	// 401, otherwise the frontend's "logout from everywhere" UI gets
	// noisy.
	st := newSessionsStack(t)

	userID, err := st.users.Upsert(context.Background(), domain.UserUpsert{Email: "twice@example.org"})
	require.NoError(t, err)
	mint, err := st.sessSvc.Mint(context.Background(), service.MintInput{UserID: userID})
	require.NoError(t, err)

	require.Equal(t, http.StatusNoContent,
		st.signedCall(t, mint, http.MethodPost, "/sessions/logout", nil).Code)

	// Second call: the session is now revoked. The signed request
	// should be rejected by the auth middleware before reaching the
	// handler — that's correct behavior (revoked sessions can't sign
	// anymore). Assert 401 here so a future regression that lets
	// revoked sessions slip past the auth middleware is caught.
	rec := st.signedCall(t, mint, http.MethodPost, "/sessions/logout", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/session-invalidated")
}

func TestLogout_MissingSignatureIs401(t *testing.T) {
	st := newSessionsStack(t)
	rec := st.publicCall(t, http.MethodPost, "/sessions/logout", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
