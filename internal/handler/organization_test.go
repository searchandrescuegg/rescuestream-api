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

// orgStack assembles AuthMiddleware + OrganizationHandler for the /orgs
// routes against a real Postgres testcontainer.
type orgStack struct {
	td       *testutil.TestDatabase
	router   *mux.Router
	mint     *service.MintResult
	userID   uuid.UUID
	superAdm *database.SuperAdminRepo
	orgRepo  *database.OrganizationRepo
}

func newOrgStack(t *testing.T) *orgStack {
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
	orgSvc := service.NewOrganizationService(orgRepo)

	mw := handler.NewAuthMiddleware(sessSvc, identityRes, nil)
	orgHandler := handler.NewOrganizationHandler(orgSvc, nil)

	userID, err := userRepo.Upsert(context.Background(), domain.UserUpsert{Email: "alice@example.org"})
	require.NoError(t, err)
	mint, err := sessSvc.Mint(context.Background(), service.MintInput{UserID: userID})
	require.NoError(t, err)

	r := mux.NewRouter()
	protected := r.PathPrefix("").Subrouter()
	protected.Use(mw.Authenticate)
	org := protected.PathPrefix("/orgs").Subrouter()
	org.HandleFunc("", orgHandler.Create).Methods(http.MethodPost)
	org.HandleFunc("", orgHandler.List).Methods(http.MethodGet)
	org.HandleFunc("/{id}", orgHandler.Get).Methods(http.MethodGet)
	org.HandleFunc("/{id}", orgHandler.Update).Methods(http.MethodPatch)
	org.HandleFunc("/{id}", orgHandler.Delete).Methods(http.MethodDelete)

	return &orgStack{td: td, router: r, mint: mint, userID: userID, superAdm: superRepo, orgRepo: orgRepo}
}

func (st *orgStack) signedCall(t *testing.T, method, fullURL string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	// The HMAC signing string covers METHOD + r.URL.Path + TIMESTAMP +
	// BODY (research §2). Strip the query string before signing so the
	// server side's r.URL.Path-based recompute matches.
	signPath := fullURL
	if before, _, ok := strings.Cut(fullURL, "?"); ok {
		signPath = before
	}

	ts := time.Now().Unix()
	sig := service.SignRequest(st.mint.SigningKey, method, signPath, ts, body)

	var rdr *strings.Reader
	if body != nil {
		rdr = strings.NewReader(string(body))
	}
	var req *http.Request
	if rdr != nil {
		req = httptest.NewRequest(method, fullURL, rdr)
	} else {
		req = httptest.NewRequest(method, fullURL, nil)
	}
	req.Header.Set("X-API-Key", st.mint.KeyID)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Timestamp", strconv.FormatInt(ts, 10))

	rec := httptest.NewRecorder()
	st.router.ServeHTTP(rec, req)
	return rec
}

// makeAlphaSuper makes alice a super-admin so she can hit super-admin-only routes.
func (st *orgStack) makeAliceSuper(t *testing.T) {
	t.Helper()
	require.NoError(t, st.superAdm.Add(context.Background(), st.userID, nil, true))
}

func TestOrganizationHandler_Create_HappyPath(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	body := []byte(`{"name":"  King County SAR  ","slug":"KCSARA"}`)
	rec := st.signedCall(t, http.MethodPost, "/orgs", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	var got domain.Organization
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "King County SAR", got.Name, "name must be trimmed")
	assert.Equal(t, "kcsara", got.Slug, "slug must be lowercased")
	assert.Equal(t, domain.OrgStatusActive, got.Status)
	assert.Equal(t, st.userID, got.CreatedByUserID)
}

