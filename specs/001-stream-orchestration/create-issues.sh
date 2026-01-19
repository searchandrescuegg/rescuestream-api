#!/bin/bash
# Create GitHub issues for Stream Orchestration API tasks
# Repository: searchandrescuegg/rescuestream-api
# Feature: 001-stream-orchestration

set -e

REPO="searchandrescuegg/rescuestream-api"
FEATURE_LABEL="001-stream-orchestration"

# Create feature label if it doesn't exist
gh label create "$FEATURE_LABEL" --repo "$REPO" --color "0366d6" --description "Stream Orchestration API feature" 2>/dev/null || true
gh label create "phase:setup" --repo "$REPO" --color "c5def5" --description "Setup phase tasks" 2>/dev/null || true
gh label create "phase:foundational" --repo "$REPO" --color "bfdadc" --description "Foundational phase tasks" 2>/dev/null || true
gh label create "phase:polish" --repo "$REPO" --color "d4c5f9" --description "Polish phase tasks" 2>/dev/null || true
gh label create "user-story:US1" --repo "$REPO" --color "fbca04" --description "User Story 1 - Stream Key Authentication" 2>/dev/null || true
gh label create "user-story:US2" --repo "$REPO" --color "f9d0c4" --description "User Story 2 - Active Stream Listing" 2>/dev/null || true
gh label create "user-story:US3" --repo "$REPO" --color "c2e0c6" --description "User Story 3 - Stream Key Management" 2>/dev/null || true
gh label create "user-story:US4" --repo "$REPO" --color "e6e6e6" --description "User Story 4 - Individual Stream Details" 2>/dev/null || true
gh label create "parallelizable" --repo "$REPO" --color "5319e7" --description "Can run in parallel with other tasks" 2>/dev/null || true

echo "Creating issues..."

# ============================================================
# SKIPPING T001-T040 (already created as issues 1-41)
# ============================================================
: <<'SKIP_ALREADY_CREATED'

# Phase 1: Setup
gh issue create --repo "$REPO" --title "T001: Add new dependencies to go.mod" --label "$FEATURE_LABEL,phase:setup" --body "$(cat <<'EOF'
## Task
Add new dependencies to go.mod: pgxpool, otelpgx, rfc9457, gomediamtx, testcontainers-go, testify

## Dependencies to add
```go
github.com/jackc/pgx/v5/pgxpool
github.com/exaring/otelpgx
github.com/alpineworks/rfc9457
alpineworks.io/gomediamtx
github.com/testcontainers/testcontainers-go
github.com/stretchr/testify
```

## Acceptance Criteria
- [ ] All dependencies added to go.mod
- [ ] `go mod tidy` runs successfully
- [ ] `go build ./...` succeeds

## Phase
Setup (Phase 1)
EOF
)"

gh issue create --repo "$REPO" --title "T002: Create directory structure per plan.md" --label "$FEATURE_LABEL,phase:setup,parallelizable" --body "$(cat <<'EOF'
## Task
Create directory structure per plan.md

## Directories to create
```
internal/database/
internal/database/migrations/
internal/domain/
internal/service/
internal/handler/
internal/server/
tests/integration/
tests/unit/
tests/unit/service/
tests/unit/handler/
```

## Acceptance Criteria
- [ ] All directories created
- [ ] .gitkeep files added where needed

## Phase
Setup (Phase 1) - Can run in parallel with T003, T004
EOF
)"

gh issue create --repo "$REPO" --title "T003: Configure golangci-lint with .golangci.yml" --label "$FEATURE_LABEL,phase:setup,parallelizable" --body "$(cat <<'EOF'
## Task
Configure golangci-lint with .golangci.yml for Go standards compliance

## Requirements
- Enable standard linters: govet, errcheck, staticcheck, gosimple, ineffassign
- Configure for Go 1.22+
- Exclude test files from certain checks as appropriate

## Acceptance Criteria
- [ ] .golangci.yml created at repository root
- [ ] `golangci-lint run` passes on existing code
- [ ] Configuration aligns with Constitution Principle I (Go Standards Compliance)

## Phase
Setup (Phase 1) - Can run in parallel with T002, T004
EOF
)"

gh issue create --repo "$REPO" --title "T004: Create Makefile with targets" --label "$FEATURE_LABEL,phase:setup,parallelizable" --body "$(cat <<'EOF'
## Task
Create Makefile with targets: build, test, lint, migrate, run

## Targets
- `make build` - Build the application
- `make test` - Run all tests
- `make lint` - Run golangci-lint
- `make migrate` - Run database migrations
- `make run` - Run the application locally

## Acceptance Criteria
- [ ] Makefile created at repository root
- [ ] All targets work correctly
- [ ] `make help` shows available targets

## Phase
Setup (Phase 1) - Can run in parallel with T002, T003
EOF
)"

# Phase 2: Foundational
gh issue create --repo "$REPO" --title "T005: Extend config.go with new environment variables" --label "$FEATURE_LABEL,phase:foundational" --body "$(cat <<'EOF'
## Task
Extend internal/config/config.go with new env vars: DATABASE_URL, API_SECRET, MEDIAMTX_API_URL, MEDIAMTX_PUBLIC_URL, API_PORT

