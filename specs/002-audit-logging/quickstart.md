# Quickstart: Audit Logging Implementation

**Feature**: 002-audit-logging | **Date**: 2026-02-05

This guide provides step-by-step implementation instructions following the established codebase patterns.

## Prerequisites

- Go 1.22+
- PostgreSQL 15 running (via docker-compose)
- Existing codebase patterns understood (see [research.md](research.md))

## Implementation Order

1. Database migration
2. Domain model and repository interface
3. Database repository implementation
4. Service layer
5. Handler
6. Middleware for automatic logging
7. Server routing updates
8. Tests

---

## Step 1: Database Migration

Create migration files in `internal/database/migrations/`:

**000002_add_audit_logs.up.sql**
```sql
-- See data-model.md for full schema
```

**000002_add_audit_logs.down.sql**
```sql
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS api_keys;
```

Run migration:
```bash
# Migrations run automatically on startup, or manually:
go run cmd/migrate/main.go up
```

---

## Step 2: Domain Model

Create `internal/domain/auditlog.go`:

```go
package domain

import (
    "context"
    "time"

    "github.com/google/uuid"
)

// AuditLogEntry represents a single audit log record.
type AuditLogEntry struct {
    ID            uuid.UUID              `json:"id"`
    Timestamp     time.Time              `json:"timestamp"`
    Actor         string                 `json:"actor"`
    Action        string                 `json:"action"`
    ResourceType  *string                `json:"resource_type,omitempty"`
    ResourceID    *uuid.UUID             `json:"resource_id,omitempty"`
    RequestMethod string                 `json:"request_method"`
    RequestPath   string                 `json:"request_path"`
    IPAddress     string                 `json:"ip_address"`
    Outcome       string                 `json:"outcome"`
    FailureReason *string                `json:"failure_reason,omitempty"`
    Metadata      map[string]interface{} `json:"metadata"`
    RequestID     *string                `json:"request_id,omitempty"`
}

// AuditLogFilter defines filter criteria for listing audit logs.
type AuditLogFilter struct {
    From         *time.Time
    To           *time.Time
    Actor        *string
    Action       *string
    ResourceType *string
    ResourceID   *uuid.UUID
    Limit        int
    Offset       int
}

// AuditLogRepository defines the interface for audit log persistence.
type AuditLogRepository interface {
    Create(ctx context.Context, entry *AuditLogEntry) error
    List(ctx context.Context, filter AuditLogFilter) ([]AuditLogEntry, int64, error)
}
```

Add to `internal/domain/errors.go`:
```go
var ErrAdminRequired = errors.New("admin privileges required")
```

---

## Step 3: Database Repository

Create `internal/database/auditlog_repo.go`:

