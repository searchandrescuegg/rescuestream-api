# Tasks: Stream Orchestration API

**Input**: Design documents from `/specs/001-stream-orchestration/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Constitution requires comprehensive testing (testcontainers, httptest, testify). Tests are included for all user stories.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Go project**: `cmd/`, `internal/`, `tests/` at repository root
- Paths follow Go standard project layout per plan.md

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and dependency setup

- [x] T001 Add new dependencies to go.mod: pgxpool, otelpgx, rfc9457, gomediamtx, testcontainers-go, testify
- [x] T002 [P] Create directory structure per plan.md: internal/database/, internal/domain/, internal/service/, internal/handler/, internal/server/, tests/integration/, tests/unit/
- [x] T003 [P] Configure golangci-lint with .golangci.yml for Go standards compliance
- [x] T004 [P] Create Makefile with targets: build, test, lint, migrate, run

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**CRITICAL**: No user story work can begin until this phase is complete

- [x] T005 Extend internal/config/config.go with new env vars: DATABASE_URL, API_SECRET, MEDIAMTX_API_URL, MEDIAMTX_PUBLIC_URL, API_PORT
- [x] T006 Create internal/database/database.go with pgxpool client using functional options pattern
- [x] T007 Create internal/database/migrations/001_initial_schema.sql with broadcasters, stream_keys, streams tables from data-model.md
- [x] T008 Create internal/database/migrate.go with migration runner using embed for SQL files
- [x] T009 [P] Create internal/domain/broadcaster.go with Broadcaster entity and BroadcasterRepository interface
- [x] T010 [P] Create internal/domain/streamkey.go with StreamKey entity, StreamKeyStatus enum, and StreamKeyRepository interface
- [x] T011 [P] Create internal/domain/stream.go with Stream entity, StreamStatus enum, StreamURLs type, and StreamRepository interface
- [x] T012 Create internal/domain/errors.go with domain error types (ErrNotFound, ErrAlreadyExists, ErrInvalidStatus)
- [x] T013 Create internal/handler/middleware.go with API key auth middleware, request ID middleware, and logging middleware
- [x] T014 Create internal/handler/errors.go with RFC 9457 error response helpers using alpineworks/rfc9457
- [x] T015 Create internal/server/server.go with HTTP server setup, route registration, and graceful shutdown
- [x] T016 Create internal/service/mediamtx.go with MediaMTX client wrapper using functional options pattern
- [x] T017 Create tests/integration/testutil/database.go with testcontainers PostgreSQL helper

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Stream Key Authentication (Priority: P1) MVP

**Goal**: MediaMTX can authenticate stream keys via POST /auth endpoint

**Independent Test**: Send auth requests with valid/invalid/in-use/revoked keys and verify correct accept/reject responses

### Tests for User Story 1

- [ ] T018 [P] [US1] Create tests/integration/auth_test.go with test cases: valid key accepts, invalid key rejects, in-use key rejects, revoked key rejects, expired key rejects
- [ ] T019 [P] [US1] Create tests/unit/service/auth_test.go with mock repository tests for AuthService

### Implementation for User Story 1

- [x] T020 [US1] Create internal/database/broadcaster_repo.go implementing BroadcasterRepository with pgxpool
- [x] T021 [US1] Create internal/database/streamkey_repo.go implementing StreamKeyRepository with pgxpool, including GetAndLockByKeyValue with SELECT FOR UPDATE
- [x] T022 [US1] Create internal/database/stream_repo.go implementing StreamRepository with pgxpool
- [x] T023 [US1] Create internal/service/auth.go with AuthService.Authenticate() handling all validation rules (valid, not expired, not revoked, not in-use)
- [x] T024 [US1] Create internal/handler/auth.go with POST /auth handler matching MediaMTX auth request format (user, password, ip, action, path, protocol, id, query)
- [x] T025 [US1] Create internal/handler/webhook.go with POST /webhook/ready and POST /webhook/not-ready handlers for stream lifecycle
- [x] T026 [US1] Register /auth and /webhook/* routes in internal/server/server.go (no API key required)
- [x] T027 [US1] Update cmd/rescuestream-api/main.go to initialize database, run migrations, and start HTTP server

**Checkpoint**: Stream key authentication is functional. MediaMTX can validate keys. Streams are tracked via webhooks.

---

## Phase 4: User Story 2 - Active Stream Listing (Priority: P2)

**Goal**: Frontend can list all active streams with video playback URLs

**Independent Test**: Query GET /streams and verify response contains active streams with HLS/WebRTC URLs

### Tests for User Story 2

- [ ] T028 [P] [US2] Create tests/integration/stream_test.go with test cases: list returns active streams with URLs, list returns empty when no streams, inactive streams excluded
- [ ] T029 [P] [US2] Create tests/unit/service/stream_test.go with mock repository tests for StreamService

### Implementation for User Story 2

- [x] T030 [US2] Create internal/service/stream.go with StreamService.ListActive() returning StreamWithURLs including HLS/WebRTC URLs constructed from MEDIAMTX_PUBLIC_URL
- [x] T031 [US2] Create internal/handler/stream.go with GET /streams handler returning StreamList response
- [x] T032 [US2] Register /streams route in internal/server/server.go with API key middleware

**Checkpoint**: Frontend can discover and view active streams

---

## Phase 5: User Story 3 - Stream Key Management (Priority: P3)

**Goal**: Administrators can create, list, and revoke stream keys

**Independent Test**: Create a stream key, list keys to verify it appears, revoke it, verify auth fails for revoked key

### Tests for User Story 3

- [ ] T033 [P] [US3] Create tests/integration/streamkey_test.go with test cases: create returns key_value, list omits key_value, get by ID works, revoke invalidates key, revoke terminates active stream
- [ ] T034 [P] [US3] Create tests/unit/service/streamkey_test.go with mock repository tests for StreamKeyService

### Implementation for User Story 3

- [x] T035 [US3] Create internal/service/streamkey.go with StreamKeyService: Create() generates secure key with crypto/rand, List(), GetByID(), Revoke() terminates active streams
- [x] T036 [US3] Create internal/handler/streamkey.go with POST /stream-keys, GET /stream-keys, GET /stream-keys/{id}, DELETE /stream-keys/{id} handlers
- [x] T037 [US3] Register /stream-keys/* routes in internal/server/server.go with API key middleware
- [x] T038 [US3] Create internal/handler/broadcaster.go with full CRUD: POST /broadcasters, GET /broadcasters, GET /broadcasters/{id}, PATCH /broadcasters/{id}, DELETE /broadcasters/{id}
- [x] T039 [US3] Register /broadcasters/* routes in internal/server/server.go with API key middleware

**Checkpoint**: Full stream key lifecycle management available

---

## Phase 6: User Story 4 - Individual Stream Details (Priority: P4)

**Goal**: Frontend can get detailed info about a specific stream

**Independent Test**: Request GET /streams/{id} for an active stream and verify all fields including multiple video URLs

### Tests for User Story 4

- [ ] T040 [P] [US4] Add test cases to tests/integration/stream_test.go: get stream by ID returns full details, get non-existent stream returns 404

### Implementation for User Story 4

- [x] T041 [US4] Add StreamService.GetByID() to internal/service/stream.go returning full stream details with URLs
- [x] T042 [US4] Add GET /streams/{id} handler to internal/handler/stream.go
- [x] T043 [US4] Register /streams/{id} route in internal/server/server.go with API key middleware

**Checkpoint**: All user stories complete

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [x] T044 [P] Add health check endpoint GET /health in internal/handler/health.go
- [x] T045 [P] Add request logging with slog in all handlers including request ID, duration, status
- [ ] T046 Run go vet and golangci-lint, fix any issues
- [ ] T047 Run all tests and verify >60% coverage
- [ ] T048 Test quickstart.md flow end-to-end with docker-compose
- [ ] T049 [P] Update docker-compose.yml with postgres, mediamtx, and api services
- [ ] T050 [P] Create docker/mediamtx/mediamtx.yml with auth and webhook configuration pointing to API

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3-6)**: All depend on Foundational phase completion
  - US1 (Auth) has no dependencies on other stories
  - US2 (Stream List) depends on US1 for stream data via webhooks
  - US3 (Key Management) can run in parallel with US2
  - US4 (Stream Details) depends on US2 stream service
- **Polish (Phase 7)**: Depends on all user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational - Creates auth flow and stream tracking
- **User Story 2 (P2)**: Needs US1 webhook handling to have stream data to list
- **User Story 3 (P3)**: Can start after Foundational - Independent key management
- **User Story 4 (P4)**: Extends US2 stream service with detail endpoint

### Within Each User Story

- Tests written first to define expected behavior
- Repository implementations before services
- Services before handlers
- Handlers before route registration

### Parallel Opportunities

- T002, T003, T004 can run in parallel (different files)
- T009, T010, T011 can run in parallel (different domain files)
- T018, T019 can run in parallel (different test files)
- T028, T029 can run in parallel (different test files)
- T033, T034 can run in parallel (different test files)
- T044, T045, T049, T050 can run in parallel (different files)

---

## Parallel Example: Foundational Phase

```bash
# After T005-T008 complete (database setup), launch domain entities in parallel:
Task: "T009 Create internal/domain/broadcaster.go"
Task: "T010 Create internal/domain/streamkey.go"
Task: "T011 Create internal/domain/stream.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational
3. Complete Phase 3: User Story 1 (Stream Key Authentication)
4. **STOP and VALIDATE**: Test auth endpoint with curl, verify MediaMTX integration
5. Deploy MVP if auth-only functionality is valuable

### Incremental Delivery

1. Setup + Foundational → Database and infrastructure ready
2. US1 (Auth) → MediaMTX can authenticate streams (MVP!)
3. US2 (Stream List) → Frontend can see active streams
4. US3 (Key Management) → Admins can manage access
5. US4 (Stream Details) → Enhanced stream info
6. Polish → Production-ready

### Suggested MVP Scope

**User Story 1 only** - Stream Key Authentication enables:
- MediaMTX to validate broadcaster credentials
- Stream lifecycle tracking via webhooks
- Core security gate functional

This delivers the foundational security value while subsequent stories add frontend/admin features.

---

## Notes

- All handlers must return RFC 9457 errors using internal/handler/errors.go
- All database clients must use functional options pattern
- All tests must use testcontainers for PostgreSQL
- JSON field naming must use snake_case
- Response times must be <50ms for auth and list endpoints
- Constitution compliance verified via golangci-lint and test coverage
