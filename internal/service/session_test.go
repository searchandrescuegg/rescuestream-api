package service_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/pepper"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

const testPepper = "test-pepper-eeeeeeeeeeeeeeeeeeeeeeeee"

func newSessionStack(t *testing.T) (*service.SessionService, *database.SessionRepo, *testutil.TestDatabase, uuid.UUID) {
	t.Helper()
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	hasher, err := pepper.New(testPepper)
	require.NoError(t, err)

	repo := database.NewSessionRepo(td.Pool)
	svc := service.NewSessionService(repo, hasher)

	userID, err := database.NewUserRepo(td.Pool).Upsert(context.Background(), domain.UserUpsert{Email: "alice@example.org"})
	require.NoError(t, err)

	return svc, repo, td, userID
}

func TestSessionService_MintReturnsValidSession(t *testing.T) {
	svc, _, _, userID := newSessionStack(t)
	ctx := context.Background()

	res, err := svc.Mint(ctx, service.MintInput{
		UserID:    userID,
		ClientIP:  "192.0.2.1",
		UserAgent: "Test/1.0",
	})
	require.NoError(t, err)
	require.NotNil(t, res.Session)
	assert.True(t, res.Session.Valid())
	assert.NotEmpty(t, res.KeyID)
	assert.NotEmpty(t, res.SigningKey)
	assert.Equal(t, res.SigningKey, res.Session.HMACSecretHash, "SigningKey must equal stored hmac_secret_hash")
}

func TestSessionService_MintProducesDistinctCredentials(t *testing.T) {
	svc, _, _, userID := newSessionStack(t)
	ctx := context.Background()

	a, err := svc.Mint(ctx, service.MintInput{UserID: userID})
	require.NoError(t, err)
	b, err := svc.Mint(ctx, service.MintInput{UserID: userID})
	require.NoError(t, err)

	assert.NotEqual(t, a.KeyID, b.KeyID)
	assert.NotEqual(t, a.SigningKey, b.SigningKey)
}

func TestSessionService_ValidateSignedRequest_happyPath(t *testing.T) {
	svc, _, _, userID := newSessionStack(t)
	ctx := context.Background()

	mint, err := svc.Mint(ctx, service.MintInput{UserID: userID})
	require.NoError(t, err)

	ts := time.Now().Unix()
	body := []byte(`{"name":"KCSARA"}`)
	sig := service.SignRequest(mint.SigningKey, "POST", "/organizations", ts, body)

	got, err := svc.ValidateSignedRequest(ctx, service.SignedRequest{
		APIKey:       mint.KeyID,
		Signature:    sig,
		TimestampStr: strconv.FormatInt(ts, 10),
		Method:       "POST",
		Path:         "/organizations",
		Body:         body,
	})
	require.NoError(t, err)
	assert.Equal(t, mint.Session.ID, got.ID)
}

func TestSessionService_ValidateSignedRequest_revokedReturnsErrSessionInvalidated(t *testing.T) {
	svc, repo, _, userID := newSessionStack(t)
	ctx := context.Background()

	mint, err := svc.Mint(ctx, service.MintInput{UserID: userID})
	require.NoError(t, err)
	require.NoError(t, repo.Revoke(ctx, mint.Session.ID, domain.SessionRevokeReasonAdminForceLogout))

	ts := time.Now().Unix()
	sig := service.SignRequest(mint.SigningKey, "GET", "/health", ts, nil)
	_, err = svc.ValidateSignedRequest(ctx, service.SignedRequest{
		APIKey:       mint.KeyID,
		Signature:    sig,
		TimestampStr: strconv.FormatInt(ts, 10),
		Method:       "GET",
		Path:         "/health",
	})
	assert.True(t, errors.Is(err, domain.ErrSessionInvalidated))
}

func TestSessionService_ValidateSignedRequest_unknownKeyReturnsErrSessionInvalidated(t *testing.T) {
	svc, _, _, _ := newSessionStack(t)

	ts := time.Now().Unix()
	_, err := svc.ValidateSignedRequest(context.Background(), service.SignedRequest{
		APIKey:       "no-such-key",
		Signature:    "deadbeef",
		TimestampStr: strconv.FormatInt(ts, 10),
		Method:       "GET",
		Path:         "/health",
	})
	assert.True(t, errors.Is(err, domain.ErrSessionInvalidated))
}