## File
`internal/config/config.go`

## New fields
```go
DatabaseURL      string `env:"DATABASE_URL" envRequired:"true"`
APISecret        string `env:"API_SECRET" envRequired:"true"`
MediaMTXAPIURL   string `env:"MEDIAMTX_API_URL" envDefault:"http://localhost:9997"`
MediaMTXPublicURL string `env:"MEDIAMTX_PUBLIC_URL" envDefault:"http://localhost:8889"`
APIPort          int    `env:"API_PORT" envDefault:"8080"`
```

## Acceptance Criteria
- [ ] Config struct extended with new fields
- [ ] Environment variable parsing works
- [ ] Required fields validated

## Phase
Foundational (Phase 2)
EOF
)"

gh issue create --repo "$REPO" --title "T006: Create database.go with pgxpool client" --label "$FEATURE_LABEL,phase:foundational" --body "$(cat <<'EOF'
## Task
Create internal/database/database.go with pgxpool client using functional options pattern

## File
`internal/database/database.go`

## Requirements
- Functional options pattern per Constitution Principle II
- Connection pooling with configurable min/max connections
- otelpgx tracing integration
- Context-aware operations

## Example signature
```go
type Option func(*Client)

func NewClient(ctx context.Context, connString string, opts ...Option) (*Client, error)
func WithMinConns(n int32) Option
func WithMaxConns(n int32) Option
func WithTracer(tracer trace.Tracer) Option
```

## Acceptance Criteria
- [ ] Functional options pattern implemented
- [ ] pgxpool configured with sensible defaults
- [ ] otelpgx tracing enabled
- [ ] Graceful close method

## Phase
Foundational (Phase 2)
EOF
)"

gh issue create --repo "$REPO" --title "T007: Create initial database migration" --label "$FEATURE_LABEL,phase:foundational" --body "$(cat <<'EOF'
## Task
Create internal/database/migrations/001_initial_schema.sql with broadcasters, stream_keys, streams tables from data-model.md

## File
`internal/database/migrations/001_initial_schema.sql`

## Tables
- `broadcasters` - Broadcaster entities
- `stream_keys` - Stream key credentials
- `streams` - Active/historical streams

## Requirements
- UUID primary keys
- Proper foreign key relationships
- Indexes for common queries
- Partial unique index for one active stream per key

## Acceptance Criteria
- [ ] All three tables created
- [ ] Indexes defined per data-model.md
- [ ] Constraints and triggers in place
- [ ] Migration is idempotent

## Phase
Foundational (Phase 2)
EOF
)"

gh issue create --repo "$REPO" --title "T008: Create migrate.go with migration runner" --label "$FEATURE_LABEL,phase:foundational" --body "$(cat <<'EOF'
## Task
Create internal/database/migrate.go with migration runner using embed for SQL files

## File
`internal/database/migrate.go`

## Requirements
- Use Go embed to include SQL files
- Track applied migrations in database
- Support up/down migrations
- Transaction per migration

## Acceptance Criteria
- [ ] Migrations embedded at compile time
- [ ] Migration tracking table created
- [ ] Migrations run in order
- [ ] Rollback support (if needed)

## Phase
Foundational (Phase 2)
EOF
)"

gh issue create --repo "$REPO" --title "T009: Create broadcaster.go domain entity" --label "$FEATURE_LABEL,phase:foundational,parallelizable" --body "$(cat <<'EOF'
## Task
Create internal/domain/broadcaster.go with Broadcaster entity and BroadcasterRepository interface

## File
`internal/domain/broadcaster.go`

## Entity
```go
type Broadcaster struct {
    ID          uuid.UUID
    DisplayName string
    Metadata    map[string]interface{}
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

## Repository Interface
```go
type BroadcasterRepository interface {
    Create(ctx context.Context, broadcaster *Broadcaster) error
    GetByID(ctx context.Context, id uuid.UUID) (*Broadcaster, error)
    Update(ctx context.Context, broadcaster *Broadcaster) error
    Delete(ctx context.Context, id uuid.UUID) error
    List(ctx context.Context) ([]Broadcaster, error)
}
```

## Acceptance Criteria
- [ ] Entity struct with JSON tags (snake_case)
- [ ] Repository interface defined
- [ ] Documentation comments on exported types

## Phase
Foundational (Phase 2) - Can run in parallel with T010, T011
EOF
)"

gh issue create --repo "$REPO" --title "T010: Create streamkey.go domain entity" --label "$FEATURE_LABEL,phase:foundational,parallelizable" --body "$(cat <<'EOF'
## Task
Create internal/domain/streamkey.go with StreamKey entity, StreamKeyStatus enum, and StreamKeyRepository interface

## File
`internal/domain/streamkey.go`

## Entity and Enum
```go
type StreamKeyStatus string

const (
    StreamKeyStatusActive  StreamKeyStatus = "active"
    StreamKeyStatusRevoked StreamKeyStatus = "revoked"
    StreamKeyStatusExpired StreamKeyStatus = "expired"
)

