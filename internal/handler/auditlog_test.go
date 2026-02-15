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
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/handler"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

// ============================================================
// Tests for User Story 1 (T021, T022, T023)
// ============================================================

func TestAuditLogHandler_ListAuditLogs(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create test audit entries
	createTestAuditLog(t, db.Pool, "test-api-key", "create", "broadcaster")
	createTestAuditLog(t, db.Pool, "test-api-key", "update", "stream")
	createTestAuditLog(t, db.Pool, "test-api-key", "delete", "stream_key")

	// Setup handler with admin permissions
	h := setupAuditLogHandler(t, db.Pool)

	// Execute
	req := httptest.NewRequest(http.MethodGet, "/audit-logs", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.AuditLogListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Len(t, resp.AuditLogs, 3)
	assert.Equal(t, int64(3), resp.Pagination.Total)
	assert.Equal(t, 50, resp.Pagination.Limit)
	assert.Equal(t, 0, resp.Pagination.Offset)
}

func TestAuditLogHandler_ListAuditLogs_EmptyResult(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler with admin permissions (no audit entries created)
	h := setupAuditLogHandler(t, db.Pool)

	// Execute
	req := httptest.NewRequest(http.MethodGet, "/audit-logs", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.AuditLogListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Empty(t, resp.AuditLogs)
	assert.Equal(t, int64(0), resp.Pagination.Total)
}

// ============================================================
// Tests for User Story 5 (T032, T033)
// ============================================================

func TestAuditLogHandler_CreateAuditEvent(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler (any auth, not admin-only)
	h := setupAuditLogHandler(t, db.Pool)

	// Create request
	reqBody := handler.CreateAuditEventRequest{
		EventType: "login",
		Metadata:  map[string]interface{}{"browser": "Chrome"},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// Execute
	req := httptest.NewRequest(http.MethodPost, "/audit-events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = addAPIKeyToContext(req, "regular-api-key")
	req = addRequestIDToContext(req, "test-request-id")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusCreated, recorder.Code)

	var resp domain.AuditLogEntry
	err = json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.NotEqual(t, uuid.Nil, resp.ID)
	assert.Equal(t, "login", resp.Action)
	assert.Equal(t, "regular-api-key", resp.Actor)
	assert.Equal(t, "success", resp.Outcome)
}

func TestAuditLogHandler_CreateAuditEvent_AnyAuthAccess(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Setup handler - even non-admin can submit events
	h := setupAuditLogHandler(t, db.Pool)

	// Create request
	reqBody := handler.CreateAuditEventRequest{
		EventType: "logout",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// Execute with non-admin key
	req := httptest.NewRequest(http.MethodPost, "/audit-events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = addAPIKeyToContext(req, "non-admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert - should succeed even for non-admin
	assert.Equal(t, http.StatusCreated, recorder.Code)
}

func TestAuditLogHandler_CreateAuditEvent_MissingEventType(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	h := setupAuditLogHandler(t, db.Pool)

	// Create request without event_type
	reqBody := handler.CreateAuditEventRequest{}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/audit-events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = addAPIKeyToContext(req, "api-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	var errResp map[string]interface{}
	err = json.NewDecoder(recorder.Body).Decode(&errResp)
	require.NoError(t, err)

	assert.Equal(t, "event_type is required", errResp["detail"])
}

func TestAuditLogHandler_CreateAuditEvent_EventTypeTooLong(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	h := setupAuditLogHandler(t, db.Pool)

	// Create request with event_type > 50 chars
	reqBody := handler.CreateAuditEventRequest{
		EventType: "this_is_a_very_long_event_type_that_exceeds_fifty_characters_limit",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/audit-events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = addAPIKeyToContext(req, "api-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	// Assert
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	var errResp map[string]interface{}
	err = json.NewDecoder(recorder.Body).Decode(&errResp)
	require.NoError(t, err)

	assert.Equal(t, "event_type must be 50 characters or less", errResp["detail"])
}

// ============================================================
// Tests for User Story 3 - Filtering (T039)
// ============================================================

func TestAuditLogHandler_ListAuditLogs_FilterByActor(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create entries with different actors
	createTestAuditLog(t, db.Pool, "actor-1", "create", "broadcaster")
	createTestAuditLog(t, db.Pool, "actor-2", "update", "broadcaster")
	createTestAuditLog(t, db.Pool, "actor-1", "delete", "stream")

	h := setupAuditLogHandler(t, db.Pool)

	// Filter by actor-1
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?actor=actor-1", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.AuditLogListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Len(t, resp.AuditLogs, 2)
	for _, entry := range resp.AuditLogs {
		assert.Equal(t, "actor-1", entry.Actor)
	}
}

func TestAuditLogHandler_ListAuditLogs_FilterByAction(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	createTestAuditLog(t, db.Pool, "test-actor", "create", "broadcaster")
	createTestAuditLog(t, db.Pool, "test-actor", "update", "broadcaster")
	createTestAuditLog(t, db.Pool, "test-actor", "create", "stream")

	h := setupAuditLogHandler(t, db.Pool)

	// Filter by action=create
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?action=create", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.AuditLogListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Len(t, resp.AuditLogs, 2)
	for _, entry := range resp.AuditLogs {
		assert.Equal(t, "create", entry.Action)
	}
}

func TestAuditLogHandler_ListAuditLogs_FilterByResourceType(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	createTestAuditLog(t, db.Pool, "test-actor", "create", "broadcaster")
	createTestAuditLog(t, db.Pool, "test-actor", "update", "stream")
	createTestAuditLog(t, db.Pool, "test-actor", "delete", "broadcaster")

	h := setupAuditLogHandler(t, db.Pool)

	// Filter by resource_type=broadcaster
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?resource_type=broadcaster", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.AuditLogListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Len(t, resp.AuditLogs, 2)
	for _, entry := range resp.AuditLogs {
		assert.Equal(t, "broadcaster", *entry.ResourceType)
	}
}

func TestAuditLogHandler_ListAuditLogs_FilterByResourceID(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	resourceID := uuid.New()
	otherID := uuid.New()

	createTestAuditLogWithResourceID(t, db.Pool, "test-actor", "create", "broadcaster", &resourceID)
	createTestAuditLogWithResourceID(t, db.Pool, "test-actor", "update", "broadcaster", &otherID)

	h := setupAuditLogHandler(t, db.Pool)

	// Filter by resource_id
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?resource_id="+resourceID.String(), nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.AuditLogListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Len(t, resp.AuditLogs, 1)
	assert.Equal(t, resourceID, *resp.AuditLogs[0].ResourceID)
}

func TestAuditLogHandler_ListAuditLogs_FilterByDateRange(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	now := time.Now()
	past := now.Add(-48 * time.Hour)

	// Create entries at different times
	createTestAuditLogWithTimestamp(t, db.Pool, "test-actor", "create", "broadcaster", past)
	createTestAuditLogWithTimestamp(t, db.Pool, "test-actor", "update", "stream", now)

	h := setupAuditLogHandler(t, db.Pool)

	// Filter to only include recent entries
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?from="+from, nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.AuditLogListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	// Should only get the recent entry
	assert.Len(t, resp.AuditLogs, 1)
}

func TestAuditLogHandler_ListAuditLogs_InvalidResourceType(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	h := setupAuditLogHandler(t, db.Pool)

	// Filter with invalid resource_type
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?resource_type=invalid_type", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	var errResp map[string]interface{}
	err := json.NewDecoder(recorder.Body).Decode(&errResp)
	require.NoError(t, err)

	assert.Contains(t, errResp["detail"], "invalid 'resource_type'")
}

func TestAuditLogHandler_ListAuditLogs_InvalidDateFormat(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	h := setupAuditLogHandler(t, db.Pool)

	// Filter with invalid date format
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?from=not-a-date", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	var errResp map[string]interface{}
	err := json.NewDecoder(recorder.Body).Decode(&errResp)
	require.NoError(t, err)

	assert.Contains(t, errResp["detail"], "invalid 'from' date format")
}

func TestAuditLogHandler_ListAuditLogs_InvalidResourceID(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	h := setupAuditLogHandler(t, db.Pool)

	// Filter with invalid UUID
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?resource_id=not-a-uuid", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	var errResp map[string]interface{}
	err := json.NewDecoder(recorder.Body).Decode(&errResp)
	require.NoError(t, err)

	assert.Contains(t, errResp["detail"], "invalid 'resource_id' format")
}

// ============================================================
// Tests for User Story 4 - Pagination (T046, T047)
// ============================================================

func TestAuditLogHandler_ListAuditLogs_Pagination(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create 10 audit entries
	for i := 0; i < 10; i++ {
		createTestAuditLog(t, db.Pool, "test-actor", "create", "broadcaster")
	}

	h := setupAuditLogHandler(t, db.Pool)

	// Request with limit=5
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?limit=5", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.AuditLogListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Len(t, resp.AuditLogs, 5)
	assert.Equal(t, int64(10), resp.Pagination.Total)
	assert.Equal(t, 5, resp.Pagination.Limit)
	assert.Equal(t, 0, resp.Pagination.Offset)
}

func TestAuditLogHandler_ListAuditLogs_PaginationWithOffset(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create 10 audit entries
	for i := 0; i < 10; i++ {
		createTestAuditLog(t, db.Pool, "test-actor", "create", "broadcaster")
	}

	h := setupAuditLogHandler(t, db.Pool)

	// Request with limit=5 and offset=5
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?limit=5&offset=5", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.AuditLogListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Len(t, resp.AuditLogs, 5)
	assert.Equal(t, int64(10), resp.Pagination.Total)
	assert.Equal(t, 5, resp.Pagination.Limit)
	assert.Equal(t, 5, resp.Pagination.Offset)
}

func TestAuditLogHandler_ListAuditLogs_LimitExceedsMax(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	h := setupAuditLogHandler(t, db.Pool)

	// Request with limit > 100 should be rejected
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?limit=200", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	var errResp map[string]interface{}
	err := json.NewDecoder(recorder.Body).Decode(&errResp)
	require.NoError(t, err)

	assert.Contains(t, errResp["detail"], "limit must be between 1 and 100")
}

func TestAuditLogHandler_ListAuditLogs_InvalidLimit(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	h := setupAuditLogHandler(t, db.Pool)

	// Request with limit=0
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?limit=0", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestAuditLogHandler_ListAuditLogs_NegativeOffset(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	h := setupAuditLogHandler(t, db.Pool)

	// Request with negative offset
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?offset=-1", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	var errResp map[string]interface{}
	err := json.NewDecoder(recorder.Body).Decode(&errResp)
	require.NoError(t, err)

	assert.Contains(t, errResp["detail"], "offset must be a non-negative integer")
}

func TestAuditLogHandler_ListAuditLogs_LargeOffset(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	// Create only 5 entries
	for i := 0; i < 5; i++ {
		createTestAuditLog(t, db.Pool, "test-actor", "create", "broadcaster")
	}

	h := setupAuditLogHandler(t, db.Pool)

	// Request with offset beyond total
	req := httptest.NewRequest(http.MethodGet, "/audit-logs?offset=100", nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)

	var resp handler.AuditLogListResponse
	err := json.NewDecoder(recorder.Body).Decode(&resp)
	require.NoError(t, err)

	// Should return empty array but correct total
	assert.Empty(t, resp.AuditLogs)
	assert.Equal(t, int64(5), resp.Pagination.Total)
}

// ============================================================
// Helper Functions
// ============================================================

func setupAuditLogHandler(t *testing.T, pool *pgxpool.Pool) *handler.AuditLogHandler {
	t.Helper()

	auditRepo := database.NewAuditLogRepo(pool)
	auditService := service.NewAuditLogService(auditRepo)

	return handler.NewAuditLogHandler(auditService, nil)
}

func addAPIKeyToContext(r *http.Request, apiKey string) *http.Request {
	ctx := handler.ContextWithAPIKey(r.Context(), apiKey)
	return r.WithContext(ctx)
}

func addRequestIDToContext(r *http.Request, requestID string) *http.Request {
	ctx := handler.ContextWithRequestID(r.Context(), requestID)
	return r.WithContext(ctx)
}

func createTestAuditLog(t *testing.T, pool *pgxpool.Pool, actor, action, resourceType string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO audit_logs (id, timestamp, actor, action, resource_type, request_method, request_path, ip_address, outcome, metadata)
		 VALUES ($1, NOW(), $2, $3, $4, 'POST', '/test', '127.0.0.1', 'success', '{}')`,
		id, actor, action, resourceType)
	require.NoError(t, err)

	return id
}

func createTestAuditLogWithResourceID(t *testing.T, pool *pgxpool.Pool, actor, action, resourceType string, resourceID *uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO audit_logs (id, timestamp, actor, action, resource_type, resource_id, request_method, request_path, ip_address, outcome, metadata)
		 VALUES ($1, NOW(), $2, $3, $4, $5, 'POST', '/test', '127.0.0.1', 'success', '{}')`,
		id, actor, action, resourceType, resourceID)
	require.NoError(t, err)

	return id
}

func createTestAuditLogWithTimestamp(t *testing.T, pool *pgxpool.Pool, actor, action, resourceType string, timestamp time.Time) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO audit_logs (id, timestamp, actor, action, resource_type, request_method, request_path, ip_address, outcome, metadata)
		 VALUES ($1, $2, $3, $4, $5, 'POST', '/test', '127.0.0.1', 'success', '{}')`,
		id, timestamp, actor, action, resourceType)
	require.NoError(t, err)

	return id
}
