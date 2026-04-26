package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

const authTestPepper = "auth-mw-pepper-eeeeeeeeeeeeeeeeeeeeee"

// authStack constructs a minted session and the middleware that gates it.
// Returns (mw, mintResult, userID).
type authStack struct {
	mw     *handler.AuthMiddleware
	mint   *service.MintResult
	userID uuid.UUID
	repo   *database.SessionRepo
}

func newAuthStack(t *testing.T) (*authStack, *testutil.TestDatabase) {
	t.Helper()
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	hasher, err := pepper.New(authTestPepper)
	require.NoError(t, err)

	sessRepo := database.NewSessionRepo(td.Pool)
	svc := service.NewSessionService(sessRepo, hasher)

	userID, err := database.NewUserRepo(td.Pool).Upsert(context.Background(), domain.UserUpsert{Email: "alice@example.org"})
	require.NoError(t, err)

	mint, err := svc.Mint(context.Background(), service.MintInput{UserID: userID})
	require.NoError(t, err)

	return &authStack{
		mw:     handler.NewAuthMiddleware(svc, nil),
		mint:   mint,
		userID: userID,
		repo:   sessRepo,
	}, td
}

// signedRequest builds an HTTP request signed under the given session.
func signedRequest(t *testing.T, signingKey, method, path string, body []byte) (*http.Request, string) {
	t.Helper()
	ts := time.Now().Unix()
	sig := service.SignRequest(signingKey, method, path, ts, body)
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	r := httptest.NewRequest(method, path, rdr)
	r.Header.Set("X-Timestamp", strconv.FormatInt(ts, 10))
	r.Header.Set("X-Signature", sig)
	return r, ts2str(ts)
}

func ts2str(ts int64) string { return strconv.FormatInt(ts, 10) }

// echoHandler is the inner handler used for verifying middleware behavior.
// It writes 200 + JSON containing the auth context the middleware attached.
func echoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"session_id": handler.SessionIDFromContext(r.Context()).String(),
			"user_id":    handler.UserIDFromContext(r.Context()).String(),
			"api_key":    handler.APIKeyFromContext(r.Context()),
		})
	}
}

func TestAuthMiddleware_HappyPath(t *testing.T) {
	st, _ := newAuthStack(t)
	body := []byte(`{"name":"KCSARA"}`)

	req, _ := signedRequest(t, st.mint.SigningKey, http.MethodPost, "/organizations", body)
	req.Header.Set("X-API-Key", st.mint.KeyID)

	rec := httptest.NewRecorder()
	st.mw.Authenticate(echoHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var got map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, st.mint.Session.ID.String(), got["session_id"])
	assert.Equal(t, st.userID.String(), got["user_id"])
	assert.Equal(t, st.mint.KeyID, got["api_key"], "X-API-Key must be exposed via APIKeyFromContext for audit middleware compat")
}

func TestAuthMiddleware_BodyIsRestoredForDownstream(t *testing.T) {
	st, _ := newAuthStack(t)
	body := []byte(`{"echo":"hello"}`)

	req, _ := signedRequest(t, st.mint.SigningKey, http.MethodPost, "/x", body)
	req.Header.Set("X-API-Key", st.mint.KeyID)

	var seenBody []byte
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		seenBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	st.mw.Authenticate(inner).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, body, seenBody, "downstream handler must observe the original body bytes")
}

func TestAuthMiddleware_MissingHeadersIs401(t *testing.T) {
	st, _ := newAuthStack(t)

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	// No X-API-Key / X-Signature / X-Timestamp.
	rec := httptest.NewRecorder()
	st.mw.Authenticate(echoHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "Missing authentication headers")
}

func TestAuthMiddleware_RevokedSessionReturnsSessionInvalidated(t *testing.T) {
	st, _ := newAuthStack(t)
	require.NoError(t, st.repo.Revoke(context.Background(), st.mint.Session.ID, domain.SessionRevokeReasonAdminForceLogout))

	req, _ := signedRequest(t, st.mint.SigningKey, http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", st.mint.KeyID)

	rec := httptest.NewRecorder()
	st.mw.Authenticate(echoHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/session-invalidated")
}

func TestAuthMiddleware_BadSignatureIs401(t *testing.T) {
	st, _ := newAuthStack(t)

	req, _ := signedRequest(t, "different-key-than-the-session", http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", st.mint.KeyID)

	rec := httptest.NewRecorder()
	st.mw.Authenticate(echoHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/session-invalidated")
}

func TestAuthMiddleware_UnknownAPIKeyIs401(t *testing.T) {
	st, _ := newAuthStack(t)

	body := []byte(`{}`)
	req, _ := signedRequest(t, st.mint.SigningKey, http.MethodPost, "/x", body)
	req.Header.Set("X-API-Key", "AAAAAAAAAAAAAAAAAAAAAA") // 22-char nonsense

	rec := httptest.NewRecorder()
	st.mw.Authenticate(echoHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/session-invalidated")
}

func TestAuthMiddleware_DriftedTimestampIs401(t *testing.T) {
	st, _ := newAuthStack(t)

	// 10 minutes in the past — beyond MaxTimestampDrift.
	staleTs := time.Now().Add(-10 * time.Minute).Unix()
	sig := service.SignRequest(st.mint.SigningKey, http.MethodGet, "/x", staleTs, nil)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", st.mint.KeyID)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Timestamp", strconv.FormatInt(staleTs, 10))

	rec := httptest.NewRecorder()
	st.mw.Authenticate(echoHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "/problems/session-invalidated")
}

func TestAuthMiddleware_SuccessSlidesExpiry(t *testing.T) {
	st, _ := newAuthStack(t)
	priorExpiry := st.mint.Session.ExpiresAt

	time.Sleep(time.Second) // observability beyond timestamp resolution

	req, _ := signedRequest(t, st.mint.SigningKey, http.MethodGet, "/x", nil)
	req.Header.Set("X-API-Key", st.mint.KeyID)
	rec := httptest.NewRecorder()
	st.mw.Authenticate(echoHandler()).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	got, err := st.repo.FindByKeyID(context.Background(), st.mint.KeyID)
	require.NoError(t, err)
	assert.True(t, got.ExpiresAt.After(priorExpiry),
		"a successful auth must slide expires_at forward")
}