type StreamKey struct {
    ID            uuid.UUID
    KeyValue      string
    BroadcasterID uuid.UUID
    Status        StreamKeyStatus
    CreatedAt     time.Time
    ExpiresAt     *time.Time
    RevokedAt     *time.Time
    LastUsedAt    *time.Time
}
```

## Repository Interface
Including GetAndLockByKeyValue for atomic auth operations

## Acceptance Criteria
- [ ] Entity struct with JSON tags (snake_case)
- [ ] Status enum defined
- [ ] Repository interface with locking method

## Phase
Foundational (Phase 2) - Can run in parallel with T009, T011
EOF
)"

gh issue create --repo "$REPO" --title "T011: Create stream.go domain entity" --label "$FEATURE_LABEL,phase:foundational,parallelizable" --body "$(cat <<'EOF'
## Task
Create internal/domain/stream.go with Stream entity, StreamStatus enum, StreamURLs type, and StreamRepository interface

## File
`internal/domain/stream.go`

## Types
```go
type StreamStatus string

const (
    StreamStatusActive StreamStatus = "active"
    StreamStatusEnded  StreamStatus = "ended"
)

type Stream struct {
    ID           uuid.UUID
    StreamKeyID  uuid.UUID
    Path         string
    Status       StreamStatus
    StartedAt    time.Time
    EndedAt      *time.Time
    SourceType   *string
    SourceID     *string
    Metadata     map[string]interface{}
    RecordingRef *string
}

type StreamURLs struct {
    HLS    string
    WebRTC string
}