func TestSessionService_ValidateSignedRequest_badSignatureReturnsErrSessionInvalidated(t *testing.T) {
	svc, _, _, userID := newSessionStack(t)
	ctx := context.Background()

	mint, err := svc.Mint(ctx, service.MintInput{UserID: userID})
	require.NoError(t, err)

	ts := time.Now().Unix()
	wrongSig := service.SignRequest("some-other-key", "GET", "/health", ts, nil)
	_, err = svc.ValidateSignedRequest(ctx, service.SignedRequest{
		APIKey:       mint.KeyID,
		Signature:    wrongSig,
		TimestampStr: strconv.FormatInt(ts, 10),
		Method:       "GET",
		Path:         "/health",
	})
	assert.True(t, errors.Is(err, domain.ErrSessionInvalidated))
}

func TestSessionService_ValidateSignedRequest_timestampDriftRejected(t *testing.T) {
	svc, _, _, userID := newSessionStack(t)
	ctx := context.Background()

	mint, err := svc.Mint(ctx, service.MintInput{UserID: userID})
	require.NoError(t, err)

	// 10 minutes in the past — exceeds MaxTimestampDrift of 5m.
	ts := time.Now().Add(-10 * time.Minute).Unix()
	sig := service.SignRequest(mint.SigningKey, "GET", "/health", ts, nil)
	_, err = svc.ValidateSignedRequest(ctx, service.SignedRequest{
		APIKey:       mint.KeyID,
		Signature:    sig,
		TimestampStr: strconv.FormatInt(ts, 10),
		Method:       "GET",
		Path:         "/health",
	})
	assert.True(t, errors.Is(err, domain.ErrSessionInvalidated))
}

func TestSessionService_ValidateSignedRequest_missingHeadersIsNonDomainError(t *testing.T) {
	svc, _, _, _ := newSessionStack(t)

	_, err := svc.ValidateSignedRequest(context.Background(), service.SignedRequest{})
	require.Error(t, err)
	assert.False(t, errors.Is(err, domain.ErrSessionInvalidated),
		"missing headers is a client-side validation error, not session-invalidated")
	assert.True(t, strings.Contains(err.Error(), "missing"))
}

func TestSessionService_ValidateSignedRequest_slidesExpiry(t *testing.T) {
	svc, repo, _, userID := newSessionStack(t)
	ctx := context.Background()

	mint, err := svc.Mint(ctx, service.MintInput{UserID: userID})
	require.NoError(t, err)
	priorExpiry := mint.Session.ExpiresAt

	// Sleep one second so the sliding-expiry update is observable beyond
	// timestamp resolution.
	time.Sleep(time.Second)

	ts := time.Now().Unix()
	sig := service.SignRequest(mint.SigningKey, "GET", "/health", ts, nil)
	_, err = svc.ValidateSignedRequest(ctx, service.SignedRequest{
		APIKey:       mint.KeyID,
		Signature:    sig,
		TimestampStr: strconv.FormatInt(ts, 10),
		Method:       "GET",
		Path:         "/health",
	})
	require.NoError(t, err)

	got, err := repo.FindByKeyID(ctx, mint.KeyID)
	require.NoError(t, err)
	assert.True(t, got.ExpiresAt.After(priorExpiry),
		"a successful ValidateSignedRequest must slide expires_at forward")
}

func TestSessionService_Logout(t *testing.T) {
	svc, repo, _, userID := newSessionStack(t)
	ctx := context.Background()

	mint, err := svc.Mint(ctx, service.MintInput{UserID: userID})
	require.NoError(t, err)
	require.NoError(t, svc.Logout(ctx, mint.Session.ID))

	got, err := repo.FindByKeyID(ctx, mint.KeyID)
	require.NoError(t, err)
	assert.False(t, got.Valid())
	require.NotNil(t, got.RevokedReason)
	assert.Equal(t, domain.SessionRevokeReasonSelfLogout, *got.RevokedReason)
}

func TestSessionService_RevokeAllForUser(t *testing.T) {
	svc, _, _, userID := newSessionStack(t)
	ctx := context.Background()

	for range 3 {
		_, err := svc.Mint(ctx, service.MintInput{UserID: userID})
		require.NoError(t, err)
	}

	n, err := svc.RevokeAllForUser(ctx, userID, domain.SessionRevokeReasonAdminForceLogout)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
}

// SignRequest is exposed for round-trip use by the handler middleware tests
// that land in the next chunk; this assertion locks the canonical-string
// layout against accidental drift.
func TestSignRequest_canonicalStringIsStable(t *testing.T) {
	body := []byte(`{"k":"v"}`)
	sig1 := service.SignRequest("test-key", "POST", "/x", 1700000000, body)
	sig2 := service.SignRequest("test-key", "POST", "/x", 1700000000, body)
	assert.Equal(t, sig1, sig2)
	assert.NotEqual(t, sig1, service.SignRequest("test-key", "POST", "/x", 1700000001, body))
	assert.NotEqual(t, sig1, service.SignRequest("test-key", "GET", "/x", 1700000000, body))
}
