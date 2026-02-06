# Implementation Plan: Audit Logging

**Branch**: `002-audit-logging` | **Date**: 2026-02-05 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/002-audit-logging/spec.md`

## Summary

Implement a synchronous audit logging system that records all authenticated mutating API actions to PostgreSQL. The system includes:
- Automatic capture of create/update/delete operations via middleware
- Admin-only retrieval endpoint with filtering and pagination
- Custom event submission endpoint for any authenticated API key
- 90-day retention with appropriate indexing for query performance

## Technical Context

**Language/Version**: Go 1.22+
**Primary Dependencies**: gorilla/mux, pgx/v5, alpineworks/rfc9457, google/uuid
**Storage**: PostgreSQL 15 (existing `rescuestream` database)
**Testing**: testcontainers-go, stretchr/testify, net/http/httptest
**Target Platform**: Linux server (Docker)
**Project Type**: Single Go API service
**Performance Goals**: <50ms p95 for audit log writes, <1s for filtered queries on 100k entries
**Constraints**: <50ms additional latency per request, synchronous writes
**Scale/Scope**: 100k+ audit entries, retention 90 days

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| I. Go Standards Compliance | PASS | Will use standard patterns from existing handlers |
| II. Functional Options Pattern | PASS | AuditLogService will use `...Option` pattern |
| III. Comprehensive Testing | PASS | Will use testcontainers-go, httptest, testify |
| IV. RFC 9457 Error Responses | PASS | Will use alpineworks/rfc9457 for all errors |
| V. JSON-Only Protocol | PASS | All endpoints return JSON with snake_case |
| VI. Performance Requirements | PASS | Synchronous write adds <50ms, indexed queries |

**Pre-Design Gate**: PASSED

## Project Structure

### Documentation (this feature)

```text
specs/002-audit-logging/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   └── audit-api.yaml   # OpenAPI specification
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
internal/
├── domain/
│   ├── auditlog.go      # AuditLogEntry entity, repository interface, filters
│   └── errors.go        # Add ErrAdminRequired error
├── database/
│   ├── auditlog_repo.go # PostgreSQL repository implementation
│   └── migrations/
│       └── 000002_add_audit_logs.up.sql
│       └── 000002_add_audit_logs.down.sql
├── service/
│   └── auditlog.go      # AuditLogService with functional options
├── handler/
│   ├── auditlog.go      # AuditLogHandler for GET/POST endpoints
│   ├── middleware.go    # Add AuditLoggingMiddleware
│   └── errors.go        # Add admin error type
└── server/
    └── server.go        # Register new routes and middleware
```

**Structure Decision**: Single Go API service following existing patterns. New files added to existing packages - no new packages required.

## Complexity Tracking

No constitution violations requiring justification.

## Architecture Decisions

### AD-1: Synchronous vs Asynchronous Logging

**Decision**: Synchronous logging within request transaction
**Rationale**: Guarantees audit trail completeness per FR-001. Performance target (<50ms) is achievable with proper indexing.
**Trade-off**: Slight latency increase vs guaranteed capture of all actions

### AD-2: Admin Authorization Model

**Decision**: Add `admin` boolean column to existing API keys table
**Rationale**: Simpler than role-based access control; aligns with spec requirement for "admin flag"
**Implementation**: Migration adds column with default `false`, existing keys unchanged

### AD-3: Middleware Placement

**Decision**: Audit middleware wraps protected routes after auth middleware
**Rationale**: Ensures API key is available in context for actor identification
**Implementation**: Middleware captures response status and logs after handler completes

### AD-4: Custom Event Submission

**Decision**: Separate `/audit-events` endpoint from admin-only `/audit-logs`
**Rationale**: FR-012 allows any authenticated key to submit; FR-005 restricts retrieval to admin
**Implementation**: Different authorization checks per endpoint

## Key Implementation Notes

1. **IP Address Capture**: Use `r.Header.Get("X-Forwarded-For")` with fallback to `r.RemoteAddr`

2. **Sensitive Data Filtering**: Middleware must not log:
   - Stream key values (path may contain key, sanitize)
   - Request/response bodies containing secrets
   - Authentication headers

3. **Before/After State**: For updates, service layer captures entity state before modification and stores diff in metadata

4. **Correlation IDs**: Use existing `X-Request-ID` header for tracing; bulk operations share same request ID

5. **Pagination**: Use offset-based pagination with max limit of 100; return total count in response

6. **Date Range Filtering**: Accept RFC 3339 timestamps in query params
