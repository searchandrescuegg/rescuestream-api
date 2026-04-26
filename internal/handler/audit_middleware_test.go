package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/handler"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

// ============================================================
// Integration Tests for Audit Middleware (T012)
// ============================================================

func TestAuditMiddleware_RecordsPostRequest(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	auditRepo := database.NewAuditLogRepo(db.Pool)
	auditService := service.NewAuditLogService(auditRepo)

	// Create test handler that returns 201
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "created"})
	})

	// Wrap with audit middleware
	middleware := handler.AuditMiddleware(auditService, nil)
	wrappedHandler := middleware(testHandler)

	// Execute POST request
	req := httptest.NewRequest(http.MethodPost, "/broadcasters", nil)
	req = addAPIKeyToContext(req, "test-api-key")
	req = addRequestIDToContext(req, "req-123")
	req.RemoteAddr = "192.168.1.1:12345"
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	// Assert response went through
	assert.Equal(t, http.StatusCreated, recorder.Code)

	// Verify audit entry was created
	time.Sleep(10 * time.Millisecond) // Small delay to ensure async write completes

	entries, total, err := auditRepo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)

	entry := entries[0]
	assert.Equal(t, "test-api-key", entry.Actor)
	assert.Equal(t, "create", entry.Action)
	assert.Equal(t, "broadcaster", *entry.ResourceType)
	assert.Equal(t, "POST", entry.RequestMethod)
	assert.Equal(t, "/broadcasters", entry.RequestPath)
	assert.Equal(t, "192.168.1.1", entry.IPAddress)
	assert.Equal(t, "success", entry.Outcome)
}

func TestAuditMiddleware_RecordsDeleteRequest(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	auditRepo := database.NewAuditLogRepo(db.Pool)
	auditService := service.NewAuditLogService(auditRepo)

	resourceID := uuid.New()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	middleware := handler.AuditMiddleware(auditService, nil)
	wrappedHandler := middleware(testHandler)

	// Execute DELETE request
	req := httptest.NewRequest(http.MethodDelete, "/broadcasters/"+resourceID.String(), nil)
	req = addAPIKeyToContext(req, "admin-key")
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusNoContent, recorder.Code)

	time.Sleep(10 * time.Millisecond)

	entries, _, err := auditRepo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "delete", entry.Action)
	assert.Equal(t, "broadcaster", *entry.ResourceType)
	assert.Equal(t, &resourceID, entry.ResourceID)
	assert.Equal(t, "success", entry.Outcome)
}

func TestAuditMiddleware_RecordsPatchRequest(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	auditRepo := database.NewAuditLogRepo(db.Pool)
	auditService := service.NewAuditLogService(auditRepo)

	resourceID := uuid.New()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := handler.AuditMiddleware(auditService, nil)
	wrappedHandler := middleware(testHandler)

	req := httptest.NewRequest(http.MethodPatch, "/broadcasters/"+resourceID.String(), nil)
	req = addAPIKeyToContext(req, "test-key")
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)

	time.Sleep(10 * time.Millisecond)

	entries, _, err := auditRepo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, "update", entries[0].Action)
}

func TestAuditMiddleware_SkipsGetRequest(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	auditRepo := database.NewAuditLogRepo(db.Pool)
	auditService := service.NewAuditLogService(auditRepo)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := handler.AuditMiddleware(auditService, nil)
	wrappedHandler := middleware(testHandler)

	// Execute GET request
	req := httptest.NewRequest(http.MethodGet, "/broadcasters", nil)
	req = addAPIKeyToContext(req, "test-key")
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)

	time.Sleep(10 * time.Millisecond)

	// Verify no audit entry was created
	_, total, err := auditRepo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
}

func TestAuditMiddleware_SkipsAuditEndpoints(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	auditRepo := database.NewAuditLogRepo(db.Pool)
	auditService := service.NewAuditLogService(auditRepo)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	middleware := handler.AuditMiddleware(auditService, nil)
	wrappedHandler := middleware(testHandler)

	// Execute POST to /audit-events (should be skipped)
	req := httptest.NewRequest(http.MethodPost, "/audit-events", nil)
	req = addAPIKeyToContext(req, "test-key")
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusCreated, recorder.Code)

	time.Sleep(10 * time.Millisecond)

	// Verify no audit entry was created (to prevent recursion)
	_, total, err := auditRepo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
}