type StreamWithURLs struct {
    Stream
    URLs StreamURLs
}
```

## Acceptance Criteria
- [ ] Entity struct with JSON tags (snake_case)
- [ ] Status enum defined
- [ ] StreamWithURLs for API responses
- [ ] Repository interface defined

## Phase
Foundational (Phase 2) - Can run in parallel with T009, T010
EOF
)"

gh issue create --repo "$REPO" --title "T012: Create errors.go with domain error types" --label "$FEATURE_LABEL,phase:foundational" --body "$(cat <<'EOF'
## Task
Create internal/domain/errors.go with domain error types (ErrNotFound, ErrAlreadyExists, ErrInvalidStatus)

## File
`internal/domain/errors.go`

## Errors
```go
var (
    ErrNotFound      = errors.New("not found")
    ErrAlreadyExists = errors.New("already exists")
    ErrInvalidStatus = errors.New("invalid status transition")
    ErrKeyInUse      = errors.New("stream key already in use")
    ErrKeyRevoked    = errors.New("stream key revoked")
    ErrKeyExpired    = errors.New("stream key expired")
)
```

## Acceptance Criteria
- [ ] All domain errors defined
- [ ] Errors can be wrapped with context
- [ ] Used consistently across domain layer

## Phase
Foundational (Phase 2)
EOF
)"

gh issue create --repo "$REPO" --title "T013: Create middleware.go with auth and logging middleware" --label "$FEATURE_LABEL,phase:foundational" --body "$(cat <<'EOF'
## Task
Create internal/handler/middleware.go with API key auth middleware, request ID middleware, and logging middleware

## File
`internal/handler/middleware.go`

## Middleware
1. **APIKeyAuth** - Validates X-API-Key header against configured secret
2. **RequestID** - Adds unique request ID to context and response header
3. **Logger** - Logs request/response with slog including request ID, duration, status

## Acceptance Criteria
- [ ] API key middleware returns 401 for missing/invalid keys
- [ ] Request ID added to context and X-Request-ID header
- [ ] Structured logging with slog
- [ ] Middleware can be composed

## Phase
Foundational (Phase 2)
EOF
)"

gh issue create --repo "$REPO" --title "T014: Create errors.go with RFC 9457 helpers" --label "$FEATURE_LABEL,phase:foundational" --body "$(cat <<'EOF'
## Task
Create internal/handler/errors.go with RFC 9457 error response helpers using alpineworks/rfc9457

## File
`internal/handler/errors.go`

## Requirements
- Use github.com/alpineworks/rfc9457 package
- Helper functions for common error types
- Consistent error type URIs

## Error Types
- `/errors/unauthorized`
- `/errors/not-found`
- `/errors/invalid-request`
- `/errors/invalid-stream-key`
- `/errors/stream-key-in-use`
- `/errors/stream-key-revoked`

## Acceptance Criteria
- [ ] RFC 9457 compliant error responses
- [ ] Helper functions for each error type
- [ ] Content-Type: application/problem+json
- [ ] Per Constitution Principle IV

## Phase
Foundational (Phase 2)
EOF
)"

gh issue create --repo "$REPO" --title "T015: Create server.go with HTTP server setup" --label "$FEATURE_LABEL,phase:foundational" --body "$(cat <<'EOF'
## Task
Create internal/server/server.go with HTTP server setup, route registration, and graceful shutdown

## File
`internal/server/server.go`

## Requirements
- Use net/http with Go 1.22+ routing
- Graceful shutdown on SIGINT/SIGTERM
- Route registration with middleware composition
- Health check endpoint

## Acceptance Criteria
- [ ] HTTP server with configurable port
- [ ] Graceful shutdown with timeout
- [ ] Route registration methods
- [ ] Middleware application

## Phase
Foundational (Phase 2)
EOF
)"

gh issue create --repo "$REPO" --title "T016: Create mediamtx.go client wrapper" --label "$FEATURE_LABEL,phase:foundational" --body "$(cat <<'EOF'
## Task
Create internal/service/mediamtx.go with MediaMTX client wrapper using functional options pattern

## File
`internal/service/mediamtx.go`

## Requirements
- Wrap alpineworks.io/gomediamtx client
- Functional options pattern per Constitution Principle II
- Methods for kicking connections (for key revocation)

## Acceptance Criteria
- [ ] Functional options for base URL, timeout, etc.
- [ ] Method to kick RTMP connection by ID
- [ ] Proper error handling

## Phase
Foundational (Phase 2)
EOF
)"

gh issue create --repo "$REPO" --title "T017: Create testcontainers PostgreSQL helper" --label "$FEATURE_LABEL,phase:foundational" --body "$(cat <<'EOF'
## Task
Create tests/integration/testutil/database.go with testcontainers PostgreSQL helper

## File
`tests/integration/testutil/database.go`

## Requirements
- Start PostgreSQL container
- Run migrations
- Provide connection string
- Cleanup after tests

## Acceptance Criteria
- [ ] PostgreSQL 15 container starts
- [ ] Migrations applied automatically
- [ ] Connection pooling for tests
- [ ] Container cleanup on test completion

## Phase
Foundational (Phase 2)
EOF
)"

# Phase 3: User Story 1
gh issue create --repo "$REPO" --title "T018: Create auth integration tests" --label "$FEATURE_LABEL,user-story:US1,parallelizable" --body "$(cat <<'EOF'
## Task
Create tests/integration/auth_test.go with test cases: valid key accepts, invalid key rejects, in-use key rejects, revoked key rejects, expired key rejects

## File
`tests/integration/auth_test.go`

## Test Cases
1. Valid, active key → 200 OK
2. Invalid/unknown key → 401 Unauthorized
3. Key already in use → 401 Unauthorized
4. Revoked key → 401 Unauthorized
5. Expired key → 401 Unauthorized

## Requirements
- Use testcontainers for PostgreSQL
- Test MediaMTX auth request format
- Verify stream record created on success

## Acceptance Criteria
- [ ] All test cases implemented
- [ ] Tests pass with real database
- [ ] Per Constitution Principle III

## User Story
US1 - Stream Key Authentication (P1)
EOF
)"

gh issue create --repo "$REPO" --title "T019: Create auth service unit tests" --label "$FEATURE_LABEL,user-story:US1,parallelizable" --body "$(cat <<'EOF'
## Task
Create tests/unit/service/auth_test.go with mock repository tests for AuthService

## File
`tests/unit/service/auth_test.go`

## Requirements
- Mock StreamKeyRepository
- Mock StreamRepository
- Test all validation logic paths
- Use testify assertions

## Acceptance Criteria
- [ ] Mock repositories created
- [ ] All auth logic paths tested
- [ ] Fast unit tests (no containers)

## User Story
US1 - Stream Key Authentication (P1)
EOF
)"

gh issue create --repo "$REPO" --title "T020: Implement BroadcasterRepository with pgxpool" --label "$FEATURE_LABEL,user-story:US1" --body "$(cat <<'EOF'
## Task
Create internal/database/broadcaster_repo.go implementing BroadcasterRepository with pgxpool

## File
`internal/database/broadcaster_repo.go`

## Methods
- Create
- GetByID
- Update
- Delete
- List

## Requirements
- Use prepared statements
- Proper error mapping to domain errors
- Context propagation

## Acceptance Criteria
- [ ] All repository methods implemented
- [ ] Domain errors returned appropriately
- [ ] Tested via integration tests

## User Story
US1 - Stream Key Authentication (P1)
EOF
)"

gh issue create --repo "$REPO" --title "T021: Implement StreamKeyRepository with pgxpool" --label "$FEATURE_LABEL,user-story:US1" --body "$(cat <<'EOF'
## Task
Create internal/database/streamkey_repo.go implementing StreamKeyRepository with pgxpool, including GetAndLockByKeyValue with SELECT FOR UPDATE

## File
`internal/database/streamkey_repo.go`

## Methods
- Create
- GetByID
- GetByKeyValue
- GetAndLockByKeyValue (SELECT FOR UPDATE)
- ListByBroadcaster
- ListAll
- UpdateStatus
- UpdateLastUsed

## Critical: GetAndLockByKeyValue
Must use SELECT FOR UPDATE to prevent race conditions in auth

## Acceptance Criteria
- [ ] All repository methods implemented
- [ ] Row-level locking for auth
- [ ] Domain errors returned appropriately

## User Story
US1 - Stream Key Authentication (P1)
EOF
)"

gh issue create --repo "$REPO" --title "T022: Implement StreamRepository with pgxpool" --label "$FEATURE_LABEL,user-story:US1" --body "$(cat <<'EOF'
## Task
Create internal/database/stream_repo.go implementing StreamRepository with pgxpool

## File
`internal/database/stream_repo.go`

## Methods
- Create
- GetByID
- GetActiveByPath
- GetActiveByStreamKeyID
- ListActive
- EndStream
- EndStreamByPath

## Acceptance Criteria
- [ ] All repository methods implemented
- [ ] Active stream queries optimized
- [ ] Domain errors returned appropriately

## User Story
US1 - Stream Key Authentication (P1)
EOF
)"

gh issue create --repo "$REPO" --title "T023: Create AuthService with authentication logic" --label "$FEATURE_LABEL,user-story:US1" --body "$(cat <<'EOF'
## Task
Create internal/service/auth.go with AuthService.Authenticate() handling all validation rules (valid, not expired, not revoked, not in-use)

## File
`internal/service/auth.go`

## Method: Authenticate
1. Look up key by value (with lock)
2. Check status is active
3. Check not expired
4. Check not already in use
5. Create stream record
6. Update last_used_at

## Requirements
- Transaction for atomicity
- Return appropriate domain errors
- <50ms response time

## Acceptance Criteria
- [ ] All validation rules implemented
- [ ] Atomic operation with transaction
- [ ] Proper error types returned

## User Story
US1 - Stream Key Authentication (P1)
EOF
)"

gh issue create --repo "$REPO" --title "T024: Create auth handler for POST /auth" --label "$FEATURE_LABEL,user-story:US1" --body "$(cat <<'EOF'
## Task
Create internal/handler/auth.go with POST /auth handler matching MediaMTX auth request format (user, password, ip, action, path, protocol, id, query)

## File
`internal/handler/auth.go`

## Request Format
```json
{
  "user": "",
  "password": "stream-key-value",
  "ip": "192.168.1.100",
  "action": "publish",
  "path": "stream-name",
  "protocol": "rtmp",
  "id": "connection-id",
  "query": ""
}
```

## Response
- 200 OK for success
- 401 Unauthorized for failure (RFC 9457 format)

## Acceptance Criteria
- [ ] MediaMTX request format parsed
- [ ] Stream key from password field
- [ ] RFC 9457 error responses

## User Story
US1 - Stream Key Authentication (P1)
EOF
)"

gh issue create --repo "$REPO" --title "T025: Create webhook handlers for stream lifecycle" --label "$FEATURE_LABEL,user-story:US1" --body "$(cat <<'EOF'
## Task
Create internal/handler/webhook.go with POST /webhook/ready and POST /webhook/not-ready handlers for stream lifecycle

## File
`internal/handler/webhook.go`

## Endpoints
1. POST /webhook/ready - Stream became available
   - Update stream with source_type, source_id
2. POST /webhook/not-ready - Stream ended
   - Mark stream as ended

## Request Format
```json
{
  "path": "stream-path",
  "source_type": "rtmpConn",
  "source_id": "abc123"
}
```

## Acceptance Criteria
- [ ] Both endpoints implemented
- [ ] Stream state updated correctly
- [ ] 204 No Content on success

## User Story
US1 - Stream Key Authentication (P1)
EOF
)"

gh issue create --repo "$REPO" --title "T026: Register auth and webhook routes" --label "$FEATURE_LABEL,user-story:US1" --body "$(cat <<'EOF'
## Task
Register /auth and /webhook/* routes in internal/server/server.go (no API key required)

## File
`internal/server/server.go`

## Routes
- POST /auth - No auth middleware (called by MediaMTX)
- POST /webhook/ready - No auth middleware (called by MediaMTX)
- POST /webhook/not-ready - No auth middleware (called by MediaMTX)

## Acceptance Criteria
- [ ] Routes registered
- [ ] No API key middleware on these routes
- [ ] Request ID and logging middleware applied

## User Story
US1 - Stream Key Authentication (P1)
EOF
)"

gh issue create --repo "$REPO" --title "T027: Update main.go with database and server initialization" --label "$FEATURE_LABEL,user-story:US1" --body "$(cat <<'EOF'
## Task
Update cmd/rescuestream-api/main.go to initialize database, run migrations, and start HTTP server

## File
`cmd/rescuestream-api/main.go`

## Initialization Order
1. Load config
2. Setup logging/telemetry (existing)
3. Connect to database
4. Run migrations
5. Create repositories
6. Create services
7. Create handlers
8. Start HTTP server
9. Wait for shutdown signal
10. Graceful shutdown

## Acceptance Criteria
- [ ] Database connection established
- [ ] Migrations run on startup
- [ ] HTTP server starts
- [ ] Graceful shutdown works

## User Story
US1 - Stream Key Authentication (P1)
EOF
)"

# Phase 4: User Story 2
gh issue create --repo "$REPO" --title "T028: Create stream listing integration tests" --label "$FEATURE_LABEL,user-story:US2,parallelizable" --body "$(cat <<'EOF'
## Task
Create tests/integration/stream_test.go with test cases: list returns active streams with URLs, list returns empty when no streams, inactive streams excluded

## File
`tests/integration/stream_test.go`

## Test Cases
1. List with active streams → returns streams with URLs
2. List with no streams → returns empty array
3. List excludes ended streams
4. URLs correctly constructed from config

## Acceptance Criteria
- [ ] All test cases implemented
- [ ] Tests use testcontainers
- [ ] URL construction verified

## User Story
US2 - Active Stream Listing (P2)
EOF
)"

gh issue create --repo "$REPO" --title "T029: Create stream service unit tests" --label "$FEATURE_LABEL,user-story:US2,parallelizable" --body "$(cat <<'EOF'
## Task
Create tests/unit/service/stream_test.go with mock repository tests for StreamService

## File
`tests/unit/service/stream_test.go`

## Requirements
- Mock StreamRepository
- Test URL construction logic
- Use testify assertions

## Acceptance Criteria
- [ ] Mock repository tests
- [ ] URL building tested
- [ ] Fast unit tests

## User Story
US2 - Active Stream Listing (P2)
EOF
)"

gh issue create --repo "$REPO" --title "T030: Create StreamService with ListActive" --label "$FEATURE_LABEL,user-story:US2" --body "$(cat <<'EOF'
## Task
Create internal/service/stream.go with StreamService.ListActive() returning StreamWithURLs including HLS/WebRTC URLs constructed from MEDIAMTX_PUBLIC_URL

## File
`internal/service/stream.go`

## Method: ListActive
1. Fetch active streams from repository
2. For each stream, construct URLs:
   - HLS: `{MEDIAMTX_PUBLIC_URL}/{path}/index.m3u8`
   - WebRTC: `{MEDIAMTX_PUBLIC_URL}/{path}/whep`
3. Return StreamWithURLs slice

## Acceptance Criteria
- [ ] ListActive returns streams with URLs
- [ ] URLs constructed correctly
- [ ] <50ms response time

## User Story
US2 - Active Stream Listing (P2)
EOF
)"

gh issue create --repo "$REPO" --title "T031: Create stream handler for GET /streams" --label "$FEATURE_LABEL,user-story:US2" --body "$(cat <<'EOF'
## Task
Create internal/handler/stream.go with GET /streams handler returning StreamList response

## File
`internal/handler/stream.go`

## Response Format
```json
{
  "streams": [...],
  "count": 5
}
```

## Requirements
- JSON response with snake_case
- Include count field
- RFC 9457 errors

## Acceptance Criteria
- [ ] GET /streams handler implemented
- [ ] StreamList response format
- [ ] Error handling

## User Story
US2 - Active Stream Listing (P2)
EOF
)"

gh issue create --repo "$REPO" --title "T032: Register /streams route with API key middleware" --label "$FEATURE_LABEL,user-story:US2" --body "$(cat <<'EOF'
## Task
Register /streams route in internal/server/server.go with API key middleware

## File
`internal/server/server.go`

## Route
- GET /streams - Requires API key

## Acceptance Criteria
- [ ] Route registered
- [ ] API key middleware applied
- [ ] 401 returned without valid key

## User Story
US2 - Active Stream Listing (P2)
EOF
)"

# Phase 5: User Story 3
gh issue create --repo "$REPO" --title "T033: Create stream key management integration tests" --label "$FEATURE_LABEL,user-story:US3,parallelizable" --body "$(cat <<'EOF'
## Task
Create tests/integration/streamkey_test.go with test cases: create returns key_value, list omits key_value, get by ID works, revoke invalidates key, revoke terminates active stream

## File
`tests/integration/streamkey_test.go`

## Test Cases
1. Create → returns full key including key_value
2. List → omits key_value for security
3. Get by ID → returns key details (no key_value)
4. Revoke → key becomes invalid for auth
5. Revoke with active stream → stream terminated

## Acceptance Criteria
- [ ] All test cases implemented
- [ ] Security of key_value verified
- [ ] Revocation flow tested

## User Story
US3 - Stream Key Management (P3)
EOF
)"

gh issue create --repo "$REPO" --title "T034: Create stream key service unit tests" --label "$FEATURE_LABEL,user-story:US3,parallelizable" --body "$(cat <<'EOF'
## Task
Create tests/unit/service/streamkey_test.go with mock repository tests for StreamKeyService

## File
`tests/unit/service/streamkey_test.go`

## Requirements
- Mock repositories
- Test key generation
- Test revocation logic
- Use testify assertions

## Acceptance Criteria
- [ ] Key generation tested
- [ ] Revocation logic tested
- [ ] Fast unit tests

## User Story
US3 - Stream Key Management (P3)
EOF
)"

gh issue create --repo "$REPO" --title "T035: Create StreamKeyService with CRUD operations" --label "$FEATURE_LABEL,user-story:US3" --body "$(cat <<'EOF'
## Task
Create internal/service/streamkey.go with StreamKeyService: Create() generates secure key with crypto/rand, List(), GetByID(), Revoke() terminates active streams

## File
`internal/service/streamkey.go`

## Methods
1. **Create** - Generate secure key (sk_ + 43 char base64url)
2. **List** - Return all keys (without key_value)
3. **GetByID** - Return single key (without key_value)
4. **Revoke** - Mark revoked, terminate active stream, kick MediaMTX connection

## Key Generation
```go
// Generate 32 bytes of random data
// Base64url encode
// Prefix with "sk_"
```

## Acceptance Criteria
- [ ] Secure key generation with crypto/rand
- [ ] Revocation terminates streams
- [ ] MediaMTX connection kicked on revoke

## User Story
US3 - Stream Key Management (P3)
EOF
)"

gh issue create --repo "$REPO" --title "T036: Create stream key handler with CRUD endpoints" --label "$FEATURE_LABEL,user-story:US3" --body "$(cat <<'EOF'
## Task
Create internal/handler/streamkey.go with POST /stream-keys, GET /stream-keys, GET /stream-keys/{id}, DELETE /stream-keys/{id} handlers

## File
`internal/handler/streamkey.go`

## Endpoints
1. POST /stream-keys - Create new key (returns key_value)
2. GET /stream-keys - List all keys (no key_value)
3. GET /stream-keys/{id} - Get key details (no key_value)
4. DELETE /stream-keys/{id} - Revoke key

## Acceptance Criteria
- [ ] All CRUD endpoints implemented
- [ ] key_value only in create response
- [ ] RFC 9457 errors

## User Story
US3 - Stream Key Management (P3)
EOF
)"

gh issue create --repo "$REPO" --title "T037: Register /stream-keys routes with API key middleware" --label "$FEATURE_LABEL,user-story:US3" --body "$(cat <<'EOF'
## Task
Register /stream-keys/* routes in internal/server/server.go with API key middleware

## File
`internal/server/server.go`

## Routes
- POST /stream-keys
- GET /stream-keys
- GET /stream-keys/{id}
- DELETE /stream-keys/{id}

All require API key authentication.

## Acceptance Criteria
- [ ] All routes registered
- [ ] API key middleware applied
- [ ] Path parameter extraction works

## User Story
US3 - Stream Key Management (P3)
EOF
)"

gh issue create --repo "$REPO" --title "T038: Create broadcaster handler with full CRUD" --label "$FEATURE_LABEL,user-story:US3" --body "$(cat <<'EOF'
## Task
Create internal/handler/broadcaster.go with full CRUD: POST /broadcasters, GET /broadcasters, GET /broadcasters/{id}, PATCH /broadcasters/{id}, DELETE /broadcasters/{id}

## File
`internal/handler/broadcaster.go`

## Endpoints
1. POST /broadcasters - Create broadcaster
2. GET /broadcasters - List broadcasters
3. GET /broadcasters/{id} - Get broadcaster
4. PATCH /broadcasters/{id} - Update broadcaster
5. DELETE /broadcasters/{id} - Delete broadcaster (cascades to keys)

## Acceptance Criteria
- [ ] All CRUD endpoints implemented
- [ ] Proper request validation
- [ ] RFC 9457 errors

## User Story
US3 - Stream Key Management (P3)
EOF
)"

gh issue create --repo "$REPO" --title "T039: Register /broadcasters routes with API key middleware" --label "$FEATURE_LABEL,user-story:US3" --body "$(cat <<'EOF'
## Task
Register /broadcasters/* routes in internal/server/server.go with API key middleware

## File
`internal/server/server.go`

## Routes
- POST /broadcasters
- GET /broadcasters
- GET /broadcasters/{id}
- PATCH /broadcasters/{id}
- DELETE /broadcasters/{id}

All require API key authentication.

## Acceptance Criteria
- [ ] All routes registered
- [ ] API key middleware applied

## User Story
US3 - Stream Key Management (P3)
EOF
)"

# Phase 6: User Story 4
gh issue create --repo "$REPO" --title "T040: Add stream detail test cases" --label "$FEATURE_LABEL,user-story:US4,parallelizable" --body "$(cat <<'EOF'
## Task
Add test cases to tests/integration/stream_test.go: get stream by ID returns full details, get non-existent stream returns 404

## File
`tests/integration/stream_test.go`

## Test Cases
1. Get existing stream → returns full details with URLs
2. Get non-existent stream → 404 Not Found (RFC 9457)

## Acceptance Criteria
- [ ] Test cases added to existing file
- [ ] Full stream details verified
- [ ] 404 error format verified

## User Story
US4 - Individual Stream Details (P4)
EOF
)"

SKIP_ALREADY_CREATED

# ============================================================
# Continue from T041 onwards (issues 42+)
# ============================================================

gh issue create --repo "$REPO" --title "T041: Add StreamService.GetByID method" --label "$FEATURE_LABEL,user-story:US4" --body "$(cat <<'EOF'
## Task
Add StreamService.GetByID() to internal/service/stream.go returning full stream details with URLs

## File
`internal/service/stream.go`

## Method: GetByID
1. Fetch stream by ID from repository
2. Return ErrNotFound if not exists
3. Construct URLs (same as ListActive)
4. Return StreamWithURLs

## Acceptance Criteria
- [ ] GetByID method implemented
- [ ] URLs included in response
- [ ] Proper error handling

## User Story
US4 - Individual Stream Details (P4)
EOF
)"

gh issue create --repo "$REPO" --title "T042: Add GET /streams/{id} handler" --label "$FEATURE_LABEL,user-story:US4" --body "$(cat <<'EOF'
## Task
Add GET /streams/{id} handler to internal/handler/stream.go

## File
`internal/handler/stream.go`

## Endpoint
GET /streams/{id} - Returns single stream with URLs

## Response
Full Stream object with URLs

## Errors
- 404 Not Found if stream does not exist

## Acceptance Criteria
- [ ] Handler implemented
- [ ] Path parameter extracted
- [ ] RFC 9457 404 error

## User Story
US4 - Individual Stream Details (P4)
EOF
)"

gh issue create --repo "$REPO" --title "T043: Register /streams/{id} route" --label "$FEATURE_LABEL,user-story:US4" --body "$(cat <<'EOF'
## Task
Register /streams/{id} route in internal/server/server.go with API key middleware

## File
`internal/server/server.go`

## Route
- GET /streams/{id} - Requires API key

## Acceptance Criteria
- [ ] Route registered
- [ ] Path parameter works
- [ ] API key required

## User Story
US4 - Individual Stream Details (P4)
EOF
)"

# Phase 7: Polish
gh issue create --repo "$REPO" --title "T044: Add health check endpoint" --label "$FEATURE_LABEL,phase:polish,parallelizable" --body "$(cat <<'EOF'
## Task
Add health check endpoint GET /health in internal/handler/health.go

## File
`internal/handler/health.go`

## Endpoint
GET /health - No auth required

## Response
```json
{
  "status": "ok"
}
```

## Optional enhancements
- Check database connectivity
- Return degraded status if issues

## Acceptance Criteria
- [ ] Health endpoint implemented
- [ ] No auth required
- [ ] Returns 200 OK

## Phase
Polish (Phase 7)
EOF
)"

gh issue create --repo "$REPO" --title "T045: Add request logging to all handlers" --label "$FEATURE_LABEL,phase:polish,parallelizable" --body "$(cat <<'EOF'
## Task
Add request logging with slog in all handlers including request ID, duration, status

## Requirements
- Log on request completion
- Include: method, path, status, duration, request_id
- Use structured logging with slog

## Example log
```json
{
  "level": "INFO",
  "msg": "request completed",
  "method": "GET",
  "path": "/streams",
  "status": 200,
  "duration_ms": 12,
  "request_id": "abc123"
}
```

## Acceptance Criteria
- [ ] All requests logged
- [ ] Structured format
- [ ] Request ID included

## Phase
Polish (Phase 7)
EOF
)"

gh issue create --repo "$REPO" --title "T046: Run linting and fix issues" --label "$FEATURE_LABEL,phase:polish" --body "$(cat <<'EOF'
## Task
Run go vet and golangci-lint, fix any issues

## Commands
```bash
go vet ./...
golangci-lint run
```

## Requirements
- All issues fixed
- No linting errors
- Constitution Principle I compliance

## Acceptance Criteria
- [ ] go vet passes
- [ ] golangci-lint passes
- [ ] No suppressions added

## Phase
Polish (Phase 7)
EOF
)"

gh issue create --repo "$REPO" --title "T047: Run tests and verify coverage" --label "$FEATURE_LABEL,phase:polish" --body "$(cat <<'EOF'
## Task
Run all tests and verify >60% coverage

## Commands
```bash
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

