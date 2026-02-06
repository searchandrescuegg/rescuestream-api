# Research: Audit Logging

**Feature**: 002-audit-logging | **Date**: 2026-02-05

## Phase 0 Findings

### R-1: API Key Admin Authorization Model

**Context**: The spec requires an "admin" flag on API keys (FR-005). Current codebase uses `EnvKeyStore` with a single shared secret - no database-backed API key management exists.

**Decision**: Create new `api_keys` table with admin flag

**Rationale**:
- Enables per-key admin authorization without changing existing auth flow
- Existing HMAC signature verification remains unchanged
- KeyStore interface can be extended to return admin status
- Migration adds table; existing env-based auth continues to work

**Alternatives Considered**:
1. **Environment variable for admin keys**: Rejected - doesn't scale for multiple keys
2. **Hardcoded admin key list**: Rejected - not configurable at runtime
3. **JWT claims**: Rejected - would require major auth refactor

**Implementation**:
```sql
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    key_identifier VARCHAR(255) NOT NULL UNIQUE,
    description VARCHAR(500),
    is_admin BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);
```

---

### R-2: Audit Log Entry Schema Design

**Context**: FR-002 requires: timestamp, actor, action type, resource type, resource ID, request path, outcome. FR-003 adds IP address. FR-004 adds before/after metadata.

**Decision**: Single `audit_logs` table with JSONB metadata column

**Rationale**:
- JSONB provides flexibility for before/after state without schema changes
- PostgreSQL 15 has excellent JSONB indexing and query performance
- Matches existing pattern (broadcasters, streams use JSONB metadata)

**Schema Design**:
```sql
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    actor VARCHAR(255) NOT NULL,  -- API key identifier
    action VARCHAR(50) NOT NULL,  -- create, update, delete, revoke, custom
    resource_type VARCHAR(50),    -- broadcaster, stream, stream_key, null for custom events
    resource_id UUID,             -- null for custom events
    request_method VARCHAR(10) NOT NULL,
    request_path VARCHAR(1024) NOT NULL,
    ip_address VARCHAR(45) NOT NULL,  -- IPv6 max length
    outcome VARCHAR(20) NOT NULL DEFAULT 'success',  -- success, failure
    failure_reason TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    request_id VARCHAR(36)  -- correlation ID from X-Request-ID
);
```

**Index Strategy**:
- Composite index on `(timestamp DESC)` for recent log queries
- Index on `actor` for user-specific audit trails
- Index on `resource_type, resource_id` for entity-specific queries
- Index on `action` for action-type filtering
- Composite index on `(resource_type, action, timestamp DESC)` for filtered queries

---

### R-3: IP Address Capture Strategy

**Context**: FR-003 requires IP address logging. Load balancers and proxies may mask real client IP.

**Decision**: Check X-Forwarded-For header first, fall back to RemoteAddr

**Rationale**:
- Standard pattern for reverse proxy deployments
- X-Forwarded-For contains original client IP when behind proxy
- RemoteAddr works for direct connections

**Implementation**:
```go
func getClientIP(r *http.Request) string {
    // Check X-Forwarded-For header first (load balancer/proxy)
    xff := r.Header.Get("X-Forwarded-For")
    if xff != "" {
        // Take first IP in comma-separated list
        if idx := strings.Index(xff, ","); idx != -1 {
            return strings.TrimSpace(xff[:idx])
        }
        return strings.TrimSpace(xff)
    }

    // Check X-Real-IP header (common alternative)
    xri := r.Header.Get("X-Real-IP")
    if xri != "" {
        return strings.TrimSpace(xri)
    }

    // Fall back to RemoteAddr
    ip, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        return r.RemoteAddr
    }
    return ip
}
```

---

### R-4: Sensitive Data Filtering

**Context**: FR-010 prohibits logging stream key values, passwords, or secrets.

**Decision**: Sanitize at middleware level before audit entry creation

**Patterns to Filter**:
1. **Stream key in path**: `/auth` path contains stream key as user in basic auth
2. **Request headers**: Never log X-API-Key, X-Signature, Authorization
3. **Request body**: Don't log request bodies that may contain secrets

