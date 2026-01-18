package handler_test

import (
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

func TestStreamHandler_ListStreams_ReturnsActiveStreams(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster and stream key
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")
	keyValue := createTestStreamKey(t, db.Pool, broadcasterID, "active", nil)

	// Get the stream key ID
	var keyID uuid.UUID
	err := db.Pool.QueryRow(context.Background(), "SELECT id FROM stream_keys WHERE key_value = $1", keyValue).Scan(&keyID)
	require.NoError(t, err)

	// Create active stream
	createTestStream(t, db.Pool, keyID, keyValue, "active")

	// Setup handler
	h := setupStreamHandler(t, db.Pool)

	// Execute
	req := httptest.NewRequest(http.MethodGet, "/streams", nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.StreamListResponse
	err = json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, 1, resp.Count)
	assert.Len(t, resp.Streams, 1)
	assert.Equal(t, keyValue, resp.Streams[0].Path)
	assert.NotEmpty(t, resp.Streams[0].URLs.WebRTC)
}

func TestStreamHandler_ListStreams_EmptyWhenNoStreams(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler
	h := setupStreamHandler(t, db.Pool)

	// Execute
	req := httptest.NewRequest(http.MethodGet, "/streams", nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.StreamListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, 0, resp.Count)
	assert.Empty(t, resp.Streams)
}

func TestStreamHandler_ListStreams_ExcludesInactiveStreams(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster and stream key
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")
	keyValue := createTestStreamKey(t, db.Pool, broadcasterID, "active", nil)

	// Get the stream key ID
	var keyID uuid.UUID
	err := db.Pool.QueryRow(context.Background(), "SELECT id FROM stream_keys WHERE key_value = $1", keyValue).Scan(&keyID)
	require.NoError(t, err)

	// Create ended stream
	createTestStream(t, db.Pool, keyID, keyValue, "ended")

	// Setup handler
	h := setupStreamHandler(t, db.Pool)

	// Execute
	req := httptest.NewRequest(http.MethodGet, "/streams", nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.StreamListResponse
	err = json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, 0, resp.Count, "ended streams should not be included")
}

func TestStreamHandler_GetStream_ReturnsDetails(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster and stream key
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")
	keyValue := createTestStreamKey(t, db.Pool, broadcasterID, "active", nil)

	// Get the stream key ID
	var keyID uuid.UUID
	err := db.Pool.QueryRow(context.Background(), "SELECT id FROM stream_keys WHERE key_value = $1", keyValue).Scan(&keyID)
	require.NoError(t, err)

	// Create active stream
	streamID := createTestStream(t, db.Pool, keyID, keyValue, "active")

	// Setup handler with mux router
	h := setupStreamHandler(t, db.Pool)

	router := mux.NewRouter()
	router.Handle("/streams/{id}", h)

	// Execute
	req := httptest.NewRequest(http.MethodGet, "/streams/"+streamID.String(), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp domain.StreamWithURLs
	err = json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, streamID, resp.ID)
	assert.Equal(t, keyValue, resp.Path)
	assert.NotEmpty(t, resp.URLs.WebRTC)
}

func TestStreamHandler_GetStream_NotFound(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler with mux router
	h := setupStreamHandler(t, db.Pool)

	router := mux.NewRouter()
	router.Handle("/streams/{id}", h)

	// Execute with non-existent ID
	nonExistentID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/streams/"+nonExistentID.String(), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestStreamHandler_GetStream_InvalidID(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler with mux router
	h := setupStreamHandler(t, db.Pool)

	router := mux.NewRouter()
	router.Handle("/streams/{id}", h)

	// Execute with invalid ID
	req := httptest.NewRequest(http.MethodGet, "/streams/not-a-uuid", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func setupStreamHandler(t *testing.T, pool *pgxpool.Pool) *handler.StreamHandler {
	t.Helper()

	streamRepo := database.NewStreamRepo(pool)
	mediaMTXClient, err := service.NewMediaMTXClient("http://localhost:9997", "http://localhost:8889")
	require.NoError(t, err)
	streamService := service.NewStreamService(streamRepo, mediaMTXClient)

	return handler.NewStreamHandler(streamService, nil)
}
