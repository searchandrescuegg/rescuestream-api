package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/handler"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

func TestStreamKeyHandler_Create_ReturnsKeyValue(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")

	// Setup handler
	h := setupStreamKeyHandler(t, db.Pool)

	// Create request
	reqBody := handler.CreateStreamKeyRequest{
		BroadcasterID: broadcasterID.String(),
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// Execute
	req := httptest.NewRequest(http.MethodPost, "/stream-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusCreated, recorder.Code)

	var resp domain.StreamKey
	err = json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.NotEmpty(t, resp.KeyValue, "key_value should be present in create response")
	assert.True(t, len(resp.KeyValue) > 20, "key_value should be sufficiently long")
	assert.Equal(t, domain.StreamKeyStatusActive, resp.Status)
}

func TestStreamKeyHandler_List_OmitsKeyValue(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster and stream key
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")
	createTestStreamKey(t, db.Pool, broadcasterID, "active", nil)

	// Setup handler
	h := setupStreamKeyHandler(t, db.Pool)

	// Execute
	req := httptest.NewRequest(http.MethodGet, "/stream-keys", nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.StreamKeyListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, 1, resp.Count)
	assert.Len(t, resp.StreamKeys, 1)
	assert.Empty(t, resp.StreamKeys[0].KeyValue, "key_value should be omitted in list response")
}

func TestStreamKeyHandler_GetByID_Works(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster and stream key
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")
	keyValue := createTestStreamKey(t, db.Pool, broadcasterID, "active", nil)

	// Get the stream key ID
	var keyID uuid.UUID
	err := db.Pool.QueryRow(context.Background(), "SELECT id FROM stream_keys WHERE key_value = $1", keyValue).Scan(&keyID)
	require.NoError(t, err)

	// Setup handler with mux router
	h := setupStreamKeyHandler(t, db.Pool)

	router := mux.NewRouter()
	router.Handle("/stream-keys/{id}", h)

	// Execute
	req := httptest.NewRequest(http.MethodGet, "/stream-keys/"+keyID.String(), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp domain.StreamKey
	err = json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, keyID, resp.ID)
	assert.Empty(t, resp.KeyValue, "key_value should be cleared for security")
}

func TestStreamKeyHandler_Revoke_InvalidatesKey(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster and stream key
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")
	keyValue := createTestStreamKey(t, db.Pool, broadcasterID, "active", nil)

	// Get the stream key ID
	var keyID uuid.UUID
	err := db.Pool.QueryRow(context.Background(), "SELECT id FROM stream_keys WHERE key_value = $1", keyValue).Scan(&keyID)
	require.NoError(t, err)

	// Setup handlers
	streamKeyHandler := setupStreamKeyHandler(t, db.Pool)
	authHandler := setupAuthHandler(t, db.Pool)

	router := mux.NewRouter()
	router.Handle("/stream-keys/{id}", streamKeyHandler)

	// Revoke the key
	req := httptest.NewRequest(http.MethodDelete, "/stream-keys/"+keyID.String(), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	// Assert revoke succeeded
	assert.Equal(t, http.StatusNoContent, recorder.Code)

	// Verify the key is now revoked in the database
	var status string
	err = db.Pool.QueryRow(context.Background(), "SELECT status FROM stream_keys WHERE id = $1", keyID).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "revoked", status)

	// Try to authenticate with the revoked key
	authReq := service.AuthRequest{
		User:     "",
		Password: keyValue,
		IP:       "192.168.1.1",
		Action:   "publish",
		Path:     keyValue,
		Protocol: "rtmp",
		ID:       "conn-123",
		Query:    "",
	}
	authResp := executeAuthRequest(t, authHandler, authReq)
	assert.Equal(t, http.StatusUnauthorized, authResp.Code, "revoked key should fail auth")
}

func TestStreamKeyHandler_Revoke_NotFound(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler with mux router
	h := setupStreamKeyHandler(t, db.Pool)

	router := mux.NewRouter()
	router.Handle("/stream-keys/{id}", h)

	// Execute with non-existent ID
	nonExistentID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/stream-keys/"+nonExistentID.String(), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestStreamKeyHandler_Create_MissingBroadcasterID(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler
	h := setupStreamKeyHandler(t, db.Pool)

	// Create request without broadcaster_id
	reqBody := handler.CreateStreamKeyRequest{}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// Execute
	req := httptest.NewRequest(http.MethodPost, "/stream-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestStreamKeyHandler_Create_InvalidBroadcasterID(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler
	h := setupStreamKeyHandler(t, db.Pool)

	// Create request with non-existent broadcaster
	reqBody := handler.CreateStreamKeyRequest{
		BroadcasterID: uuid.New().String(),
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// Execute
	req := httptest.NewRequest(http.MethodPost, "/stream-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert - returns 500 because FK constraint violation is not mapped to ErrNotFound
	// Future improvement: service could validate broadcaster exists first
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func setupStreamKeyHandler(t *testing.T, pool *pgxpool.Pool) *handler.StreamKeyHandler {
	t.Helper()

	streamKeyRepo := database.NewStreamKeyRepo(pool)
	streamRepo := database.NewStreamRepo(pool)
	streamKeyService := service.NewStreamKeyService(
		streamKeyRepo,
		streamRepo,
		nil, // mediamtx client not needed for these tests
	)

	return handler.NewStreamKeyHandler(streamKeyService, nil)
}