**Implementation**:
- Audit middleware logs path and method only, not full request/response
- Before/after metadata captures entity state from service layer (already sanitized)
- Stream key `key_value` field never included in metadata

---

### R-5: Synchronous Logging Performance

**Context**: SC-004 requires <50ms additional latency. FR-001 requires synchronous writes.

**Decision**: Single INSERT within request lifecycle, rely on PostgreSQL connection pooling

**Performance Analysis**:
- PostgreSQL INSERT to indexed table: ~1-5ms typical
- pgx connection pool eliminates connection overhead
- Prepared statements reduce parse time
- 50ms budget is generous for single insert

**Mitigation if Needed**:
- Batch inserts with short buffer (100ms) - only if monitoring shows issues
- Background goroutine with channel - violates sync requirement

**Recommendation**: Implement synchronous first, monitor in production, optimize only if needed.

---

### R-6: Middleware Design for Automatic Logging

**Context**: Need to capture all mutating requests automatically without modifying each handler.

**Decision**: Wrap response to capture status code, log after handler completes

**Pattern**:
```go
func AuditMiddleware(auditService *service.AuditLogService) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Skip non-mutating methods
            if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
                next.ServeHTTP(w, r)
                return
            }

            // Capture response status
            wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
            next.ServeHTTP(wrapped, r)

            // Log after handler completes
            entry := buildAuditEntry(r, wrapped.statusCode)
            if err := auditService.Create(r.Context(), entry); err != nil {
                // Log error but don't fail request (per edge case spec)
                slog.Error("audit logging failed", "error", err)
            }
        })
    }
}
```

**Placement**: After auth middleware (need API key in context), before handlers.

---

### R-7: Custom Event Submission Model

**Context**: FR-012 requires endpoint for custom events (login, logout, started_stream) accessible to any authenticated key.

**Decision**: POST /audit-events endpoint with simplified schema

**Request Schema**:
```json
{
    "event_type": "login",          // required
    "metadata": {                    // optional
        "session_id": "abc123",
        "browser": "Chrome"
    }
}
```

**Mapping to AuditLogEntry**:
- `action`: event_type value
- `resource_type`: null (custom events don't target resources)
- `resource_id`: null
- `actor`: API key from auth context
- Other fields populated from request context

---

### R-8: Pagination Implementation

**Context**: FR-007 requires pagination with default 50, max 100.

**Decision**: Offset-based pagination with total count

**Query Pattern**:
```sql
SELECT * FROM audit_logs
WHERE ... (filters)
ORDER BY timestamp DESC
LIMIT $limit OFFSET $offset;

SELECT COUNT(*) FROM audit_logs WHERE ... (filters);
```

**Response Format**:
```json
{
    "audit_logs": [...],
    "pagination": {
        "limit": 50,
        "offset": 0,
        "total": 1234
    }
}
```

**Rationale**: Offset-based is simpler than cursor-based and sufficient for 100k entries with proper indexing.

---

### R-9: Date Range Filtering

**Context**: FR-006 requires filtering by date range.

**Decision**: Accept ISO 8601 / RFC 3339 timestamps in query parameters

**Query Parameters**:
- `from`: Start timestamp (inclusive), e.g., `2026-02-01T00:00:00Z`
- `to`: End timestamp (exclusive), e.g., `2026-02-05T23:59:59Z`

**Validation**:
- Parse with `time.Parse(time.RFC3339, value)`
- Return 400 if invalid format
- If only `from` provided, no upper bound
- If only `to` provided, no lower bound

---

## Constitution Compliance Verification

| Research Item | Constitution Principle | Compliance |
|---------------|----------------------|------------|
| R-2 Schema | V. JSON-Only, snake_case | ✓ All fields use snake_case |
| R-5 Performance | VI. <50ms p95 | ✓ Single INSERT well within budget |
| R-6 Middleware | I. Go Standards | ✓ Standard http.Handler pattern |
| R-7 Custom Events | IV. RFC 9457 Errors | ✓ Will use rfc9457 for validation errors |
| R-8 Pagination | V. JSON-Only | ✓ JSON response with pagination object |

---

## Open Items (None)

All NEEDS CLARIFICATION items have been resolved through codebase research and spec clarification session.
