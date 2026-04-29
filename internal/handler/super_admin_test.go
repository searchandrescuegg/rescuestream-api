package handler_test

import (
	"context"
	"encoding/json"
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
	"github.com/searchandrescuegg/rescuestream-api/internal/pepper"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

// superAdminStack assembles the full middleware + handler chain for the
// /super-admins routes against a real Postgres testcontainer.
type superAdminStack struct {
	td       *testutil.TestDatabase
	mw       *handler.AuthMiddleware
	handler  *handler.SuperAdminHandler
	router   *mux.Router
	mint     *service.MintResult
	userID   uuid.UUID
	superAdm *database.SuperAdminRepo
}

func newSuperAdminStack(t *testing.T) *superAdminStack {
	t.Helper()
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	hasher, err := pepper.New(authTestPepper)
	require.NoError(t, err)

	sessRepo := database.NewSessionRepo(td.Pool)
	superRepo := database.NewSuperAdminRepo(td.Pool)
	memberRepo := database.NewMembershipRepo(td.Pool)
	orgRepo := database.NewOrganizationRepo(td.Pool)
	userRepo := database.NewUserRepo(td.Pool)

	sessSvc := service.NewSessionService(sessRepo, hasher)
	identityRes := service.NewIdentityResolver(superRepo, memberRepo, orgRepo)
	saSvc := service.NewSuperAdminService(td.Pool, superRepo, userRepo)

	mw := handler.NewAuthMiddleware(sessSvc, identityRes, nil)
	saHandler := handler.NewSuperAdminHandler(saSvc, nil)

	userID, err := userRepo.Upsert(context.Background(), domain.UserUpsert{Email: "alice@example.org"})
	require.NoError(t, err)

	mint, err := sessSvc.Mint(context.Background(), service.MintInput{UserID: userID})
	require.NoError(t, err)

	r := mux.NewRouter()
	protected := r.PathPrefix("").Subrouter()
	protected.Use(mw.Authenticate)
	sa := protected.PathPrefix("/super-admins").Subrouter()
	sa.Use(handler.RequireSuperAdmin)
	sa.HandleFunc("", saHandler.List).Methods(http.MethodGet)
	sa.HandleFunc("", saHandler.Add).Methods(http.MethodPost)
	sa.HandleFunc("/{user_id}", saHandler.Remove).Methods(http.MethodDelete)

	return &superAdminStack{
		td: td, mw: mw, handler: saHandler, router: r,
		mint: mint, userID: userID, superAdm: superRepo,
	}
}

// signedCall fires a signed request through the router. body is nil for
// GET/DELETE.
func (st *superAdminStack) signedCall(t *testing.T, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	ts := time.Now().Unix()
	sig := service.SignRequest(st.mint.SigningKey, method, path, ts, body)
	var rdr *strings.Reader
	if body != nil {
		rdr = strings.NewReader(string(body))
	}
	var req *http.Request
	if rdr != nil {
		req = httptest.NewRequest(method, path, rdr)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("X-API-Key", st.mint.KeyID)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Timestamp", strconv.FormatInt(ts, 10))

	rec := httptest.NewRecorder()
	st.router.ServeHTTP(rec, req)
	return rec
}

func TestSuperAdminHandler_NonSuperAdminGets403(t *testing.T) {
	st := newSuperAdminStack(t)
	// Alice is not a super-admin AND has no membership — AuthMiddleware
	// should reject her with /problems/no-org-membership before reaching
	// the super-admins route. (Adding a membership row would let her
	// pass auth and hit RequireSuperAdmin's 403.) Both outcomes prove the
	// route is unreachable to non-super-admins; we assert the upstream
	// no-org check fires.
	rec := st.signedCall(t, http.MethodGet, "/super-admins", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/no-org-membership")
}

func TestSuperAdminHandler_NonSuperAdminMemberGets403(t *testing.T) {
	st := newSuperAdminStack(t)
	// Make alice an org-admin of the default org so she passes auth + no-org.
	defaultOrgID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	seedMembershipRow(t, st.td, st.userID, defaultOrgID, nil, domain.MembershipRoleOrgAdmin)

	rec := st.signedCall(t, http.MethodGet, "/super-admins", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "super-admin privileges")
}

func TestSuperAdminHandler_ListEmpty(t *testing.T) {
	st := newSuperAdminStack(t)
	require.NoError(t, st.superAdm.Add(context.Background(), st.userID, nil, true))

	rec := st.signedCall(t, http.MethodGet, "/super-admins", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		SuperAdmins []service.SuperAdminEntry `json:"super_admins"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.Len(t, got.SuperAdmins, 1, "exactly one super-admin (alice)")
	assert.Equal(t, "alice@example.org", got.SuperAdmins[0].Email)
	assert.Equal(t, st.userID, got.SuperAdmins[0].UserID)
}

func TestSuperAdminHandler_AddByEmail(t *testing.T) {
	st := newSuperAdminStack(t)
	require.NoError(t, st.superAdm.Add(context.Background(), st.userID, nil, true))

	body := []byte(`{"email":"BOB@example.ORG"}`)
	rec := st.signedCall(t, http.MethodPost, "/super-admins", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	var entry service.SuperAdminEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entry))
	assert.Equal(t, "bob@example.org", entry.Email, "email must be normalized")
	require.NotNil(t, entry.GrantedByUserID)
	assert.Equal(t, st.userID, *entry.GrantedByUserID, "GrantedByUserID = caller")
	assert.False(t, entry.SeededFromEnv)

	// Idempotent: posting the same email a second time succeeds with 201
	// returning the same row (Add ON CONFLICT DO NOTHING).
	rec2 := st.signedCall(t, http.MethodPost, "/super-admins", body)
	require.Equal(t, http.StatusCreated, rec2.Code)
	var entry2 service.SuperAdminEntry
	require.NoError(t, json.NewDecoder(rec2.Body).Decode(&entry2))
	assert.Equal(t, entry.ID, entry2.ID, "second Add returns the same row id")
}

func TestSuperAdminHandler_AddRejectsBlankEmail(t *testing.T) {
	st := newSuperAdminStack(t)
	require.NoError(t, st.superAdm.Add(context.Background(), st.userID, nil, true))

	rec := st.signedCall(t, http.MethodPost, "/super-admins", []byte(`{"email":""}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "`email` is required")
}

func TestSuperAdminHandler_RemoveSucceedsWhenMoreThanOneRemains(t *testing.T) {
	st := newSuperAdminStack(t)
	require.NoError(t, st.superAdm.Add(context.Background(), st.userID, nil, true))

	// Add bob via the API.
	rec := st.signedCall(t, http.MethodPost, "/super-admins", []byte(`{"email":"bob@example.org"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var bob service.SuperAdminEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&bob))

	// Remove bob.
	rec = st.signedCall(t, http.MethodDelete, "/super-admins/"+bob.UserID.String(), nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Confirm via repo.
	exists, err := st.superAdm.IsSuperAdmin(context.Background(), bob.UserID)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSuperAdminHandler_RemoveLastReturns409LastSuperAdmin(t *testing.T) {
	st := newSuperAdminStack(t)
	require.NoError(t, st.superAdm.Add(context.Background(), st.userID, nil, true))

	rec := st.signedCall(t, http.MethodDelete, "/super-admins/"+st.userID.String(), nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/last-super-admin")

	// Alice is still a super-admin.
	exists, err := st.superAdm.IsSuperAdmin(context.Background(), st.userID)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestSuperAdminHandler_RemoveAbsentReturns404(t *testing.T) {
	st := newSuperAdminStack(t)
	require.NoError(t, st.superAdm.Add(context.Background(), st.userID, nil, true))

	// Add a second so we're past the last-super-admin guard.
	other, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "carol@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), other, nil, false))

	// Try to delete a UUID that isn't a super-admin.
	rec := st.signedCall(t, http.MethodDelete, "/super-admins/"+uuid.New().String(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSuperAdminHandler_RemoveInvalidUUID(t *testing.T) {
	st := newSuperAdminStack(t)
	require.NoError(t, st.superAdm.Add(context.Background(), st.userID, nil, true))

	rec := st.signedCall(t, http.MethodDelete, "/super-admins/not-a-uuid", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