func TestOrganizationHandler_Create_DuplicateSlugIs409(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	body := []byte(`{"name":"First","slug":"shared"}`)
	require.Equal(t, http.StatusCreated, st.signedCall(t, http.MethodPost, "/orgs", body).Code)

	body = []byte(`{"name":"Second","slug":"shared"}`)
	rec := st.signedCall(t, http.MethodPost, "/orgs", body)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestOrganizationHandler_Create_InvalidSlugIs400(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	rec := st.signedCall(t, http.MethodPost, "/orgs", []byte(`{"name":"X","slug":"BAD SLUG!"}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOrganizationHandler_Create_BlankNameIs400(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	rec := st.signedCall(t, http.MethodPost, "/orgs", []byte(`{"name":"   ","slug":"valid-slug"}`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOrganizationHandler_Create_RequiresSuperAdmin(t *testing.T) {
	st := newOrgStack(t)
	defaultOrgID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	seedMembershipRow(t, st.td, st.userID, defaultOrgID, nil, domain.MembershipRoleOrgAdmin)

	rec := st.signedCall(t, http.MethodPost, "/orgs", []byte(`{"name":"X","slug":"x-org"}`))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOrganizationHandler_List_RequiresSuperAdmin(t *testing.T) {
	st := newOrgStack(t)
	defaultOrgID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	seedMembershipRow(t, st.td, st.userID, defaultOrgID, nil, domain.MembershipRoleOrgAdmin)

	rec := st.signedCall(t, http.MethodGet, "/orgs", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOrganizationHandler_List_SuperAdminWithSearchAndPagination(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	for _, slug := range []string{"alpha-sar", "bravo-sar", "charlie-rescue"} {
		require.Equal(t, http.StatusCreated,
			st.signedCall(t, http.MethodPost, "/orgs",
				[]byte(`{"name":"`+slug+`","slug":"`+slug+`"}`)).Code)
	}

	// Search "sar": matches alpha + bravo. Default + charlie excluded by name.
	rec := st.signedCall(t, http.MethodGet, "/orgs?q=sar&limit=10", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Orgs       []domain.Organization `json:"orgs"`
		TotalCount int64                 `json:"total_count"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, int64(2), resp.TotalCount)
	assert.Len(t, resp.Orgs, 2)
}

func TestOrganizationHandler_Get_OrgAdminOfTargetSucceeds(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	// Create an org as super-admin, then make alice an org-admin of it
	// and drop her super-admin status — to prove org-admins can read
	// their own org.
	rec := st.signedCall(t, http.MethodPost, "/orgs",
		[]byte(`{"name":"Alpha","slug":"alpha-org"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var created domain.Organization
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))

	// Need a second super-admin first so we can demote alice safely.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))

	seedMembershipRow(t, st.td, st.userID, created.ID, nil, domain.MembershipRoleOrgAdmin)

	rec = st.signedCall(t, http.MethodGet, "/orgs/"+created.ID.String(), nil)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestOrganizationHandler_Get_OrgAdminOfDifferentOrgIsRejected(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	rec := st.signedCall(t, http.MethodPost, "/orgs",
		[]byte(`{"name":"Alpha","slug":"alpha-org"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var alpha domain.Organization
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&alpha))

	// Demote alice and make her org-admin of a DIFFERENT org (the default).
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))

	defaultOrgID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	seedMembershipRow(t, st.td, st.userID, defaultOrgID, nil, domain.MembershipRoleOrgAdmin)

	rec = st.signedCall(t, http.MethodGet, "/orgs/"+alpha.ID.String(), nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/not-in-org")
}

func TestOrganizationHandler_Update_RenameByOrgAdmin(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	rec := st.signedCall(t, http.MethodPost, "/orgs",
		[]byte(`{"name":"Old","slug":"rename-org"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var created domain.Organization
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))

	// Demote alice and make her org-admin of the new org.
	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))
	seedMembershipRow(t, st.td, st.userID, created.ID, nil, domain.MembershipRoleOrgAdmin)

	rec = st.signedCall(t, http.MethodPatch, "/orgs/"+created.ID.String(),
		[]byte(`{"name":"New"}`))
	require.Equal(t, http.StatusOK, rec.Code)

	var got domain.Organization
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "New", got.Name)
}

func TestOrganizationHandler_Update_StatusChangeRequiresSuperAdmin(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	rec := st.signedCall(t, http.MethodPost, "/orgs",
		[]byte(`{"name":"X","slug":"x-org"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var created domain.Organization
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))

	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))
	seedMembershipRow(t, st.td, st.userID, created.ID, nil, domain.MembershipRoleOrgAdmin)

	// Org-admin attempts to suspend their own org — must be rejected.
	rec = st.signedCall(t, http.MethodPatch, "/orgs/"+created.ID.String(),
		[]byte(`{"status":"suspended"}`))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOrganizationHandler_Update_SuperAdminCanSuspend(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	rec := st.signedCall(t, http.MethodPost, "/orgs",
		[]byte(`{"name":"X","slug":"x-org"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var created domain.Organization
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))

	rec = st.signedCall(t, http.MethodPatch, "/orgs/"+created.ID.String(),
		[]byte(`{"status":"suspended"}`))
	require.Equal(t, http.StatusOK, rec.Code)
	var got domain.Organization
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, domain.OrgStatusSuspended, got.Status)
}

func TestOrganizationHandler_Get_NotFound(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	rec := st.signedCall(t, http.MethodGet, "/orgs/"+uuid.New().String(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOrganizationHandler_Delete_RequiresSuperAdmin(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	rec := st.signedCall(t, http.MethodPost, "/orgs",
		[]byte(`{"name":"X","slug":"x-org"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var created domain.Organization
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))

	bobID, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "bob@example.org"},
	)
	require.NoError(t, err)
	require.NoError(t, st.superAdm.Add(context.Background(), bobID, nil, false))
	require.NoError(t, st.superAdm.Remove(context.Background(), st.userID))
	seedMembershipRow(t, st.td, st.userID, created.ID, nil, domain.MembershipRoleOrgAdmin)

	rec = st.signedCall(t, http.MethodDelete, "/orgs/"+created.ID.String(), nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOrganizationHandler_Delete_SuperAdminSucceeds(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	rec := st.signedCall(t, http.MethodPost, "/orgs",
		[]byte(`{"name":"X","slug":"x-org"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var created domain.Organization
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))

	rec = st.signedCall(t, http.MethodDelete, "/orgs/"+created.ID.String(), nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	rec = st.signedCall(t, http.MethodGet, "/orgs/"+created.ID.String(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOrganizationHandler_InitialAdminEmails_AcceptedButDeferred(t *testing.T) {
	st := newOrgStack(t)
	st.makeAliceSuper(t)

	rec := st.signedCall(t, http.MethodPost, "/orgs",
		[]byte(`{"name":"X","slug":"x-org","initial_admin_emails":["future@example.org"]}`))
	// Currently the field is accepted but the orchestration is owed
	// (next commit). 201 is the contract; the warning log signals the
	// defer.
	assert.Equal(t, http.StatusCreated, rec.Code)
}
