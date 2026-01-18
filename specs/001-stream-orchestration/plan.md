# Implementation Plan: Stream Orchestration API

**Branch**: `001-stream-orchestration` | **Date**: 2026-01-17 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-stream-orchestration/spec.md`

## Summary

Build a Go API that orchestrates video streams via MediaMTX integration. The API provides:
stream key authentication for MediaMTX's external auth mechanism, webhook endpoints for
stream lifecycle events (onReady/onNotReady), stream key management for administrators,
and active stream listing for the frontend. All data persisted in PostgreSQL with
sub-50ms response time targets.

## Technical Context

**Language/Version**: Go 1.22+
**Primary Dependencies**:
- `alpineworks.io/gomediamtx` - MediaMTX API client
- `github.com/jackc/pgx/v5/pgxpool` - PostgreSQL connection pooling
- `github.com/exaring/otelpgx` - OpenTelemetry instrumentation for pgx
- `github.com/alpineworks/rfc9457` - RFC 9457 error responses
- `alpineworks.io/ootel` - OpenTelemetry setup (existing)
- `github.com/caarlos0/env/v11` - Environment configuration (existing)
- `log/slog` - Structured logging (stdlib)

**Testing**:
- `github.com/testcontainers/testcontainers-go` - PostgreSQL containers
- `net/http/httptest` - HTTP handler testing
- `github.com/stretchr/testify` - Assertions (assert, require)

**Storage**: PostgreSQL via pgxpool with otelpgx tracing
**Target Platform**: Linux server (containerized)
**Project Type**: Single API service
**Performance Goals**: <50ms p95 for auth/list endpoints, <100ms p95 for DB writes
**Constraints**: <50ms auth response (MediaMTX timeout), 100 concurrent auth requests
**Scale/Scope**: Dozens of concurrent streams, single PostgreSQL instance

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Go Standards Compliance | PASS | Using stdlib patterns, will enforce via golangci-lint |
| II. Functional Options Pattern | PASS | Will implement for DB client, MediaMTX client wrappers |
| III. Comprehensive Testing | PASS | testcontainers for Postgres, httptest for handlers, testify assertions |
| IV. RFC 9457 Error Responses | PASS | Using `github.com/alpineworks/rfc9457` package |
| V. JSON-Only Protocol | PASS | All endpoints return JSON, snake_case fields, RFC 3339 dates |
| VI. Performance Requirements | PASS | <50ms target, pgxpool for connection pooling, prepared statements |

**Technical Constraints Check**:
- Authentication & Security: Shared secret via header (X-API-Key), env vars for secrets
- Dependencies: Go modules with checksums, rfc9457 package required
- Observability: slog for logging, ootel for metrics/traces, otelpgx for DB tracing

## Project Structure

### Documentation (this feature)

```text
specs/001-stream-orchestration/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output (OpenAPI specs)
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
cmd/
└── rescuestream-api/
    └── main.go              # Application entrypoint (existing, will extend)

internal/
├── config/
│   └── config.go            # Environment configuration (existing, will extend)
├── logging/
│   └── logging.go           # Logging utilities (existing)
├── database/
│   ├── database.go          # pgxpool client with functional options
│   ├── migrations/          # SQL migration files
│   └── queries/             # SQL query files (sqlc or raw)
├── domain/
│   ├── streamkey.go         # Stream key entity and repository interface
│   ├── stream.go            # Stream entity and repository interface
│   └── broadcaster.go       # Broadcaster entity and repository interface
├── service/
│   ├── auth.go              # Stream key authentication service
│   ├── streamkey.go         # Stream key management service
│   ├── stream.go            # Stream listing/details service
│   └── mediamtx.go          # MediaMTX client wrapper with functional options
├── handler/
│   ├── auth.go              # POST /auth (MediaMTX external auth)
│   ├── webhook.go           # POST /webhook/ready, /webhook/not-ready
│   ├── stream.go            # GET /streams, GET /streams/{id}
│   ├── streamkey.go         # POST/GET/DELETE /stream-keys
│   └── middleware.go        # Shared secret auth, request ID, logging
└── server/
    └── server.go            # HTTP server setup with graceful shutdown

tests/
├── integration/
│   ├── auth_test.go         # Auth endpoint with testcontainers
│   ├── stream_test.go       # Stream endpoints with testcontainers
│   └── streamkey_test.go    # Stream key management with testcontainers
└── unit/
    ├── service/             # Service layer unit tests
    └── handler/             # Handler unit tests with mocks
```

**Structure Decision**: Single Go project following standard layout. The `internal/`
package prevents external imports. Domain entities are separated from persistence
(repository pattern) to enable testing with mocks. Services contain business logic,
handlers are thin HTTP adapters.

## Complexity Tracking

No constitution violations requiring justification. The repository pattern adds
slight complexity but is justified by:
1. Enables unit testing services without database
2. Constitution requires testcontainers for integration tests (need both layers)
3. Clean separation supports future caching layer insertion