```go
package database

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

type AuditLogRepo struct {
    pool *pgxpool.Pool
}

func NewAuditLogRepo(pool *pgxpool.Pool) *AuditLogRepo {
    return &AuditLogRepo{pool: pool}
}

func (r *AuditLogRepo) Create(ctx context.Context, entry *domain.AuditLogEntry) error {
    if entry.ID == uuid.Nil {
        entry.ID = uuid.New()
    }

    metadataJSON, err := json.Marshal(entry.Metadata)
    if err != nil {
        return fmt.Errorf("failed to marshal metadata: %w", err)
    }

    query := `
        INSERT INTO audit_logs (
            id, timestamp, actor, action, resource_type, resource_id,
            request_method, request_path, ip_address, outcome,
            failure_reason, metadata, request_id
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
    `

    _, err = r.pool.Exec(ctx, query,
        entry.ID,
        entry.Timestamp,
        entry.Actor,
        entry.Action,
        entry.ResourceType,
        entry.ResourceID,
        entry.RequestMethod,
        entry.RequestPath,
        entry.IPAddress,
        entry.Outcome,
        entry.FailureReason,
        metadataJSON,
        entry.RequestID,
    )
    if err != nil {
        return fmt.Errorf("failed to create audit log entry: %w", err)
    }

    return nil
}

func (r *AuditLogRepo) List(ctx context.Context, filter domain.AuditLogFilter) ([]domain.AuditLogEntry, int64, error) {
    // Build dynamic WHERE clause
    var conditions []string
    var args []interface{}
    argNum := 1

    if filter.From != nil {
        conditions = append(conditions, fmt.Sprintf("timestamp >= $%d", argNum))
        args = append(args, *filter.From)
        argNum++
    }
    if filter.To != nil {
        conditions = append(conditions, fmt.Sprintf("timestamp < $%d", argNum))
        args = append(args, *filter.To)
        argNum++
    }
    if filter.Actor != nil {
        conditions = append(conditions, fmt.Sprintf("actor = $%d", argNum))
        args = append(args, *filter.Actor)
        argNum++
    }
    if filter.Action != nil {
        conditions = append(conditions, fmt.Sprintf("action = $%d", argNum))
        args = append(args, *filter.Action)
        argNum++
    }
    if filter.ResourceType != nil {
        conditions = append(conditions, fmt.Sprintf("resource_type = $%d", argNum))
        args = append(args, *filter.ResourceType)
        argNum++
    }
    if filter.ResourceID != nil {
        conditions = append(conditions, fmt.Sprintf("resource_id = $%d", argNum))
        args = append(args, *filter.ResourceID)
        argNum++
    }

    whereClause := ""
    if len(conditions) > 0 {
        whereClause = "WHERE " + strings.Join(conditions, " AND ")
    }

    // Count query
    countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_logs %s", whereClause)
    var total int64
    if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
        return nil, 0, fmt.Errorf("failed to count audit logs: %w", err)
    }

    // Data query with pagination
    dataQuery := fmt.Sprintf(`
        SELECT id, timestamp, actor, action, resource_type, resource_id,
               request_method, request_path, ip_address, outcome,
               failure_reason, metadata, request_id
        FROM audit_logs
        %s
        ORDER BY timestamp DESC
        LIMIT $%d OFFSET $%d
    `, whereClause, argNum, argNum+1)

    args = append(args, filter.Limit, filter.Offset)

    rows, err := r.pool.Query(ctx, dataQuery, args...)
    if err != nil {
        return nil, 0, fmt.Errorf("failed to query audit logs: %w", err)
    }
    defer rows.Close()

    var entries []domain.AuditLogEntry
    for rows.Next() {
        var entry domain.AuditLogEntry
        var metadataJSON []byte

        err := rows.Scan(
            &entry.ID,
            &entry.Timestamp,
            &entry.Actor,
            &entry.Action,
            &entry.ResourceType,
            &entry.ResourceID,
            &entry.RequestMethod,
            &entry.RequestPath,
            &entry.IPAddress,
            &entry.Outcome,
            &entry.FailureReason,
            &metadataJSON,
            &entry.RequestID,
        )
        if err != nil {
            return nil, 0, fmt.Errorf("failed to scan audit log: %w", err)
        }

        if err := json.Unmarshal(metadataJSON, &entry.Metadata); err != nil {
            return nil, 0, fmt.Errorf("failed to unmarshal metadata: %w", err)
        }

        entries = append(entries, entry)
    }

    return entries, total, nil
}
```

---

## Step 4: Service Layer

Create `internal/service/auditlog.go`:

```go
package service

import (
    "context"
    "log/slog"
    "time"

    "github.com/google/uuid"
    "github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

type AuditLogService struct {
    auditRepo domain.AuditLogRepository
    logger    *slog.Logger
}

type AuditLogServiceOption func(*AuditLogService)

func WithAuditLogLogger(logger *slog.Logger) AuditLogServiceOption {
    return func(s *AuditLogService) {
        s.logger = logger
    }
}

func NewAuditLogService(
    auditRepo domain.AuditLogRepository,
    opts ...AuditLogServiceOption,
) *AuditLogService {
    s := &AuditLogService{
        auditRepo: auditRepo,
        logger:    slog.Default(),
    }
    for _, opt := range opts {
        opt(s)
    }
    return s
}

// CreateEntry creates a new audit log entry.
func (s *AuditLogService) CreateEntry(ctx context.Context, entry *domain.AuditLogEntry) error {
    if entry.ID == uuid.Nil {
        entry.ID = uuid.New()
    }
    if entry.Timestamp.IsZero() {
        entry.Timestamp = time.Now()
    }
    if entry.Metadata == nil {
        entry.Metadata = make(map[string]interface{})
    }
    if entry.Outcome == "" {
        entry.Outcome = "success"
    }

    if err := s.auditRepo.Create(ctx, entry); err != nil {
        s.logger.Error("failed to create audit log entry",
            slog.String("error", err.Error()),
            slog.String("action", entry.Action),
        )
        return err
    }

    s.logger.Debug("audit log entry created",
        slog.String("entry_id", entry.ID.String()),
        slog.String("action", entry.Action),
        slog.String("actor", entry.Actor),
    )

    return nil
}

// List retrieves audit log entries with filtering.
func (s *AuditLogService) List(ctx context.Context, filter domain.AuditLogFilter) ([]domain.AuditLogEntry, int64, error) {
    // Apply defaults
    if filter.Limit == 0 {
        filter.Limit = 50
    }
    if filter.Limit > 100 {
        filter.Limit = 100
    }

    entries, total, err := s.auditRepo.List(ctx, filter)
    if err != nil {
        s.logger.Error("failed to list audit logs",
            slog.String("error", err.Error()),
        )
        return nil, 0, err
    }

    return entries, total, nil
}

// CreateCustomEvent creates an audit entry for a custom event.
func (s *AuditLogService) CreateCustomEvent(
    ctx context.Context,
    actor string,
    eventType string,
    metadata map[string]interface{},
    ipAddress string,
    requestID string,
) (*domain.AuditLogEntry, error) {
    entry := &domain.AuditLogEntry{
        ID:            uuid.New(),
        Timestamp:     time.Now(),
        Actor:         actor,
        Action:        eventType,
        ResourceType:  nil,
        ResourceID:    nil,
        RequestMethod: "POST",
        RequestPath:   "/audit-events",
        IPAddress:     ipAddress,
        Outcome:       "success",
        Metadata:      metadata,
        RequestID:     &requestID,
    }

    if err := s.auditRepo.Create(ctx, entry); err != nil {
        return nil, err
    }

    return entry, nil
}
```

---

## Step 5: Handler

Create `internal/handler/auditlog.go`:

```go
package handler

import (
    "encoding/json"
    "log/slog"
    "net/http"
    "strconv"
    "time"

    "github.com/google/uuid"
    "github.com/searchandrescuegg/rescuestream-api/internal/domain"
    "github.com/searchandrescuegg/rescuestream-api/internal/service"
)

type AuditLogHandler struct {
    auditService *service.AuditLogService
    adminChecker AdminChecker
    logger       *slog.Logger
}

// AdminChecker checks if an API key has admin privileges.
type AdminChecker interface {
    IsAdmin(ctx context.Context, apiKey string) (bool, error)
}

func NewAuditLogHandler(
    auditService *service.AuditLogService,
    adminChecker AdminChecker,
    logger *slog.Logger,
) *AuditLogHandler {
    if logger == nil {
        logger = slog.Default()
    }
    return &AuditLogHandler{
        auditService: auditService,
        adminChecker: adminChecker,
        logger:       logger,
    }
}

// ServeHTTP routes audit log requests.
func (h *AuditLogHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    switch r.URL.Path {
    case "/audit-logs":
        if r.Method == http.MethodGet {
            h.listAuditLogs(w, r)
            return
        }
    case "/audit-events":
        if r.Method == http.MethodPost {
            h.createAuditEvent(w, r)
            return
        }
    }
    WriteError(w, r, ErrInvalidRequest("method not allowed"))
}

type AuditLogListResponse struct {
    AuditLogs  []domain.AuditLogEntry `json:"audit_logs"`
    Pagination PaginationResponse     `json:"pagination"`
}

type PaginationResponse struct {
    Limit  int   `json:"limit"`
    Offset int   `json:"offset"`
    Total  int64 `json:"total"`
}

func (h *AuditLogHandler) listAuditLogs(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    apiKey := APIKeyFromContext(ctx)

    // Check admin status
    isAdmin, err := h.adminChecker.IsAdmin(ctx, apiKey)
    if err != nil || !isAdmin {
        WriteError(w, r, ErrForbidden("Admin privileges required to access audit logs"))
        return
    }

    // Parse query parameters
    filter, err := h.parseAuditLogFilter(r)
    if err != nil {
        WriteError(w, r, ErrInvalidRequest(err.Error()))
        return
    }

    entries, total, err := h.auditService.List(ctx, filter)
    if err != nil {
        WriteError(w, r, MapDomainError(err))
        return
    }

    response := AuditLogListResponse{
        AuditLogs: entries,
        Pagination: PaginationResponse{
            Limit:  filter.Limit,
            Offset: filter.Offset,
            Total:  total,
        },
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

func (h *AuditLogHandler) parseAuditLogFilter(r *http.Request) (domain.AuditLogFilter, error) {
    filter := domain.AuditLogFilter{
        Limit:  50,
        Offset: 0,
    }

    query := r.URL.Query()

    if fromStr := query.Get("from"); fromStr != "" {
        from, err := time.Parse(time.RFC3339, fromStr)
        if err != nil {
            return filter, fmt.Errorf("invalid 'from' date format, expected RFC 3339")
        }
        filter.From = &from
    }

    if toStr := query.Get("to"); toStr != "" {
        to, err := time.Parse(time.RFC3339, toStr)
        if err != nil {
            return filter, fmt.Errorf("invalid 'to' date format, expected RFC 3339")
        }
        filter.To = &to
    }

    if actor := query.Get("actor"); actor != "" {
        filter.Actor = &actor
    }

    if action := query.Get("action"); action != "" {
        filter.Action = &action
    }

    if resourceType := query.Get("resource_type"); resourceType != "" {
        filter.ResourceType = &resourceType
    }

    if resourceIDStr := query.Get("resource_id"); resourceIDStr != "" {
        resourceID, err := uuid.Parse(resourceIDStr)
        if err != nil {
            return filter, fmt.Errorf("invalid 'resource_id' format")
        }
        filter.ResourceID = &resourceID
    }

    if limitStr := query.Get("limit"); limitStr != "" {
        limit, err := strconv.Atoi(limitStr)
        if err != nil || limit < 1 || limit > 100 {
            return filter, fmt.Errorf("limit must be between 1 and 100")
        }
        filter.Limit = limit
    }

    if offsetStr := query.Get("offset"); offsetStr != "" {
        offset, err := strconv.Atoi(offsetStr)
        if err != nil || offset < 0 {
            return filter, fmt.Errorf("offset must be non-negative")
        }
        filter.Offset = offset
    }

    return filter, nil
}

type CreateAuditEventRequest struct {
    EventType string                 `json:"event_type"`
    Metadata  map[string]interface{} `json:"metadata"`
}

func (h *AuditLogHandler) createAuditEvent(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    apiKey := APIKeyFromContext(ctx)
    requestID := RequestIDFromContext(ctx)

    var req CreateAuditEventRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        WriteError(w, r, ErrInvalidRequest("invalid JSON body"))
        return
    }

    if req.EventType == "" {
        WriteError(w, r, ErrInvalidRequest("event_type is required"))
        return
    }

    if len(req.EventType) > 50 {
        WriteError(w, r, ErrInvalidRequest("event_type must be 50 characters or less"))
        return
    }

    ipAddress := getClientIP(r)

    entry, err := h.auditService.CreateCustomEvent(
        ctx,
        apiKey,
        req.EventType,
        req.Metadata,
        ipAddress,
        requestID,
    )
    if err != nil {
        WriteError(w, r, MapDomainError(err))
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(entry)
}
```

---

## Step 6: Audit Middleware

Add to `internal/handler/middleware.go`:

```go
// AuditMiddleware logs all mutating requests to the audit log.
func AuditMiddleware(auditService *service.AuditLogService) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Skip non-mutating methods
            if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
                next.ServeHTTP(w, r)
                return
            }

            // Skip audit endpoints to prevent recursion
            if r.URL.Path == "/audit-events" || r.URL.Path == "/audit-logs" {
                next.ServeHTTP(w, r)
                return
            }

            // Capture response status
            wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
            next.ServeHTTP(wrapped, r)

            // Build and log audit entry
            ctx := r.Context()
            apiKey := APIKeyFromContext(ctx)
            requestID := RequestIDFromContext(ctx)

            entry := &domain.AuditLogEntry{
                Actor:         apiKey,
                Action:        methodToAction(r.Method),
                RequestMethod: r.Method,
                RequestPath:   sanitizePath(r.URL.Path),
                IPAddress:     getClientIP(r),
                Outcome:       statusToOutcome(wrapped.statusCode),
                RequestID:     &requestID,
                Metadata:      make(map[string]interface{}),
            }

            // Extract resource info from path
            resourceType, resourceID := extractResourceInfo(r.URL.Path)
            if resourceType != "" {
                entry.ResourceType = &resourceType
            }
            if resourceID != uuid.Nil {
                entry.ResourceID = &resourceID
            }

            if wrapped.statusCode >= 400 {
                reason := fmt.Sprintf("HTTP %d", wrapped.statusCode)
                entry.FailureReason = &reason
            }

            if err := auditService.CreateEntry(ctx, entry); err != nil {
                slog.Error("audit logging failed",
                    slog.String("error", err.Error()),
                    slog.String("path", r.URL.Path),
                )
            }
        })
    }
}

func methodToAction(method string) string {
    switch method {
    case http.MethodPost:
        return "create"
    case http.MethodPatch, http.MethodPut:
        return "update"
    case http.MethodDelete:
        return "delete"
    default:
        return "unknown"
    }
}

func statusToOutcome(status int) string {
    if status >= 200 && status < 400 {
        return "success"
    }
    return "failure"
}

func getClientIP(r *http.Request) string {
    xff := r.Header.Get("X-Forwarded-For")
    if xff != "" {
        if idx := strings.Index(xff, ","); idx != -1 {
            return strings.TrimSpace(xff[:idx])
        }
        return strings.TrimSpace(xff)
    }

    xri := r.Header.Get("X-Real-IP")
    if xri != "" {
        return strings.TrimSpace(xri)
    }

    ip, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        return r.RemoteAddr
    }
    return ip
}

func sanitizePath(path string) string {
    // Don't log stream key values that may appear in paths
    // Pattern: /auth with basic auth containing stream key
    if strings.HasPrefix(path, "/auth") {
        return "/auth"
    }
    return path
}

func extractResourceInfo(path string) (string, uuid.UUID) {
    parts := strings.Split(strings.Trim(path, "/"), "/")
    if len(parts) < 1 {
        return "", uuid.Nil
    }

    resourceType := parts[0]
    // Map plural to singular
    switch resourceType {
    case "broadcasters":
        resourceType = "broadcaster"
    case "streams":
        resourceType = "stream"
    case "stream-keys":
        resourceType = "stream_key"
    default:
        return "", uuid.Nil
    }

    if len(parts) >= 2 {
        if id, err := uuid.Parse(parts[1]); err == nil {
            return resourceType, id
        }
    }

    return resourceType, uuid.Nil
}
```

---

## Step 7: Server Routing

Update `internal/server/server.go`:

```go
// In Server struct, add:
auditLogHandler *handler.AuditLogHandler
auditService    *service.AuditLogService

// In setupRoutes(), add to protected routes:
if s.auditLogHandler != nil {
    protected.Handle("/audit-logs", s.auditLogHandler).Methods(http.MethodGet)
    protected.Handle("/audit-events", s.auditLogHandler).Methods(http.MethodPost)
}

// Add audit middleware to protected routes (after auth, before handlers):
protected.Use(handler.AuditMiddleware(s.auditService))
```

---

## Testing Checklist

- [ ] Unit tests for AuditLogRepo.Create and List
- [ ] Unit tests for AuditLogService.CreateEntry, List, CreateCustomEvent
- [ ] Handler tests for GET /audit-logs with filters
- [ ] Handler tests for POST /audit-events
- [ ] Integration test: verify middleware logs mutating requests
- [ ] Integration test: verify admin-only access to /audit-logs
- [ ] Integration test: verify any auth key can POST to /audit-events
- [ ] Performance test: verify <50ms insert latency

---

## Verification Commands

```bash
# Run migrations
docker-compose up -d postgres
go run cmd/migrate/main.go up

# Run tests
go test ./internal/handler/... -v
go test ./internal/service/... -v
go test ./internal/database/... -v

# Test endpoints manually
./scripts/api-test.sh POST /audit-events '{"event_type":"login"}'
./scripts/api-test.sh GET /audit-logs
./scripts/api-test.sh GET '/audit-logs?action=create&limit=10'
```
