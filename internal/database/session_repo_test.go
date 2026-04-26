package database_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

// shortKeyID generates a unique <=32-char string suitable for the
// sessions.hmac_key_id column (varchar(32) per data-model §1.13).
// 16 bytes base64-RawURL → 22 characters.
func shortKeyID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(b)
}

func makeSession(t *testing.T, td *testutil.TestDatabase, userID uuid.UUID, expiresIn time.Duration) (*domain.Session, *database.SessionRepo) {
	t.Helper()
	repo := database.NewSessionRepo(td.Pool)
	s, err := repo.Create(context.Background(), domain.SessionCreate{
		UserID:         userID,
		HMACKeyID:      shortKeyID(t),
		HMACSecretHash: strings.Repeat("a", 64),
		ExpiresAt:      time.Now().Add(expiresIn),
	})
	require.NoError(t, err)
	return s, repo
}

func TestSessionRepo_CreateAndFindByKeyID(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "alice@example.org")
	repo := database.NewSessionRepo(td.Pool)

	created, err := repo.Create(ctx, domain.SessionCreate{
		UserID:         userID,
		HMACKeyID:      "test-key-id-001",
		HMACSecretHash: strings.Repeat("a", 64),
		ExpiresAt:      time.Now().Add(30 * 24 * time.Hour),
		ClientIP:       ptr("203.0.113.7"),
		UserAgent:      ptr("Mozilla/5.0 (Test)"),
	})
	require.NoError(t, err)
	assert.True(t, created.Valid())
	assert.Equal(t, userID, created.UserID)

	got, err := repo.FindByKeyID(ctx, "test-key-id-001")
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, created.HMACSecretHash, got.HMACSecretHash)
	require.NotNil(t, got.ClientIP)
	assert.Equal(t, "203.0.113.7", *got.ClientIP)
	require.NotNil(t, got.UserAgent)
	assert.Equal(t, "Mozilla/5.0 (Test)", *got.UserAgent)
}

func TestSessionRepo_FindByKeyID_notFound(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	_, err := database.NewSessionRepo(td.Pool).FindByKeyID(context.Background(), "missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestSessionRepo_Touch_slidesExpiry(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "bob@example.org")
	created, repo := makeSession(t, td, userID, time.Hour)

	priorExpiry := created.ExpiresAt
	require.NoError(t, repo.Touch(ctx, created.ID, 30*24*time.Hour))

	got, err := repo.FindByKeyID(ctx, created.HMACKeyID)
	require.NoError(t, err)
	assert.True(t, got.ExpiresAt.After(priorExpiry), "Touch must extend expires_at")
	assert.True(t, got.LastUsedAt.After(created.LastUsedAt) || got.LastUsedAt.Equal(created.LastUsedAt))
}

func TestSessionRepo_Touch_isNoopOnRevoked(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "carol@example.org")
	created, repo := makeSession(t, td, userID, time.Hour)
	require.NoError(t, repo.Revoke(ctx, created.ID, domain.SessionRevokeReasonSelfLogout))

	priorExpiry := created.ExpiresAt
	require.NoError(t, repo.Touch(ctx, created.ID, 24*time.Hour))

	got, err := repo.FindByKeyID(ctx, created.HMACKeyID)
	require.NoError(t, err)
	assert.Equal(t, priorExpiry.Truncate(time.Microsecond), got.ExpiresAt.Truncate(time.Microsecond),
		"Touch must NOT extend expiry on a revoked session")
}

func TestSessionRepo_Revoke_preservesFirstReason(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "dave@example.org")
	created, repo := makeSession(t, td, userID, time.Hour)

	require.NoError(t, repo.Revoke(ctx, created.ID, domain.SessionRevokeReasonRoleChanged))
	require.NoError(t, repo.Revoke(ctx, created.ID, domain.SessionRevokeReasonSelfLogout))

	got, err := repo.FindByKeyID(ctx, created.HMACKeyID)
	require.NoError(t, err)
	require.NotNil(t, got.RevokedReason)
	assert.Equal(t, domain.SessionRevokeReasonRoleChanged, *got.RevokedReason,
		"second Revoke must NOT overwrite the first reason")
}

func TestSessionRepo_Revoke_notFound(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	err := database.NewSessionRepo(td.Pool).Revoke(context.Background(), uuid.New(), "x")
	require.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestSessionRepo_RevokeAllForUser(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "eve@example.org")
	otherUserID := makeUser(t, td, "frank@example.org")
	repo := database.NewSessionRepo(td.Pool)

	// Three active sessions for eve, one for frank.
	for range 3 {
		_, err := repo.Create(ctx, domain.SessionCreate{
			UserID:         userID,
			HMACKeyID:      shortKeyID(t),
			HMACSecretHash: strings.Repeat("a", 64),
			ExpiresAt:      time.Now().Add(time.Hour),
		})
		require.NoError(t, err)
	}
	otherSession, err := repo.Create(ctx, domain.SessionCreate{
		UserID:         otherUserID,
		HMACKeyID:      shortKeyID(t),
		HMACSecretHash: strings.Repeat("a", 64),
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	n, err := repo.RevokeAllForUser(ctx, userID, domain.SessionRevokeReasonAdminForceLogout)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n, "expected exactly three sessions revoked")

	// Re-running is a no-op (no rows still active).
	n, err = repo.RevokeAllForUser(ctx, userID, domain.SessionRevokeReasonAdminForceLogout)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)

	// Frank's session is untouched.
	got, err := repo.FindByKeyID(ctx, otherSession.HMACKeyID)
	require.NoError(t, err)
	assert.True(t, got.Valid())
}

func TestSessionRepo_EvictExpired(t *testing.T) {
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	ctx := context.Background()
	userID := makeUser(t, td, "grace@example.org")
	repo := database.NewSessionRepo(td.Pool)

	// Manufacture one already-expired row by setting expires_at to the past.
	oldKey := shortKeyID(t)
	_, err := repo.Create(ctx, domain.SessionCreate{
		UserID:         userID,
		HMACKeyID:      oldKey,
		HMACSecretHash: strings.Repeat("a", 64),
		ExpiresAt:      time.Now().Add(-30 * 24 * time.Hour), // 30 days expired
	})
	require.NoError(t, err)

	// And one healthy row.
	newKey := shortKeyID(t)
	_, err = repo.Create(ctx, domain.SessionCreate{
		UserID:         userID,
		HMACKeyID:      newKey,
		HMACSecretHash: strings.Repeat("a", 64),
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	// Retain anything that expired within the past 7 days; the 30-day-old
	// row is outside the retention window and should be evicted.
	n, err := repo.EvictExpired(ctx, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	// Healthy row remains.
	_, err = repo.FindByKeyID(ctx, newKey)
	assert.NoError(t, err)

	// Old row is gone.
	_, err = repo.FindByKeyID(ctx, oldKey)
	assert.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestSession_ValidInvariant(t *testing.T) {
	now := time.Now()

	live := &domain.Session{ExpiresAt: now.Add(time.Hour)}
	expired := &domain.Session{ExpiresAt: now.Add(-time.Hour)}
	revokedAt := now.Add(-time.Minute)
	revoked := &domain.Session{ExpiresAt: now.Add(time.Hour), RevokedAt: &revokedAt}

	assert.True(t, live.Valid())
	assert.False(t, expired.Valid())
	assert.False(t, revoked.Valid())
}
