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
	"github.com/searchandrescuegg/rescuestream-api/internal/pepper"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

// teamStack assembles the AuthMiddleware + TeamHandler against
// testcontainers Postgres.
type teamStack struct {
	td       *testutil.TestDatabase
	router   *mux.Router
	mint     *service.MintResult
	userID   uuid.UUID
	superAdm *database.SuperAdminRepo
	orgRepo  *database.OrganizationRepo
	teamRepo *database.TeamRepo
}

func newTeamStack(t *testing.T) *teamStack {
	t.Helper()
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	hasher, err := pepper.New(authTestPepper)
	require.NoError(t, err)

	sessRepo := database.NewSessionRepo(td.Pool)
	superRepo := database.NewSuperAdminRepo(td.Pool)
	memberRepo := database.NewMembershipRepo(td.Pool)
	orgRepo := database.NewOrganizationRepo(td.Pool)
	teamRepo := database.NewTeamRepo(td.Pool)
	userRepo := database.NewUserRepo(td.Pool)

	sessSvc := service.NewSessionService(sessRepo, hasher)
	identityRes := service.NewIdentityResolver(superRepo, memberRepo, orgRepo)
	teamSvc := service.NewTeamService(teamRepo, memberRepo, sessSvc)

	mw := handler.NewAuthMiddleware(sessSvc, identityRes, nil)
	teamHandler := handler.NewTeamHandler(teamSvc, nil)

	userID, err := userRepo.Upsert(context.Background(), domain.UserUpsert{Email: "alice@example.org"})
	require.NoError(t, err)
	mint, err := sessSvc.Mint(context.Background(), service.MintInput{UserID: userID})
	require.NoError(t, err)

	r := mux.NewRouter()
	protected := r.PathPrefix("").Subrouter()
	protected.Use(mw.Authenticate)
	protected.HandleFunc("/orgs/{org_id}/teams", teamHandler.Create).Methods(http.MethodPost)
	protected.HandleFunc("/orgs/{org_id}/teams", teamHandler.ListByOrg).Methods(http.MethodGet)
	protected.HandleFunc("/teams/{team_id}", teamHandler.Get).Methods(http.MethodGet)
	protected.HandleFunc("/teams/{team_id}", teamHandler.Update).Methods(http.MethodPatch)
	protected.HandleFunc("/teams/{team_id}", teamHandler.Delete).Methods(http.MethodDelete)

	return &teamStack{
		td: td, router: r, mint: mint, userID: userID,
		superAdm: superRepo, orgRepo: orgRepo, teamRepo: teamRepo,
	}
}

func (st *teamStack) signedCall(t *testing.T, method, fullURL string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
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

// makeOrg creates an org and returns its id. Helper for the handler tests.
func (st *teamStack) makeOrg(t *testing.T, slug string) uuid.UUID {
	t.Helper()
	creator, err := database.NewUserRepo(st.td.Pool).Upsert(
		context.Background(), domain.UserUpsert{Email: "creator-" + slug + "@example.org"},
	)
	require.NoError(t, err)
	org, err := st.orgRepo.Create(context.Background(), domain.OrganizationCreate{
		Name: "Org " + slug, Slug: slug, CreatedByUserID: creator,
	})
	require.NoError(t, err)
	return org.ID
}

// makeAliceOrgAdmin makes alice an org-admin of the given org.
func (st *teamStack) makeAliceOrgAdmin(t *testing.T, orgID uuid.UUID) {
	t.Helper()
	seedMembershipRow(t, st.td, st.userID, orgID, nil, domain.MembershipRoleOrgAdmin)
}

// makeAliceMember makes alice a non-admin member of the given team.
func (st *teamStack) makeAliceMember(t *testing.T, orgID, teamID uuid.UUID) {
	t.Helper()
	seedMembershipRow(t, st.td, st.userID, orgID, &teamID, domain.MembershipRoleMember)
}

// --- Tests -----------------------------------------------------------

func TestTeamHandler_Create_HappyPath(t *testing.T) {
	st := newTeamStack(t)
	orgID := st.makeOrg(t, "create-happy")
	st.makeAliceOrgAdmin(t, orgID)

	body := []byte(`{"name":"  KCESAR  ","workspace_domain":"KCESAR.example.org"}`)
	rec := st.signedCall(t, http.MethodPost, "/orgs/"+orgID.String()+"/teams", body)
	require.Equal(t, http.StatusCreated, rec.Code)

	var got domain.Team
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "KCESAR", got.Name, "name must be trimmed")
	assert.Equal(t, "kcesar.example.org", got.WorkspaceDomain, "domain must be lowercased")
	assert.Equal(t, orgID, got.OrganizationID)
}