## Requirements
- All tests pass
- Coverage > 60% overall
- Coverage > 80% for business logic (services)

## Acceptance Criteria
- [ ] All tests pass
- [ ] Coverage threshold met
- [ ] Per Constitution Principle III

## Phase
Polish (Phase 7)
EOF
)"

gh issue create --repo "$REPO" --title "T048: Test quickstart.md flow end-to-end" --label "$FEATURE_LABEL,phase:polish" --body "$(cat <<'EOF'
## Task
Test quickstart.md flow end-to-end with docker-compose

## Steps
1. Start docker-compose stack
2. Create broadcaster via API
3. Create stream key
4. Start stream with ffmpeg/OBS
5. Verify auth succeeds
6. Verify stream appears in list
7. View stream via HLS
8. Revoke key
9. Verify stream terminates

## Acceptance Criteria
- [ ] Full flow works
- [ ] quickstart.md accurate
- [ ] No manual fixes needed

## Phase
Polish (Phase 7)
EOF
)"

gh issue create --repo "$REPO" --title "T049: Update docker-compose.yml with full stack" --label "$FEATURE_LABEL,phase:polish,parallelizable" --body "$(cat <<'EOF'
## Task
Update docker-compose.yml with postgres, mediamtx, and api services

## File
`docker-compose.yml`

