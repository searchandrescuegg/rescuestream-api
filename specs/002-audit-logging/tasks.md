# Tasks: Audit Logging

**Input**: Design documents from `/specs/002-audit-logging/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/

**Tests**: Included per constitution requirement (Principle III: Comprehensive Testing)

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story?] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3, US4)
- Include exact file paths in descriptions

## Path Conventions

Based on plan.md structure, this is a single Go API service:
- Source: `internal/` at repository root
- Tests: `internal/*/` with `_test.go` suffix
- Migrations: `internal/database/migrations/`

---

## Phase 1: Setup

**Purpose**: Create new files and migration structure for audit logging

- [X] T001 Create database migration file `internal/database/migrations/000002_add_audit_logs.up.sql` with api_keys and audit_logs tables
- [X] T002 [P] Create database rollback migration `internal/database/migrations/000002_add_audit_logs.down.sql`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T003 Create AuditLogEntry domain model and AuditLogFilter in `internal/domain/auditlog.go`
- [X] T004 [P] Add ErrAdminRequired and ErrForbidden domain errors to `internal/domain/errors.go`
- [X] T005 [P] Create APIKey domain model and APIKeyRepository interface in `internal/domain/apikey.go`
- [X] T006 Implement AuditLogRepo with Create and List methods in `internal/database/auditlog_repo.go`
- [X] T007 [P] Implement APIKeyRepo with GetByIdentifier and IsAdmin methods in `internal/database/apikey_repo.go`
- [X] T008 Create AuditLogService with functional options pattern in `internal/service/auditlog.go`
- [X] T009 [P] Create APIKeyService for admin checking in `internal/service/apikey.go`
- [X] T010 Add ErrForbidden HTTP error helper to `internal/handler/errors.go`
- [X] T011 Add getClientIP helper function to `internal/handler/middleware.go`

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 2 - System Records All API Actions (Priority: P1) 🎯 MVP

**Goal**: Automatically record all authenticated mutating API actions to provide a complete audit trail

**Independent Test**: Perform API operations (create broadcaster, revoke stream key) and verify corresponding audit entries exist in database

**Why US2 before US1**: Recording must exist before viewing makes sense. The middleware creates entries that US1 will display.

### Tests for User Story 2

- [X] T012 [P] [US2] Create integration test for audit middleware in `internal/handler/audit_middleware_test.go`
- [X] T013 [P] [US2] Create unit test for AuditLogRepo.Create in `internal/database/auditlog_repo_test.go`

### Implementation for User Story 2

- [X] T014 [US2] Add AuditLogService.CreateEntry method for middleware use in `internal/service/auditlog.go`
- [X] T015 [US2] Implement AuditMiddleware that captures method, path, status, and IP in `internal/handler/middleware.go`
- [X] T016 [US2] Add sanitizePath function to filter sensitive data from paths in `internal/handler/middleware.go`
- [X] T017 [US2] Add extractResourceInfo function to parse resource type/ID from paths in `internal/handler/middleware.go`
- [X] T018 [US2] Add methodToAction and statusToOutcome helper functions in `internal/handler/middleware.go`
- [X] T019 [US2] Register AuditMiddleware on protected routes in `internal/server/server.go`
- [X] T020 [US2] Wire AuditLogRepo and AuditLogService in server initialization in `internal/server/server.go`

**Checkpoint**: API actions are now being recorded. Verify by checking database after API calls.

---

## Phase 4: User Story 1 - Admin Reviews Recent Activity (Priority: P1)

**Goal**: Administrators can retrieve audit log entries through GET /audit-logs endpoint

**Independent Test**: Authenticate as admin, call GET /audit-logs, verify JSON response with recent actions sorted by timestamp desc

### Tests for User Story 1

- [X] T021 [P] [US1] Create handler test for GET /audit-logs in `internal/handler/auditlog_test.go`
- [X] T022 [P] [US1] Create test for admin-only access enforcement in `internal/handler/auditlog_test.go`
- [X] T023 [P] [US1] Create unit test for AuditLogRepo.List in `internal/database/auditlog_repo_test.go`

### Implementation for User Story 1

- [X] T024 [US1] Create AuditLogHandler struct with dependencies in `internal/handler/auditlog.go`
- [X] T025 [US1] Implement ServeHTTP routing for audit endpoints in `internal/handler/auditlog.go`
- [X] T026 [US1] Implement listAuditLogs handler method in `internal/handler/auditlog.go`
- [X] T027 [US1] Add admin authorization check using APIKeyService in `internal/handler/auditlog.go`
- [X] T028 [US1] Implement AuditLogService.List method in `internal/service/auditlog.go`
- [X] T029 [US1] Create AuditLogListResponse and PaginationResponse types in `internal/handler/auditlog.go`
- [X] T030 [US1] Register GET /audit-logs route in `internal/server/server.go`
- [X] T031 [US1] Wire APIKeyService for admin checking in `internal/server/server.go`

**Checkpoint**: Admin can view recent audit log entries via API. Basic retrieval works.

---

## Phase 5: User Story 5 - Custom Event Submission (Priority: P1)

**Goal**: Any authenticated API key can submit custom audit events (login, logout, started_stream) via POST /audit-events

**Note**: This was FR-012 in the spec and needs its own implementation phase

**Independent Test**: Authenticate with any API key, POST to /audit-events with event_type, verify entry created

### Tests for User Story 5

- [X] T032 [P] [US5] Create handler test for POST /audit-events in `internal/handler/auditlog_test.go`
- [X] T033 [P] [US5] Create test for any-auth access (not admin-only) in `internal/handler/auditlog_test.go`

### Implementation for User Story 5

- [X] T034 [US5] Create CreateAuditEventRequest type in `internal/handler/auditlog.go`
- [X] T035 [US5] Implement createAuditEvent handler method in `internal/handler/auditlog.go`
- [X] T036 [US5] Add AuditLogService.CreateCustomEvent method in `internal/service/auditlog.go`
- [X] T037 [US5] Register POST /audit-events route in `internal/server/server.go`
- [X] T038 [US5] Add request validation for event_type (required, max 50 chars) in `internal/handler/auditlog.go`

**Checkpoint**: Custom events can be submitted. Frontend can log user actions.

---

## Phase 6: User Story 3 - Filter and Search Audit Logs (Priority: P2)

**Goal**: Admin can filter audit logs by actor, action type, resource type, resource ID, and date range

**Independent Test**: Create entries with different attributes, apply each filter, verify only matching entries returned

### Tests for User Story 3

- [X] T039 [P] [US3] Create handler tests for each filter parameter in `internal/handler/auditlog_test.go`
- [X] T040 [P] [US3] Create repository test for filtered queries in `internal/database/auditlog_repo_test.go`

### Implementation for User Story 3

- [X] T041 [US3] Implement parseAuditLogFilter to extract query params in `internal/handler/auditlog.go`
- [X] T042 [US3] Add date range parsing (from, to) with RFC 3339 validation in `internal/handler/auditlog.go`
- [X] T043 [US3] Add actor, action, resource_type, resource_id filter parsing in `internal/handler/auditlog.go`
- [X] T044 [US3] Implement dynamic WHERE clause building in AuditLogRepo.List in `internal/database/auditlog_repo.go`
- [X] T045 [US3] Add filter validation error responses in `internal/handler/auditlog.go`

**Checkpoint**: All filter parameters work. Admin can narrow down audit entries.

---

## Phase 7: User Story 4 - Paginate Large Audit Logs (Priority: P2)

**Goal**: Admin can paginate through audit logs with limit/offset, receiving total count

**Independent Test**: Create 200+ entries, request limit=50 offset=0, verify 50 entries and total=200+

### Tests for User Story 4

- [X] T046 [P] [US4] Create handler test for pagination parameters in `internal/handler/auditlog_test.go`
- [X] T047 [P] [US4] Create test for pagination edge cases (limit bounds, large offset) in `internal/handler/auditlog_test.go`

### Implementation for User Story 4

- [X] T048 [US4] Add limit and offset parsing to parseAuditLogFilter in `internal/handler/auditlog.go`
- [X] T049 [US4] Enforce pagination limits (default 50, max 100) in `internal/service/auditlog.go`
- [X] T050 [US4] Add COUNT query to AuditLogRepo.List for total count in `internal/database/auditlog_repo.go`
- [X] T051 [US4] Include pagination metadata in response in `internal/handler/auditlog.go`

**Checkpoint**: Pagination works. Large audit logs are navigable.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [X] T052 [P] Add structured logging for all audit operations in `internal/service/auditlog.go`
- [X] T053 [P] Add before/after state capture for update operations in `internal/handler/middleware.go`
- [X] T054 Verify RFC 9457 error responses for all error cases in `internal/handler/auditlog.go`
- [X] T055 Run quickstart.md validation commands
- [X] T056 Update API documentation in `docs/API.md` with audit endpoints

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Story 2 (Phase 3)**: Depends on Foundational - recording must work before viewing
- **User Story 1 (Phase 4)**: Depends on Foundational - can proceed in parallel with US2
- **User Story 5 (Phase 5)**: Depends on Foundational - can proceed in parallel with US1/US2
- **User Story 3 (Phase 6)**: Depends on US1 (extends list endpoint)
- **User Story 4 (Phase 7)**: Depends on US1 (extends list endpoint)
- **Polish (Phase 8)**: Depends on all user stories being complete

### User Story Dependencies

```
Phase 1 (Setup)
     ↓
Phase 2 (Foundational)
     ↓
     ├─────────────────────────────────┬────────────────────┐
     ↓                                 ↓                    ↓
Phase 3 (US2: Recording)    Phase 4 (US1: Viewing)   Phase 5 (US5: Custom Events)
     └─────────────────────────────────┘
                    ↓
          ┌────────┴────────┐
          ↓                 ↓
Phase 6 (US3: Filtering)   Phase 7 (US4: Pagination)
          └────────┬────────┘
                   ↓
          Phase 8 (Polish)
```

### Within Each User Story

- Tests MUST be written first and FAIL before implementation (TDD)
- Domain/models before services
- Services before handlers
- Handlers before routes
- Integration before completion

### Parallel Opportunities

Within each phase, tasks marked [P] can run in parallel:

**Phase 2 (Foundational)**:
- T004, T005 can run in parallel with T003
- T007 can run in parallel with T006
- T009 can run in parallel with T008
- T010, T011 can run in parallel

**Each User Story Phase**:
- All test tasks marked [P] can run in parallel
- Tests and implementation proceed in TDD fashion

---

## Parallel Example: Foundational Phase

```bash
# First wave (no dependencies):
Task T003: "Create AuditLogEntry domain model..."
Task T004: "Add ErrAdminRequired domain errors..."
Task T005: "Create APIKey domain model..."

# Second wave (after T003, T005):
Task T006: "Implement AuditLogRepo..."
Task T007: "Implement APIKeyRepo..."

# Third wave (after T006, T007):
Task T008: "Create AuditLogService..."
Task T009: "Create APIKeyService..."
Task T010: "Add ErrForbidden HTTP error..."
Task T011: "Add getClientIP helper..."
```

---

## Implementation Strategy

### MVP First (User Stories 1, 2, 5)

1. Complete Phase 1: Setup (migrations)
2. Complete Phase 2: Foundational (domain, repo, service)
3. Complete Phase 3: US2 - Recording works
4. Complete Phase 4: US1 - Admin can view
5. Complete Phase 5: US5 - Custom events work
6. **STOP and VALIDATE**: Core audit functionality complete
7. Deploy/demo

### Incremental Delivery

1. Setup + Foundational → Core infrastructure ready
2. Add US2 → Recording actions → Can verify in database
3. Add US1 → Admin viewing → Deploy (MVP!)
4. Add US5 → Custom events → Deploy
5. Add US3 → Filtering → Deploy
6. Add US4 → Pagination → Deploy
7. Polish → Production ready

### Verification Commands

```bash
# Run migrations
docker-compose up -d postgres
go run cmd/migrate/main.go up

# Run all tests
go test ./internal/... -v

# Test specific package
go test ./internal/handler/... -v -run TestAudit

# Manual API testing
./scripts/api-test.sh POST /audit-events '{"event_type":"login"}'
./scripts/api-test.sh GET /audit-logs
./scripts/api-test.sh GET '/audit-logs?action=create&limit=10'
```

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- TDD: Write failing tests before implementation
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