func TestTeamHandler_Create_MemberCannotCreate(t *testing.T) {
	st := newTeamStack(t)
	orgID := st.makeOrg(t, "member-create")

	// Alice is just a member of some team in this org, not an org-admin.
	teamID := uuid.New()
	_, err := st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, orgID, "Existing", "existing-team.example.org")
	require.NoError(t, err)
	st.makeAliceMember(t, orgID, teamID)

	rec := st.signedCall(t, http.MethodPost, "/orgs/"+orgID.String()+"/teams",
		[]byte(`{"name":"NewTeam","workspace_domain":"new.example.org"}`))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTeamHandler_Create_OrgAdminOfDifferentOrgIs403(t *testing.T) {
	st := newTeamStack(t)
	orgA := st.makeOrg(t, "create-org-a")
	orgB := st.makeOrg(t, "create-org-b")
	st.makeAliceOrgAdmin(t, orgA)

	// Try to create a team in orgB.
	rec := st.signedCall(t, http.MethodPost, "/orgs/"+orgB.String()+"/teams",
		[]byte(`{"name":"NewTeam","workspace_domain":"x.example.org"}`))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTeamHandler_Create_DuplicateDomainIs409(t *testing.T) {
	st := newTeamStack(t)
	orgID := st.makeOrg(t, "create-dup")
	st.makeAliceOrgAdmin(t, orgID)

	body := []byte(`{"name":"First","workspace_domain":"clash.example.org"}`)
	require.Equal(t, http.StatusCreated,
		st.signedCall(t, http.MethodPost, "/orgs/"+orgID.String()+"/teams", body).Code)

	body = []byte(`{"name":"Second","workspace_domain":"clash.example.org"}`)
	rec := st.signedCall(t, http.MethodPost, "/orgs/"+orgID.String()+"/teams", body)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/workspace-domain-taken")
}

func TestTeamHandler_Create_BadDomainIs400(t *testing.T) {
	st := newTeamStack(t)
	orgID := st.makeOrg(t, "create-bad")
	st.makeAliceOrgAdmin(t, orgID)

	for _, bad := range []string{
		`{"name":"X","workspace_domain":""}`,
		`{"name":"X","workspace_domain":"no-dot"}`,
		`{"name":"X","workspace_domain":"bad space.example.org"}`,
		`{"name":"","workspace_domain":"valid.example.org"}`,
	} {
		rec := st.signedCall(t, http.MethodPost, "/orgs/"+orgID.String()+"/teams", []byte(bad))
		assert.Equal(t, http.StatusBadRequest, rec.Code, "bad input %q", bad)
	}
}

func TestTeamHandler_ListByOrg_MembersCanRead(t *testing.T) {
	st := newTeamStack(t)
	orgID := st.makeOrg(t, "list-by-org")

	// Seed a team and make alice a member of it.
	teamID := uuid.New()
	_, err := st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, orgID, "Existing", "existing-list.example.org")
	require.NoError(t, err)
	st.makeAliceMember(t, orgID, teamID)

	rec := st.signedCall(t, http.MethodGet, "/orgs/"+orgID.String()+"/teams", nil)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Teams []domain.Team `json:"teams"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Teams, 1)
	assert.Equal(t, "Existing", resp.Teams[0].Name)
}

func TestTeamHandler_ListByOrg_OutsiderIsRejected(t *testing.T) {
	st := newTeamStack(t)
	orgA := st.makeOrg(t, "list-a")
	orgB := st.makeOrg(t, "list-b")

	// Alice is org-admin of orgA but tries to list orgB.
	st.makeAliceOrgAdmin(t, orgA)

	rec := st.signedCall(t, http.MethodGet, "/orgs/"+orgB.String()+"/teams", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/not-in-org")
}

func TestTeamHandler_Get_OutsiderSees404(t *testing.T) {
	st := newTeamStack(t)
	orgA := st.makeOrg(t, "get-out-a")
	orgB := st.makeOrg(t, "get-out-b")
	st.makeAliceOrgAdmin(t, orgA)

	// Seed a team in orgB.
	teamID := uuid.New()
	_, err := st.td.Pool.Exec(context.Background(), `
		INSERT INTO teams (id, organization_id, name, workspace_domain)
		VALUES ($1, $2, $3, $4)
	`, teamID, orgB, "Hidden", "hidden.example.org")
	require.NoError(t, err)

	// Alice (org-admin of orgA) cannot see orgB's team — surfaces as 404
	// rather than 403 to avoid leaking team existence.
	rec := st.signedCall(t, http.MethodGet, "/teams/"+teamID.String(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTeamHandler_Get_NotFound(t *testing.T) {
	st := newTeamStack(t)
	orgID := st.makeOrg(t, "get-not-found")
	st.makeAliceOrgAdmin(t, orgID)

	rec := st.signedCall(t, http.MethodGet, "/teams/"+uuid.New().String(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTeamHandler_Update_OrgAdminCanRename(t *testing.T) {
	st := newTeamStack(t)
	orgID := st.makeOrg(t, "update-rename")
	st.makeAliceOrgAdmin(t, orgID)

	created, err := st.teamRepo.Create(context.Background(), domain.TeamCreate{
		OrganizationID: orgID, Name: "Old", WorkspaceDomain: "update-rename.example.org",
	})
	require.NoError(t, err)

	rec := st.signedCall(t, http.MethodPatch, "/teams/"+created.ID.String(),
		[]byte(`{"name":"New"}`))
	require.Equal(t, http.StatusOK, rec.Code)

	var got domain.Team
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, "New", got.Name)
	assert.Equal(t, "update-rename.example.org", got.WorkspaceDomain, "domain must remain unchanged")
}

func TestTeamHandler_Update_OutsiderSees404(t *testing.T) {
	st := newTeamStack(t)
	orgA := st.makeOrg(t, "update-a")
	orgB := st.makeOrg(t, "update-b")
	st.makeAliceOrgAdmin(t, orgA)

	created, err := st.teamRepo.Create(context.Background(), domain.TeamCreate{
		OrganizationID: orgB, Name: "Hidden", WorkspaceDomain: "update-hidden.example.org",
	})
	require.NoError(t, err)

	rec := st.signedCall(t, http.MethodPatch, "/teams/"+created.ID.String(),
		[]byte(`{"name":"Should fail"}`))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTeamHandler_Update_DomainConflictIs409(t *testing.T) {
	st := newTeamStack(t)
	orgID := st.makeOrg(t, "update-conflict")
	st.makeAliceOrgAdmin(t, orgID)

	first, err := st.teamRepo.Create(context.Background(), domain.TeamCreate{
		OrganizationID: orgID, Name: "First", WorkspaceDomain: "first-conflict.example.org",
	})
	require.NoError(t, err)
	second, err := st.teamRepo.Create(context.Background(), domain.TeamCreate{
		OrganizationID: orgID, Name: "Second", WorkspaceDomain: "second-conflict.example.org",
	})
	require.NoError(t, err)

	rec := st.signedCall(t, http.MethodPatch, "/teams/"+second.ID.String(),
		[]byte(`{"workspace_domain":"first-conflict.example.org"}`))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/workspace-domain-taken")

	// First team's row is intact.
	got, err := st.teamRepo.FindByID(context.Background(), first.ID)
	require.NoError(t, err)
	assert.Equal(t, "first-conflict.example.org", got.WorkspaceDomain)
}

func TestTeamHandler_Delete_OrgAdminCanDelete(t *testing.T) {
	st := newTeamStack(t)
	orgID := st.makeOrg(t, "delete-ok")
	st.makeAliceOrgAdmin(t, orgID)

	created, err := st.teamRepo.Create(context.Background(), domain.TeamCreate{
		OrganizationID: orgID, Name: "Doomed", WorkspaceDomain: "delete-ok.example.org",
	})
	require.NoError(t, err)

	rec := st.signedCall(t, http.MethodDelete, "/teams/"+created.ID.String(), nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	_, err = st.teamRepo.FindByID(context.Background(), created.ID)
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestTeamHandler_Delete_OutsiderSees404(t *testing.T) {
	st := newTeamStack(t)
	orgA := st.makeOrg(t, "delete-out-a")
	orgB := st.makeOrg(t, "delete-out-b")
	st.makeAliceOrgAdmin(t, orgA)

	created, err := st.teamRepo.Create(context.Background(), domain.TeamCreate{
		OrganizationID: orgB, Name: "Hidden", WorkspaceDomain: "delete-out.example.org",
	})
	require.NoError(t, err)

	rec := st.signedCall(t, http.MethodDelete, "/teams/"+created.ID.String(), nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Team is intact.
	_, err = st.teamRepo.FindByID(context.Background(), created.ID)
	assert.NoError(t, err)
}

func TestTeamHandler_SuperAdminCanDoAnything(t *testing.T) {
	st := newTeamStack(t)
	require.NoError(t, st.superAdm.Add(context.Background(), st.userID, nil, true))
	orgID := st.makeOrg(t, "superadmin-all")

	// Create.
	rec := st.signedCall(t, http.MethodPost, "/orgs/"+orgID.String()+"/teams",
		[]byte(`{"name":"SA","workspace_domain":"sa-team.example.org"}`))
	require.Equal(t, http.StatusCreated, rec.Code)
	var team domain.Team
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&team))

	// List.
	rec = st.signedCall(t, http.MethodGet, "/orgs/"+orgID.String()+"/teams", nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Get.
	rec = st.signedCall(t, http.MethodGet, "/teams/"+team.ID.String(), nil)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Update.
	rec = st.signedCall(t, http.MethodPatch, "/teams/"+team.ID.String(), []byte(`{"name":"SA-renamed"}`))
	assert.Equal(t, http.StatusOK, rec.Code)

	// Delete.
	rec = st.signedCall(t, http.MethodDelete, "/teams/"+team.ID.String(), nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}
