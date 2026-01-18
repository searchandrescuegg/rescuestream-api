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

func TestBroadcasterHandler_Create(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler
	h := setupBroadcasterHandler(t, db.Pool)

	// Create request
	reqBody := handler.CreateBroadcasterRequest{
		DisplayName: "Test Broadcaster",
		Metadata:    map[string]interface{}{"key": "value"},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// Execute
	req := httptest.NewRequest(http.MethodPost, "/broadcasters", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusCreated, recorder.Code)

	var resp domain.Broadcaster
	err = json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.NotEqual(t, uuid.Nil, resp.ID)
	assert.Equal(t, "Test Broadcaster", resp.DisplayName)
}

func TestBroadcasterHandler_Create_MissingDisplayName(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler
	h := setupBroadcasterHandler(t, db.Pool)

	// Create request without display_name
	reqBody := handler.CreateBroadcasterRequest{}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// Execute
	req := httptest.NewRequest(http.MethodPost, "/broadcasters", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestBroadcasterHandler_List(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcasters
	createTestBroadcaster(t, db.Pool, "Broadcaster 1")
	createTestBroadcaster(t, db.Pool, "Broadcaster 2")

	// Setup handler
	h := setupBroadcasterHandler(t, db.Pool)

	// Execute
	req := httptest.NewRequest(http.MethodGet, "/broadcasters", nil)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.BroadcasterListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, 2, resp.Count)
	assert.Len(t, resp.Broadcasters, 2)
}

func TestBroadcasterHandler_GetByID(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster
	broadcasterID := createTestBroadcaster(t, db.Pool, "Test Broadcaster")

	// Setup handler with mux router
	h := setupBroadcasterHandler(t, db.Pool)

	router := mux.NewRouter()
	router.Handle("/broadcasters/{id}", h)

	// Execute
	req := httptest.NewRequest(http.MethodGet, "/broadcasters/"+broadcasterID.String(), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp domain.Broadcaster
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, broadcasterID, resp.ID)
	assert.Equal(t, "Test Broadcaster", resp.DisplayName)
}

func TestBroadcasterHandler_GetByID_NotFound(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler with mux router
	h := setupBroadcasterHandler(t, db.Pool)

	router := mux.NewRouter()
	router.Handle("/broadcasters/{id}", h)

	// Execute with non-existent ID
	nonExistentID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/broadcasters/"+nonExistentID.String(), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestBroadcasterHandler_Update(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster
	broadcasterID := createTestBroadcaster(t, db.Pool, "Original Name")

	// Setup handler with mux router
	h := setupBroadcasterHandler(t, db.Pool)

	router := mux.NewRouter()
	router.Handle("/broadcasters/{id}", h)

	// Create update request
	newName := "Updated Name"
	reqBody := handler.UpdateBroadcasterRequest{
		DisplayName: &newName,
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// Execute
	req := httptest.NewRequest(http.MethodPatch, "/broadcasters/"+broadcasterID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp domain.Broadcaster
	err = json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Equal(t, "Updated Name", resp.DisplayName)
}

func TestBroadcasterHandler_Delete(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create broadcaster
	broadcasterID := createTestBroadcaster(t, db.Pool, "To Be Deleted")

	// Setup handler with mux router
	h := setupBroadcasterHandler(t, db.Pool)

	router := mux.NewRouter()
	router.Handle("/broadcasters/{id}", h)

	// Execute
	req := httptest.NewRequest(http.MethodDelete, "/broadcasters/"+broadcasterID.String(), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusNoContent, recorder.Code)

	// Verify it's deleted
	var count int
	err := db.Pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM broadcasters WHERE id = $1", broadcasterID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestBroadcasterHandler_Delete_NotFound(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler with mux router
	h := setupBroadcasterHandler(t, db.Pool)

	router := mux.NewRouter()
	router.Handle("/broadcasters/{id}", h)

	// Execute with non-existent ID
	nonExistentID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/broadcasters/"+nonExistentID.String(), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func setupBroadcasterHandler(t *testing.T, pool *pgxpool.Pool) *handler.BroadcasterHandler {
	t.Helper()

	broadcasterRepo := database.NewBroadcasterRepo(pool)
	broadcasterService := service.NewBroadcasterService(broadcasterRepo)

	return handler.NewBroadcasterHandler(broadcasterService, nil)
}
