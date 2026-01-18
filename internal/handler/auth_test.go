package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/handler"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

func TestAuthHandler_ValidKey(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster and stream key
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")
	keyValue := createTestStreamKey(t, db.Pool, broadcasterID, "active", nil)

	// Setup handler
	h := setupAuthHandler(t, db.Pool)

	// Create auth request
	req := service.AuthRequest{
		User:     "",
		Password: keyValue,
		IP:       "192.168.1.1",
		Action:   "publish",
		Path:     keyValue,
		Protocol: "rtmp",
		ID:       "conn-123",
		Query:    "",
	}

	// Execute
	resp := executeAuthRequest(t, h, req)

	// Assert
	assert.Equal(t, http.StatusOK, resp.Code, "valid key should return 200")
}

func TestAuthHandler_InvalidKey(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler
	h := setupAuthHandler(t, db.Pool)

	// Create auth request with invalid key
	req := service.AuthRequest{
		User:     "",
		Password: "invalid-key-value",
		IP:       "192.168.1.1",
		Action:   "publish",
		Path:     "invalid-key-value",
		Protocol: "rtmp",
		ID:       "conn-123",
		Query:    "",
	}

	// Execute
	resp := executeAuthRequest(t, h, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, resp.Code, "invalid key should return 401")
}

func TestAuthHandler_RevokedKey(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster and revoked stream key
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")
	keyValue := createTestStreamKey(t, db.Pool, broadcasterID, "revoked", nil)

	// Setup handler
	h := setupAuthHandler(t, db.Pool)

	// Create auth request
	req := service.AuthRequest{
		User:     "",
		Password: keyValue,
		IP:       "192.168.1.1",
		Action:   "publish",
		Path:     keyValue,
		Protocol: "rtmp",
		ID:       "conn-123",
		Query:    "",
	}

	// Execute
	resp := executeAuthRequest(t, h, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, resp.Code, "revoked key should return 401")
}

func TestAuthHandler_ExpiredKey(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster and expired stream key
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")
	expiredAt := time.Now().Add(-1 * time.Hour)
	keyValue := createTestStreamKey(t, db.Pool, broadcasterID, "active", &expiredAt)

	// Setup handler
	h := setupAuthHandler(t, db.Pool)

	// Create auth request
	req := service.AuthRequest{
		User:     "",
		Password: keyValue,
		IP:       "192.168.1.1",
		Action:   "publish",
		Path:     keyValue,
		Protocol: "rtmp",
		ID:       "conn-123",
		Query:    "",
	}

	// Execute
	resp := executeAuthRequest(t, h, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, resp.Code, "expired key should return 401")
}

func TestAuthHandler_KeyInUse(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster and stream key
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")
	keyValue := createTestStreamKey(t, db.Pool, broadcasterID, "active", nil)

	// Get the stream key ID
	var keyID uuid.UUID
	err := db.Pool.QueryRow(context.Background(), "SELECT id FROM stream_keys WHERE key_value = $1", keyValue).Scan(&keyID)
	require.NoError(t, err)

	// Create active stream using this key
	createTestStream(t, db.Pool, keyID, keyValue, "active")

	// Setup handler
	h := setupAuthHandler(t, db.Pool)

	// Create auth request
	req := service.AuthRequest{
		User:     "",
		Password: keyValue,
		IP:       "192.168.1.1",
		Action:   "publish",
		Path:     keyValue,
		Protocol: "rtmp",
		ID:       "conn-456",
		Query:    "",
	}

	// Execute
	resp := executeAuthRequest(t, h, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, resp.Code, "key in use should return 401")
}

func TestAuthHandler_ReadActionAllowed(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler
	h := setupAuthHandler(t, db.Pool)

	// Create auth request with read action (no key needed)
	req := service.AuthRequest{
		User:     "",
		Password: "",
		IP:       "192.168.1.1",
		Action:   "read",
		Path:     "some-stream",
		Protocol: "webrtc",
		ID:       "conn-123",
		Query:    "",
	}

	// Execute
	resp := executeAuthRequest(t, h, req)

	// Assert
	assert.Equal(t, http.StatusOK, resp.Code, "read action should be allowed without auth")
}

func TestAuthHandler_MissingPassword(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler
	h := setupAuthHandler(t, db.Pool)

	// Create auth request with missing password
	req := service.AuthRequest{
		User:     "",
		Password: "",
		IP:       "192.168.1.1",
		Action:   "publish",
		Path:     "some-path",
		Protocol: "rtmp",
		ID:       "conn-123",
		Query:    "",
	}

	// Execute
	resp := executeAuthRequest(t, h, req)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, resp.Code, "missing password should return 401")
}

// Helper functions

func setupAuthHandler(t *testing.T, pool *pgxpool.Pool) *handler.AuthHandler {
	t.Helper()

	streamKeyRepo := database.NewStreamKeyRepo(pool)
	streamRepo := database.NewStreamRepo(pool)
	authService := service.NewAuthService(pool, streamKeyRepo, streamRepo)

	return handler.NewAuthHandler(authService, nil)
}

func executeAuthRequest(t *testing.T, h *handler.AuthHandler, req service.AuthRequest) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(req)
	require.NoError(t, err)

	httpReq := httptest.NewRequest(http.MethodPost, "/auth", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httpReq)

	return recorder
}

func createTestBroadcaster(t *testing.T, pool *pgxpool.Pool, displayName string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		"INSERT INTO broadcasters (id, display_name, metadata, created_at, updated_at) VALUES ($1, $2, $3, NOW(), NOW())",
		id, displayName, "{}")
	require.NoError(t, err)

	return id
}

func createTestStreamKey(t *testing.T, pool *pgxpool.Pool, broadcasterID uuid.UUID, status string, expiresAt *time.Time) string {
	t.Helper()

	id := uuid.New()
	keyValue := "sk_test_" + id.String()[:8]

	_, err := pool.Exec(context.Background(),
		"INSERT INTO stream_keys (id, key_value, broadcaster_id, status, created_at, expires_at) VALUES ($1, $2, $3, $4, NOW(), $5)",
		id, keyValue, broadcasterID, status, expiresAt)
	require.NoError(t, err)

	return keyValue
}

func createTestStream(t *testing.T, pool *pgxpool.Pool, keyID uuid.UUID, path string, status string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		"INSERT INTO streams (id, stream_key_id, path, status, started_at) VALUES ($1, $2, $3, $4, NOW())",
		id, keyID, path, status)
	require.NoError(t, err)

	return id
}