func TestAuditMiddleware_RecordsFailure(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	auditRepo := database.NewAuditLogRepo(db.Pool)
	auditService := service.NewAuditLogService(auditRepo)

	// Handler that returns 400
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})

	middleware := handler.AuditMiddleware(auditService, nil)
	wrappedHandler := middleware(testHandler)

	req := httptest.NewRequest(http.MethodPost, "/broadcasters", nil)
	req = addAPIKeyToContext(req, "test-key")
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	time.Sleep(10 * time.Millisecond)

	entries, _, err := auditRepo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	entry := entries[0]
	assert.Equal(t, "failure", entry.Outcome)
	assert.NotNil(t, entry.FailureReason)
	assert.Equal(t, "HTTP 400", *entry.FailureReason)
}

func TestAuditMiddleware_ExtractsResourceInfoFromStreamKeys(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	auditRepo := database.NewAuditLogRepo(db.Pool)
	auditService := service.NewAuditLogService(auditRepo)

	resourceID := uuid.New()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := handler.AuditMiddleware(auditService, nil)
	wrappedHandler := middleware(testHandler)

	req := httptest.NewRequest(http.MethodDelete, "/stream-keys/"+resourceID.String(), nil)
	req = addAPIKeyToContext(req, "test-key")
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	time.Sleep(10 * time.Millisecond)

	entries, _, err := auditRepo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// stream-keys should be mapped to stream_key
	assert.Equal(t, "stream_key", *entries[0].ResourceType)
	assert.Equal(t, &resourceID, entries[0].ResourceID)
}

func TestAuditMiddleware_HandlesXForwardedFor(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	auditRepo := database.NewAuditLogRepo(db.Pool)
	auditService := service.NewAuditLogService(auditRepo)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	middleware := handler.AuditMiddleware(auditService, nil)
	wrappedHandler := middleware(testHandler)

	req := httptest.NewRequest(http.MethodPost, "/broadcasters", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
	req = addAPIKeyToContext(req, "test-key")
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	time.Sleep(10 * time.Millisecond)

	entries, _, err := auditRepo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// Should extract first IP from X-Forwarded-For
	assert.Equal(t, "10.0.0.1", entries[0].IPAddress)
}

func TestAuditMiddleware_HandlesXRealIP(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	auditRepo := database.NewAuditLogRepo(db.Pool)
	auditService := service.NewAuditLogService(auditRepo)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	middleware := handler.AuditMiddleware(auditService, nil)
	wrappedHandler := middleware(testHandler)

	req := httptest.NewRequest(http.MethodPost, "/broadcasters", nil)
	req.Header.Set("X-Real-IP", "172.16.0.5")
	req = addAPIKeyToContext(req, "test-key")
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	time.Sleep(10 * time.Millisecond)

	entries, _, err := auditRepo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, "172.16.0.5", entries[0].IPAddress)
}

func TestAuditMiddleware_SanitizesAuthPath(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	auditRepo := database.NewAuditLogRepo(db.Pool)
	auditService := service.NewAuditLogService(auditRepo)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := handler.AuditMiddleware(auditService, nil)
	wrappedHandler := middleware(testHandler)

	// Path that might contain sensitive stream key
	req := httptest.NewRequest(http.MethodPost, "/auth?key=secret-stream-key", nil)
	req = addAPIKeyToContext(req, "test-key")
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	time.Sleep(10 * time.Millisecond)

	entries, _, err := auditRepo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// Path should be sanitized
	assert.Equal(t, "/auth", entries[0].RequestPath)
}

func TestAuditMiddleware_CapturesRequestID(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	auditRepo := database.NewAuditLogRepo(db.Pool)
	auditService := service.NewAuditLogService(auditRepo)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	middleware := handler.AuditMiddleware(auditService, nil)
	wrappedHandler := middleware(testHandler)

	req := httptest.NewRequest(http.MethodPost, "/broadcasters", nil)
	req = addAPIKeyToContext(req, "test-key")
	req = addRequestIDToContext(req, "unique-request-id-456")
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	time.Sleep(10 * time.Millisecond)

	entries, _, err := auditRepo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.NotNil(t, entries[0].RequestID)
	assert.Equal(t, "unique-request-id-456", *entries[0].RequestID)
}