## Services
1. **postgres** - PostgreSQL 15
2. **mediamtx** - bluenviron/mediamtx with custom config
3. **api** - rescuestream-api built from Dockerfile

## Requirements
- Health checks
- Proper networking
- Volume for postgres data
- Environment variables

## Acceptance Criteria
- [ ] All services defined
- [ ] Services start correctly
- [ ] API connects to postgres and mediamtx

## Phase
Polish (Phase 7)
EOF
)"

gh issue create --repo "$REPO" --title "T050: Create MediaMTX configuration file" --label "$FEATURE_LABEL,phase:polish,parallelizable" --body "$(cat <<'EOF'
## Task
Create docker/mediamtx/mediamtx.yml with auth and webhook configuration pointing to API

## File
`docker/mediamtx/mediamtx.yml`

## Configuration
See quickstart.md for full YAML configuration including:
- authMethod: http
- authHTTPAddress pointing to API /auth endpoint
- authHTTPExclude for read/playback actions
- runOnReady webhook with path, source_type, source_id
- runOnNotReady webhook with path

## Acceptance Criteria
- [ ] Auth configured to call API
- [ ] Webhooks configured
- [ ] Read actions excluded from auth

## Phase
Polish (Phase 7)
EOF
)"

echo ""
echo "✅ All 50 issues created successfully!"
echo ""
echo "View issues at: https://github.com/$REPO/issues"
