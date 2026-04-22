---
description: "Dependency-ordered tasks for API multi-tenant platform implementation"
---

# Tasks: Multi-Tenant Platform (API)

**Input**: Design documents from `/specs/003-multi-tenant-platform/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/api-routes.md, quickstart.md

**Tests**: Required. Constitution principle III (Comprehensive Testing) mandates integration tests via `testcontainers-go` and contract tests derived from `contracts/api-routes.md`. Test tasks are marked inline in each phase.

**Organization**: Tasks are grouped by user story to enable independent implementation and incremental delivery. MVP = Phase 1 + Phase 2 + Phase 3 (US1) + Phase 4 (US2) — the two P1 stories.

## Format

`[ID] [P?] [Story?] Description` — checkbox markdown, absolute file paths, `[P]` when parallelizable, `[US#]` for user-story-phase tasks only.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization, new dependencies, scaffolding.

- [ ] T001 [P] Add new Go dependencies in `go.mod`: `tailscale.com/tsnet`, `github.com/lib/pq` (for NOTIFY/LISTEN), pin existing `pgx/v5` versions; run `go mod tidy`.
- [ ] T002 [P] Create new `internal/` package directories (empty, with a short `doc.go` each): `internal/domain/{org,team,user,membership,tag,device,room,session}`, `internal/service/{org,membership,tag,device,room,acl,session,forcelogout,streamevents}`, `internal/database/{orgrepo,teamrepo,userrepo,membershiprepo,superadminrepo,tagrepo,devicerepo,devicekeyrepo,roomrepo,sessionrepo}`, `internal/handler/{org,team,membership,tag,device,room,session,streamevents}`, `internal/tsnet`, `internal/acl`, `internal/pepper`.
- [X] T003 [P] Create `fly.toml` at repo root with healthcheck `/health`, internal port 8080, region `sea`, min 1 machine / max 3 machines, secrets referenced (not declared).
- [X] T004 [P] Update `.golangci.yml` (if present) to lint the new package paths; confirm `go vet ./...` / `go fmt ./...` clean in CI.
- [X] T004a [P] Create repo-root `justfile` replacing the existing `Makefile` with the recipe inventory specified in `research.md` §12: `build`, `run`, `test`, `test-unit`, `test-integration`, `test-contract`, `lint`, `fmt`, `migrate-local`, `migrate-local-down`, `migrate-create`, `migrate-prod`, `hooks`, `verify`, `clean`, `setup`. `migrate-local` reads `$DATABASE_URL`. `migrate-prod` MUST read `$DATABASE_MIGRATION_URL` and refuse with a non-zero exit when (a) the var is unset, (b) the hostname contains `-pooler`, or (c) the hostname is not `*neon.tech`. `just` with no recipe defaults to `just --list`.
- [X] T004b [P] Create `tests/tooling/migrate_prod_guard_test.sh` (or a Go test at `tests/tooling/migrate_prod_guard_test.go` invoking `just migrate-prod` via `os/exec`): assert exit code ≠ 0 for unset `DATABASE_MIGRATION_URL`, for a pooled Neon host (e.g. `postgres://…@ep-foo-bar-pooler.neon.tech/db`), and for a non-Neon host (e.g. `postgres://…@example.com/db`); assert exit code 0 on a dry-run mode that stops before executing `golang-migrate` when given a valid non-pooled Neon host.
- [ ] T005 [P] Create `cmd/rescuestream-migrate/main.go` — a separate binary that runs `golang-migrate` against `$DATABASE_URL` and seeds super-admins from `$SUPER_ADMIN_EMAILS`. This is the one-off Fly command invoked pre-cutover.
- [X] T006 Update `internal/config/config.go` to add env vars: `SUPER_ADMIN_EMAILS`, `DEVICE_KEY_PEPPER`, `DEVICE_KEY_PEPPER_PREV`, `SESSION_SECRET_PEPPER`, `MEDIAMTX_WEBHOOK_SECRET`, `TAILSCALE_AUTHKEY`, `TAILSCALE_ENABLED` (bool, default false for local dev), `SSE_MAX_CONNS_PER_PROCESS` (default 1000), `SESSION_EXPIRY_DAYS` (default 30). All required in prod; dev has defaults where safe.
- [X] T007 Write `tests/contract/README.md` establishing the contract-test convention: one `*_contract_test.go` per resource, assertions against `specs/003-multi-tenant-platform/contracts/api-routes.md`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Schema + cross-cutting primitives on which ALL user stories depend. No user-story code can land until this phase is green.

**⚠️ CRITICAL**: Phase 2 must be complete before Phase 3+ begins.

### Database migrations

- [ ] T008 Create migration `internal/database/migrations/000003_v2_structure.up.sql` creating all new tables per `data-model.md` §1 (organizations, teams, users, organization_memberships, super_admins, tags, user_tag_assignments, devices, device_keys, rooms, room_acl_rules, room_devices, sessions). Include indexes noted in data-model. Add nullable `organization_id` + `actor_user_id` to `audit_logs`; add nullable `organization_id`, `room_id`, `device_id` to `streams`.
- [ ] T009 Create `000003_v2_structure.down.sql` that drops the new tables in reverse FK order and removes the added columns.
- [ ] T010 Create migration `000004_v2_backfill.up.sql`: seed default org + placeholder user (per data-model §5.1 steps 4–6); UPDATE `audit_logs` to set `organization_id`=default and `actor_user_id` from backfilled users; TRUNCATE `streams` (per FR-034); DROP `stream_keys` and `broadcasters` CASCADE.
- [ ] T011 Create `000004_v2_backfill.down.sql` — no-op (documented as intentionally non-reversible per spec).
- [ ] T012 Create migration `000005_v2_constraints.up.sql`: flip `organization_id` to NOT NULL on `streams`; add partial indexes documented in data-model; add CHECK constraint `organization_memberships.role = 'member' → team_id IS NOT NULL`; add polymorphic target CHECK on `room_acl_rules` via trigger.
- [ ] T013 [P] Create `000005_v2_constraints.down.sql` dropping the constraints in reverse order.
- [ ] T014 Wire new migrations into `cmd/rescuestream-migrate/main.go` so `SUPER_ADMIN_EMAILS` is passed to the seeding step as a transactional parameter.

### Shared primitives

- [X] T015 [P] Implement peppered HMAC hasher in `internal/pepper/pepper.go`: `Hash(plain string) string`, `Verify(plain, hashHex string) bool`, `VerifyWithPrev(plain, hashHex string) (bool, bool)` (second bool true if matched via prev pepper). Functional-options-style constructor taking current and optional prev pepper.
- [X] T016 [P] Implement ACL evaluator in `internal/acl/evaluator.go` per research §8: pure function `Evaluate(rs RuleSet, a Attrs) bool`, plus `Access(role, userAttrs, roomScope, teamID, ruleSet) AccessDecision`. No DB.
- [X] T017 [P] Implement optimistic-concurrency helper in `internal/database/concurrency.go`: wraps `RowsAffected` check after UPDATE, returns typed `ErrStaleVersion` with current version fetched on conflict.
- [ ] T018 [P] Implement in-process SSE hub in `internal/service/streamevents/hub.go`: goroutine-safe map user_id → []chan Event, methods `Subscribe(ctx, userID) (<-chan Event, func())`, `Publish(evt Event)`. Per research §4.
- [ ] T019 [P] Implement Postgres NOTIFY consumer in `internal/service/streamevents/listener.go`: long-lived LISTEN goroutine on `stream_events` channel that decodes JSON payloads and forwards into the hub. Functional options for channel name and reconnect behavior.
- [ ] T020 [P] Implement embedded Tailscale client in `internal/tsnet/client.go`: builds a `tsnet.Server` when `TAILSCALE_ENABLED=true`, else returns a plain `http.Client`. Exposes `HTTPClient() *http.Client` and `Dial(ctx, network, addr)`. Functional options for hostname, tags, state dir.
- [ ] T021 [P] Add new RFC 9457 problem types in `internal/handler/problems.go`: `no-org-membership`, `not-in-org`, `acl-denied`, `room-archived`, `stale-room-version` (includes `instance.metadata.current_version`), `device-key-revoked`, `session-invalidated`, `workspace-domain-taken`, `last-super-admin`, `retired-endpoint`.

### Core domain + repos used by multiple stories

- [ ] T022 Implement `internal/domain/user/user.go`: User struct + invariants (email normalization, google_subject optional).
- [ ] T023 [P] Implement `internal/domain/org/org.go`: Organization struct, status enum, invariants.
- [ ] T024 Implement `internal/database/userrepo/repo.go`: Upsert-by-google-subject, FindByEmail, FindByID, FindBySubject. pgx-based, uses prepared statements.
- [ ] T025 Implement `internal/database/orgrepo/repo.go`: Create, Get, List (paginated, with optional `q` typeahead), Update, SuspendUnsuspend, Delete, Counts. Every method takes a `callerCtx` holding caller identity to enforce tenancy at repo boundary.

### Session store + auth middleware

- [ ] T026 Implement `internal/domain/session/session.go`: Session struct + invariants (Valid() bool based on revoked_at, expires_at).
- [ ] T027 Implement `internal/database/sessionrepo/repo.go`: Create (returns plaintext secret once), FindByKeyID (hot-path, indexed lookup with peppered hash verify), Touch (update last_used_at + sliding expiry), Revoke(id, reason), RevokeAllForUser(userID, reason), EvictExpired.
- [ ] T028 Implement `internal/service/session/service.go`: LoginComplete (exchanges Google id_token for session keypair), Logout, RevokeAllForUser, ValidateSignedRequest. Functional-options constructor for pepper, expiry window, key entropy.
- [ ] T029 Rewrite `internal/handler/middleware.go::AuthMiddleware` per research §2: parse `X-API-Key` → lookup session → peppered-verify HMAC signature → check not-revoked / not-expired → enforce ±5 min timestamp drift → attach `user_id`, `session_id`, `role`, `org_id`, `team_id` to request context. On session-invalidated → problem `session-invalidated` (401).
- [ ] T030 Implement tenancy middleware in `internal/handler/middleware.go::TenancyMiddleware`: asserts target resource's `org_id` matches caller's `org_id`; super-admin bypass. Emits `not-in-org` problem (403) on mismatch. Emits `no-org-membership` (403) when caller has no membership.
- [ ] T031 Implement suspended-org denial in tenancy middleware: loads org.status; rejects all non-super-admin callers if suspended.

### Audit log extensions

- [ ] T032 Extend `internal/database/auditrepo/repo.go` to accept and filter by `organization_id` and `actor_user_id`; update List to auto-filter by caller's org for org-admins, unfiltered for super-admins.
- [ ] T033 Extend audit middleware in `internal/handler/middleware.go::AuditMiddleware` to resolve and record `actor_user_id` from context, `organization_id` from the request path, and new action/resource_type vocabulary per research §9.

### Foundation tests

- [X] T034 [P] Unit test for `internal/pepper` in `internal/pepper/pepper_test.go`: hash determinism, verify match/no-match, prev-pepper fallback.
- [X] T035 [P] Unit tests for `internal/acl/evaluator` in `internal/acl/evaluator_test.go`: property-based sweep of AND/OR/team/tag/user combinations; team-scope gate; admin bypass.
- [X] T036 [P] Unit test for `internal/database/concurrency` in `internal/database/concurrency_test.go`: stale-version detection, success path.
- [ ] T037 [P] Integration test in `tests/integration/session_test.go` using testcontainers-Postgres: login-complete, sign request with issued key, revoke, subsequent request is rejected within the freshness budget (SC-011).
- [ ] T038 [P] Integration test in `tests/integration/tenancy_test.go`: org A admin cannot read org B resources; super-admin can.
- [ ] T039 Integration test in `tests/integration/sse_hub_test.go`: cross-goroutine publish→subscribe delivery; multi-subscriber fan-out; ACL-based filtering.

**Checkpoint**: Foundation is ready — user story phases may begin in parallel.

---

## Phase 3: User Story 1 — Platform operator provisions a new organization (Priority: P1) 🎯 MVP

**Goal**: A super-admin creates an organization with initial admin emails; the named admin can sign in and see their org.

**Independent Test**: Super-admin POSTs to `/orgs` with a fresh name and admin email; org-admin signs in via OAuth and lands on their org dashboard; non-members cannot see the org.

### Contract tests (US1)

- [ ] T040 [P] [US1] Contract test `tests/contract/orgs_contract_test.go`: `POST /orgs`, `GET /orgs`, `GET /orgs/{id}`, `PATCH /orgs/{id}`, `DELETE /orgs/{id}`, `POST /orgs/{id}/admins`, `DELETE /orgs/{id}/admins/{uid}` — assert status codes, shapes, authz denials per `contracts/api-routes.md` §2.
- [ ] T041 [P] [US1] Contract test `tests/contract/superadmins_contract_test.go`: `GET|POST|DELETE /super-admins` + `last-super-admin` 409 when removing the last one.

### Super-admins

- [ ] T042 [P] [US1] Implement `internal/database/superadminrepo/repo.go`: List, Add(user_id, granted_by, seeded_from_env), Remove(user_id), CountRemaining (used to block last-removal).
- [ ] T043 [US1] Implement `internal/service/session/bootstrap.go` (invoked from `cmd/rescuestream-api/main.go` on startup): read `SUPER_ADMIN_EMAILS`, upsert users, ensure super_admins rows, log diff.
- [ ] T044 [P] [US1] Implement `internal/handler/session/superadmin.go`: `GET /super-admins`, `POST /super-admins`, `DELETE /super-admins/{uid}` with `last-super-admin` guard.

### Organizations service + handlers

- [ ] T045 [US1] Implement `internal/service/org/service.go`: Create (accepts name, slug, initial_admin_emails; atomically creates org + org-admin memberships for any existing users + invite placeholders for non-existent emails), List (with typeahead `q`), Get, Update, SuspendUnsuspend, Delete (soft-archive rooms + revoke sessions + hard-delete). Functional-options constructor.
- [ ] T046 [US1] Implement `internal/service/org/admins.go`: AddAdmin(orgID, email) — upserts user, sets/promotes membership with role=org-admin; RemoveAdmin — downgrades or removes membership; both emit audit events and revoke existing sessions for the affected user.
- [ ] T047 [US1] Implement `internal/handler/org/handler.go`: `POST /orgs`, `GET /orgs`, `GET /orgs/{id}`, `PATCH /orgs/{id}`, `DELETE /orgs/{id}`, `POST /orgs/{id}/admins`, `DELETE /orgs/{id}/admins/{uid}` wired into the gorilla/mux router in `cmd/rescuestream-api/main.go`.
- [ ] T048 [US1] Wire audit hooks for `org.created`, `org.suspended`, `org.deleted`, `super_admin.granted`, `super_admin.revoked` into the relevant service calls.

### Integration tests (US1)

- [ ] T049 [US1] Integration test `tests/integration/us1_org_provision_test.go` covering spec US1 acceptance scenarios 1–3.

**Checkpoint**: US1 complete. Super-admin can provision orgs; org-admins are routed correctly on sign-in.

---

## Phase 4: User Story 2 — Member auto-joins via verified Workspace domain (Priority: P1)

**Goal**: A user on a team's Workspace domain signs in with Google and auto-joins the team. No-org users see awaiting-access.

**Independent Test**: Create a team with `workspace_domain = example.org`; sign in with `alice@example.org` → membership created + lands on dashboard. Sign in with `bob@other.org` not listed anywhere → awaiting-access response.

### Contract tests (US2)

- [ ] T050 [P] [US2] Contract test `tests/contract/teams_contract_test.go`: team CRUD + `workspace-domain-taken` 409.
- [ ] T051 [P] [US2] Contract test `tests/contract/sessions_contract_test.go`: `POST /sessions/login-complete` with a mock Google id_token; `POST /sessions/logout`; `POST /orgs/{id}/members/{uid}/revoke-sessions`.
- [ ] T052 [P] [US2] Contract test `tests/contract/members_contract_test.go`: `GET /orgs/{id}/members`, `GET /orgs/{id}/members/{uid}`, `DELETE /orgs/{id}/members/{uid}`.

### Teams

- [ ] T053 [P] [US2] Implement `internal/domain/team/team.go`: Team struct + `workspace_domain` validation (lowercased, RFC-1035-ish).
- [ ] T054 [P] [US2] Implement `internal/database/teamrepo/repo.go`: Create, ListByOrg, Get, Update, Delete; UNIQUE-violation detection to raise `workspace-domain-taken`.
- [ ] T055 [US2] Implement `internal/service/membership/service.go`: AutoJoinFromGoogle(ctx, googleID, email, name, avatar) — upsert the user row by google_subject; **if the user already holds any `organization_memberships` row (regardless of org/role), reuse it and skip domain-based auto-join** (this preserves org-admin precedence per the spec edge case for org-admin emails colliding with team domains elsewhere); otherwise look up a team by email domain and, if found, insert a member membership in that team's org; records an audit entry only on actual membership change.
- [ ] T056 [US2] Implement `internal/service/membership/removal.go`: RemoveMember (force-revoke sessions + drop tag assignments + audit).
- [ ] T057 [P] [US2] Implement `internal/database/membershiprepo/repo.go`: Upsert (respecting UNIQUE(user_id)), ListByOrg (paginated, filters), GetByUser, RemoveByUser.
- [ ] T058 [US2] Implement `internal/handler/team/handler.go`: `POST /orgs/{id}/teams`, `GET /orgs/{id}/teams`, `PATCH /teams/{id}`, `DELETE /teams/{id}`. Team deletion MUST be the coordinated transaction documented in `data-model.md` §1.4 Invariants: (a) delete `organization_memberships` rows with `team_id = this_team AND role = 'member'`, (b) `sessionrepo.RevokeAllForUser` for each removed member (revoked_reason = `'team-deleted'`), (c) `UPDATE` any org-admin rows referencing this `team_id` to `NULL`, (d) `DELETE` the team. Emit audit entries for each membership revocation and session revocation.
- [ ] T059 [US2] Implement `internal/handler/membership/handler.go`: `GET /orgs/{id}/members`, `GET /orgs/{id}/members/{uid}`, `DELETE /orgs/{id}/members/{uid}`, `POST /orgs/{id}/members/{uid}/revoke-sessions`.

### Login flow

- [ ] T060 [US2] Implement `internal/handler/session/login.go`: `POST /sessions/login-complete` — verify Google id_token (using `google.golang.org/api/idtoken` or crypto/jwt against Google certs), call `membership.AutoJoinFromGoogle`, check `super_admins`, mint a session keypair, return `{session_key_id, session_secret, user, role, org_id, team_id}`.
- [ ] T061 [US2] Implement `internal/handler/session/logout.go`: `POST /sessions/logout` — revokes the current session.
- [ ] T062 [US2] Implement `internal/handler/session/forcelogout.go`: `POST /orgs/{id}/members/{uid}/revoke-sessions` — bulk revoke for a member; org-admin authz; super-admin platform-wide authz.
- [ ] T063 [US2] Implement no-org handler response: when a validated session has no membership AND no super-admin record, middleware short-circuits with `no-org-membership` (403) and the handler never runs.

### Integration tests (US2)

- [ ] T064 [US2] Integration test `tests/integration/us2_auto_join_test.go` covering spec US2 acceptance scenarios 1–3.
- [ ] T064a [US2] Integration test `tests/integration/us2_v1_allowlist_post_cutover_test.go` (SC-009): run the cutover migration command with a fixture `SUPER_ADMIN_EMAILS` + a non-empty fake v1 allowlist (passed as a migration parameter); confirm the default org is created and each v1 email has a matching `users` row and member membership; `login-complete` for each such email returns a valid session with `role=member` and `org_id=<default>` in a single request with no administrator action between inputs.
- [ ] T064b [US2] Integration test `tests/integration/us2_org_admin_precedence_test.go`: user already holding an org-admin membership whose email matches a team domain in *another* organization signs in; assert their existing org-admin membership is preserved and no new membership is created (exercises T055's precedence short-circuit).
- [ ] T064c [US2] Integration test `tests/integration/us2_team_deletion_test.go`: team with members + an org-admin referencing the team; delete the team; assert member memberships removed, their sessions revoked with reason=`team-deleted`, org-admin row preserved with `team_id=NULL`, team row deleted; audit entries present for each step.
- [ ] T065 [US2] Integration test `tests/integration/us2_force_logout_test.go` for SC-011 (<5 s session invalidation).

**Checkpoint**: US1 + US2 complete. A full provisioning → sign-in → dashboard path is green. This is the MVP cut.

---

## Phase 5: User Story 3 — Org-admin manages rooms with access rules (Priority: P2)

**Goal**: Org-admin creates a room with AND/OR ACL composition over teams / tags / members; the right members see it; ACL preview works live.

**Independent Test**: With a pre-populated org (2 teams, 2 tags, 4 members), create a room with an OR rule across one team and one tag; verify only matching members see the room; switch to AND and verify it narrows.

### Contract tests (US3)

- [ ] T066 [P] [US3] Contract test `tests/contract/rooms_contract_test.go`: full room lifecycle incl. `PATCH /rooms/{id}` with/without `expected_version`, `POST /rooms/{id}/archive(_unarchive)`, 409 `stale-room-version` on stale version.
- [ ] T067 [P] [US3] Contract test `tests/contract/acl_preview_contract_test.go`: POST `/orgs/{id}/rooms/acl-preview` — verifies match_count accuracy for synthetic orgs, 500 ms p95 SLA (SC-012).
- [ ] T068 [P] [US3] Contract test `tests/contract/stream_events_contract_test.go`: SSE connect, receive `stream.started` on a test publish, receive `stream.ended`, reject if caller cannot access the room.

### Rooms

- [ ] T069 [P] [US3] Implement `internal/domain/room/room.go`: Room struct, lifecycle state machine (`active` ↔ `archived`), invariants (scope vs team_id).
- [ ] T070 [P] [US3] Implement `internal/domain/room/acl.go`: ACLRule, ACLRuleSet, validation (combinator enum, rule type enum, target-existence deferred to service).
- [ ] T071 [US3] Implement `internal/database/roomrepo/repo.go`: Create (room + ACL rules + optional device_allowlist in one tx), Get, ListByOrg (filter by lifecycle, scope, caller visibility), Update (with `expected_version` → ErrStaleVersion on conflict), ReplaceACL (tx: DELETE + INSERT rules + version bump), ArchiveUnarchive (version-guarded), TouchActivity.
- [ ] T072 [US3] Implement `internal/service/room/service.go`: Create, Get (access-gated via ACL evaluator), List (filters list by caller visibility), Update (validates ACL rule targets exist in the same org), Archive/Unarchive.
- [ ] T073 [US3] Implement `internal/service/acl/preview.go`: PreviewACL(ctx, orgID, scope, teamID, rules, combinator) — loads `members_with_tags` rows for the org, runs the evaluator in memory, returns `(match_count, sample_members[:20])`. Instrumented with the `acl_preview_duration_seconds` histogram from research §11.
- [ ] T074 [P] [US3] Implement `members_with_tags` DB view in a new migration `000006_members_with_tags_view.up.sql` (and a `.down.sql`).
- [ ] T075 [US3] Implement `internal/handler/room/handler.go`: `POST /orgs/{id}/rooms`, `GET /orgs/{id}/rooms`, `GET /rooms/{id}`, `PATCH /rooms/{id}`, `POST /rooms/{id}/archive`, `POST /rooms/{id}/unarchive`, `POST /orgs/{id}/rooms/acl-preview`.

### Room archive background job

- [ ] T076 [US3] Implement `internal/service/room/archiver.go`: a goroutine kicked off at startup that every N minutes UPDATEs rooms to `lifecycle_state='archived'` where `last_activity_at + archive_after < now()`. Emits audit entries. Functional-options configurable interval.

### Stream events consumer (SSE)

- [ ] T077 [US3] Implement `internal/handler/streamevents/handler.go`: `GET /stream-events` — sets SSE headers, subscribes to hub for the caller, per-event ACL filter using `internal/acl` + `roomrepo.Get`, 30 s heartbeat, graceful shutdown frame on SIGTERM. Respects `SSE_MAX_CONNS_PER_PROCESS`.
- [ ] T078 [US3] Wire `service/streamevents/listener.go` into `cmd/rescuestream-api/main.go` startup so NOTIFY events drive the hub.

### Integration tests (US3)

- [ ] T079 [US3] Integration test `tests/integration/us3_acl_matrix_test.go`: builds a fixture org with 2 teams × 2 tags × 4 members, creates rooms with each of the ACL shapes from US3 acceptance scenarios 1–4, asserts the correct visibility.
- [ ] T080 [US3] Integration test `tests/integration/us3_optimistic_concurrency_test.go`: concurrent PATCHes, verifies 409 on stale version (SC-014).
- [ ] T081 [US3] Integration test `tests/integration/us3_archive_test.go`: sets last_activity_at in the past, runs archiver, verifies lifecycle_state flips within 60 s of the scheduled time (SC-006).

**Checkpoint**: US1 + US2 + US3 complete. Rooms, ACL authoring, and the push channel are live.

---

## Phase 6: User Story 4 — Device authenticates and streams into a room (Priority: P2)

**Goal**: Org-admin registers a device, mints a key, device RTMP-publishes against a room, stream is authorized and visible.

**Independent Test**: Register device; mint primary key (captured plaintext); simulate RTMP publish with that key → 200 from `/auth`; stream row appears; SSE emits `stream.started`.

### Contract tests (US4)

- [ ] T082 [P] [US4] Contract test `tests/contract/devices_contract_test.go`: device CRUD + `POST /devices/{id}/keys/rotate` + `DELETE /devices/{id}/keys/{kid}`; asserts plaintext appears only in mint/rotate response.
- [ ] T083 [P] [US4] Contract test `tests/contract/auth_webhook_test.go`: mediamtx `POST /auth` with active key → 200; revoked key → 401; wrong-org room → 401; archived room → 401; bearer webhook secret required.

### Devices

- [ ] T084 [P] [US4] Implement `internal/domain/device/device.go`: Device + DeviceKey structs + invariants (max 2 active keys: primary + secondary).
- [ ] T085 [P] [US4] Implement `internal/database/devicerepo/repo.go`: Create, Get, ListByOrg, Update, Delete, TouchLastSeen.
- [ ] T086 [P] [US4] Implement `internal/database/devicekeyrepo/repo.go`: MintKey (returns plaintext; stores peppered hash; enforces one-active-per-slot), FindByHash (hot path for `/auth`), RevokeKey.
- [ ] T087 [US4] Implement `internal/service/device/service.go`: CreateDevice (mints primary + returns plaintext once), RotateKey (mints in target slot), RevokeKey, UpdateDevice, DeleteDevice. All emit audit; plaintext never written to audit metadata.
- [ ] T088 [US4] Implement `internal/handler/device/handler.go`: device CRUD + key-rotate + key-revoke per `contracts/api-routes.md` §7.

### Auth webhook rewrite

- [ ] T089 [US4] Rewrite `internal/handler/auth.go`: peppered-hash the submitted key → look up active device_key → load device + room via `path` → validate org match, room active, device policy (any-org-device vs allowlist) → return 200/401. Keep response envelope unchanged (mediamtx contract).
- [ ] T090 [US4] Add bearer check in `internal/handler/middleware.go::WebhookAuthMiddleware` for `Authorization: Bearer $MEDIAMTX_WEBHOOK_SECRET`; attach to `/auth`, `/webhook/ready`, `/webhook/not-ready` routes.

### Stream lifecycle + NOTIFY emit

- [ ] T091 [US4] Rewrite `internal/handler/webhook.go::handleReady`: insert `streams` row with org_id/room_id/device_id resolved from the authed path; `NOTIFY stream_events '<json>'` in the same tx.
- [ ] T092 [US4] Rewrite `internal/handler/webhook.go::handleNotReady`: end stream row; `NOTIFY stream_events '<json>'`.

### Room-device allowlist

- [ ] T093 [US4] Extend `roomrepo` Create/Update to persist `room_devices` when `default_device_policy='allowlist'`; validate all device_ids belong to the room's org.
- [ ] T094 [US4] Extend `handler/auth.go` to consult `room_devices` when the target room's policy is allowlist.

### mediamtx control API over tsnet

- [ ] T095 [US4] Rewrite `internal/service/mediamtx/` to use the `internal/tsnet.HTTPClient()` for outbound control-API calls; add bearer header to match mediamtx's admin auth configuration.

### Integration tests (US4)

- [ ] T096 [US4] Integration test `tests/integration/us4_device_auth_test.go` covering US4 acceptance scenarios 1–5.
- [ ] T096a [US4] Integration test `tests/integration/us4_revocation_timing_test.go` asserting SC-005: after `RevokeKey`, subsequent `/auth` calls with the revoked key MUST fail within 30 seconds wall-clock. Test implementation: capture `t_revoke`, poll `/auth` every 500 ms with the revoked key, assert first rejection occurs at `t_revoke + Δ < 30 s`. Run the poll loop 100 times across a single test process (parallel sub-tests against a shared testcontainers-Postgres) and assert ≥99 pass (matches SC-005's "99% of cases" wording).
- [ ] T097 [US4] Integration test `tests/integration/us4_sse_end_to_end_test.go`: simulate publish → NOTIFY → SSE delivery end-to-end (SC-013).

**Checkpoint**: US1–US4 complete. The platform's core product function works in v2.

---

## Phase 7: User Story 5 — Tags (Priority: P3)

**Goal**: Org-admin creates tags, assigns to members, uses them in ACLs.

**Independent Test**: Create two tags; assign to two members (one each); create a room targeting one tag; verify only the tagged member sees it.

### Contract tests (US5)

- [ ] T098 [P] [US5] Contract test `tests/contract/tags_contract_test.go`: tag CRUD + tag assignment + cascade-on-delete behavior.

### Tag domain + implementation

- [ ] T099 [P] [US5] Implement `internal/domain/tag/tag.go`: Tag struct + slug validation.
- [ ] T100 [P] [US5] Implement `internal/database/tagrepo/repo.go`: Create, ListByOrg, Update, Delete; on Delete, cascade via FK to `user_tag_assignments` and to `room_acl_rules WHERE type='tag'`; bump `version` on affected rooms in the same tx.
- [ ] T101 [US5] Implement `internal/service/tag/service.go`: CreateTag, UpdateTag, DeleteTag (with cascading room-version bumps), AssignTag, RevokeTag. Audit every change.
- [ ] T102 [US5] Implement `internal/handler/tag/handler.go` and `internal/handler/membership/tags.go`: tag CRUD + tag-assignment endpoints per `contracts/api-routes.md` §6.

### Integration tests (US5)

- [ ] T103 [US5] Integration test `tests/integration/us5_tags_in_acl_test.go`: create tags, assign, verify ACL match, delete tag, verify rooms lose the rule and bump version.

**Checkpoint**: US1–US5 complete.

---

## Phase 8: User Story 6 — Org-scoped audit log viewer (Priority: P3)

**Goal**: Org-admin sees audit entries scoped to their org; super-admin sees cross-org with optional filter.

**Independent Test**: Generate events in org A and org B; org-admin A sees only A; org-admin A gets 403 when forcing `org_id=B`; super-admin sees both.

### Contract tests (US6)

- [ ] T104 [P] [US6] Contract test `tests/contract/audit_contract_test.go`: `GET /audit-logs` with and without `org_id`; org-admin tenancy enforcement; super-admin cross-org.

### Audit log scoping

- [ ] T105 [US6] Wire new action vocabulary (research §9) into every mutation path that emits audit entries: orgs, teams, memberships, tags, tag assignments, devices, device keys, rooms, ACL rules, sessions, super-admins. Ensure each carries `{before, after}` where meaningful.
- [ ] T106 [US6] Update `internal/handler/auditlog.go::handleListAuditLogs` to force-filter by caller's org for org-admins (ignore or 403 if `org_id` param mismatches); super-admins may omit or set.
- [ ] T107 [US6] Update `internal/handler/auditlog.go::handleCreateAuditEvent` to require super-admin (narrowed from v1).

### Integration tests (US6)

- [ ] T108 [US6] Integration test `tests/integration/us6_audit_scoping_test.go` per spec US6 acceptance scenarios 1–3.

**Checkpoint**: All user stories complete.

---

## Phase 9: Polish & Cross-Cutting

- [ ] T109 [P] Retire v1 routes: delete `internal/handler/broadcaster.go`, `internal/handler/streamkey.go`, `internal/service/broadcaster/*`, `internal/service/streamkey/*`, `internal/database/broadcaster_repo.go`, `internal/database/streamkey_repo.go`, and related tests.
- [ ] T110 [P] Add 410 redirect handlers at the old paths returning `retired-endpoint` problem details pointing to the replacements.
- [ ] T111 [P] Remove the v1 `EnvKeyStore` + shared `API_SECRET` code; ensure no code path still reads `API_SECRET`.
- [ ] T112 [P] Register OpenTelemetry metrics from research §11 (SSE connections, event delivery latency, session validations/invalidations, ACL preview duration, room version conflicts, device auth duration).
- [ ] T113 [P] Update `README.md` to reflect v2 architecture (multi-tenant, Fly + NeonDB, retired broadcasters/stream_keys). Swap every `make <recipe>` invocation (currently lines ~108–126 covering `build`, `run`, `test`, `lint`, `migrate`, `migrate-down`, `migrate-create`) to the corresponding `just <recipe>` form; add a short note documenting `DATABASE_URL` (local/testcontainers) vs `DATABASE_MIGRATION_URL` (prod-only, Neon non-pooled) per the 2026-04-21 spec clarifications.
- [ ] T113a Delete the repo-root `Makefile` in the same PR that introduces `justfile` (T004a). No Makefile shim is kept. Verify `.githooks/` and `.github/workflows/*.yml` do not regress (current scan shows no `make` references there, but re-confirm in the PR).
- [ ] T114 [P] Update `docker-compose.yml` dev stack: remove `lgtm` container (observability now hosted); keep Postgres + mediamtx.
- [ ] T115 Run the full quickstart `quickstart.md` §§4–7 against a fresh local stack; check each smoke test passes.
- [ ] T115a Author a prod-migration operator runbook at `docs/ops/migrate-prod-runbook.md` covering: (1) pre-flight — `flyctl status`, confirm `DATABASE_MIGRATION_URL` points to the Neon non-pooled host, confirm schema_migrations table is clean (`dirty=false`); (2) the happy path — `just migrate-prod` → verify new version in `schema_migrations` → `flyctl deploy`; (3) **dirty-state recovery** — if `golang-migrate` fails mid-run and leaves `schema_migrations.dirty=true`, the steps are: inspect failure cause, manually resolve schema to the expected state of either the prior or target version, then `migrate force <version>` to clear the dirty flag, re-run `just migrate-prod`; (4) rollback stance — v2 migrations 000003/000004/000005 are explicitly forward-only (per FR-034), so the runbook documents a "fail-forward" policy (do not attempt `migrate down` across the cutover boundary). Link this runbook from `plan.md` and from `research.md` §10.
- [ ] T116 Run `go test -race ./...` and fix any races surfaced by SSE hub or session-store code.
- [ ] T117 Benchmark `/orgs/{id}/rooms/acl-preview` with a fixture org of 500 members; assert p95 < 500 ms (SC-012).
- [ ] T118 Benchmark the `/auth` webhook with a warmed pool; assert p95 < 50 ms (constitution VI).
- [ ] T119 Load-test SSE: 500 concurrent subscribers, 100 stream-start events/min; measure event delivery latency p95 < 5 s (SC-013).
- [ ] T120 Rebase `003-multi-tenant-platform` onto fresh `origin/main` (resolves the stale-base flagged in earlier plan notes).
- [ ] T121 Staging deploy rehearsal — execute the full runbook (per 2026-04-21 Q3 clarification) end-to-end against a staging Neon branch: (1) ensure `DATABASE_MIGRATION_URL` is set to the staging Neon **non-pooled** host (`*.neon.tech`, NOT `*-pooler.neon.tech`); (2) `just migrate-prod` — verify the guard refuses a pooled URL first, then succeeds against non-pooled; (3) inspect `schema_migrations` to confirm the expected target version with `dirty=false`; (4) `flyctl deploy` to roll the API machines; (5) smoke-test `/health` and one authenticated endpoint; (6) document the elapsed wall-clock for each step in the PR description. The API container MUST NOT run migrations on boot (verify via container logs showing no `golang-migrate` activity). Links: T115a runbook, research §10, `.specify/memory/constitution.md` §VI (performance).
- [ ] T122 Final constitution-compliance pass: confirm all 6 principles hold; document the one `text/event-stream` exception already recorded in plan.md Complexity Tracking.

---

## Dependencies & Execution Order

### Phase dependencies

- **Phase 1 Setup**: no dependencies.
- **Phase 2 Foundational**: depends on Phase 1; blocks all user stories.
- **Phases 3–8 (user stories)**: depend on Phase 2. Can run in parallel with adequate team capacity. Sequential priority is P1 (US1, US2) → P2 (US3, US4) → P3 (US5, US6).
- **Phase 9 Polish**: depends on all user-story phases being complete for the polish tasks to be meaningful.

### Inter-story dependencies

- US3 Rooms depends on US2 Membership (for member lookups in ACL evaluation) and Phase 2 ACL evaluator.
- US4 Devices depends on Phase 2 pepper/device_keys primitives; the auth path also consults rooms created in US3 but can ship with org-scoped rooms only (room_devices allowlist is optional).
- US5 Tags enhances US3 ACL authoring but does not block it (ACL evaluator already supports tag rules from Phase 2).
- US6 Audit scoping depends on every prior story emitting the new action vocabulary — that's why T105 wires vocabulary across all previously-implemented mutations.

### Parallel opportunities

- All Phase 1 tasks marked [P] can run concurrently (T001–T005 at least, including T004a justfile + T004b guard test).
- T113a (Makefile deletion) depends on T004a (justfile) being landed and T113 (README swap) being finalized so the repo never has a window where neither file supports the documented recipes.
- Phase 2 splits cleanly: migrations (T008–T014), primitives (T015–T021), and core domain (T022–T025) can all run in parallel; session+middleware (T026–T031) depends on pepper primitive from T015.
- Within each user story phase, contract tests (T040, T050, T066, T082, T098, T104) run in parallel with each other and with unit-scale model tasks.
- Across stories: US1, US2 can develop in parallel after Phase 2; US3, US4 can develop in parallel after US2; US5, US6 can develop in parallel after US3 + US4.

---

## Parallel Example: Phase 2 Foundational

```bash
# Migrations (one dev working the SQL):
Task: T008-T014 sequential (migrations are ordered)

# Primitives (can run concurrently — separate files):
Task: T015 pepper                        # dev A
Task: T016 acl evaluator                 # dev B
Task: T017 concurrency helper            # dev C
Task: T018 sse hub                       # dev D
Task: T019 sse listener                  # dev D (same person as T018)
Task: T020 tsnet client                  # dev E

# Core domain + repos (run after migrations available):
Task: T022-T025 in parallel after T014

# Session + middleware block (requires T015 + T024):
Task: T026-T031 sequential within the block
```

---

## Implementation Strategy

### MVP: Phase 1 + Phase 2 + US1 (P1) + US2 (P1)

Delivers: super-admin creates an org, org-admins manage admins, members auto-join by Workspace domain, force-logout and session invalidation work, no-org users see awaiting-access, audit log captures identity changes. Enough to demo the multi-tenant model end-to-end; falls short of active streaming (US4) and room authoring UX (US3).

### Incremental delivery

1. MVP (US1 + US2) → deploy to staging; validate provisioning + sign-in flows.
2. Add US3 (rooms + ACL + SSE) → deploy; validate room creation + preview + live indicator.
3. Add US4 (devices + stream auth) → deploy; validate end-to-end stream publishing.
4. Add US5 (tags) → deploy; validate ACL authoring with attributes.
5. Add US6 (scoped audit) → deploy; validate forensics.
6. Phase 9 Polish → deploy; load-test; cut over.

### Parallel team strategy

With 3 developers after Phase 2 completes:

- Dev A: US1 (small) → US3 (largest).
- Dev B: US2 → US4.
- Dev C: US5 → US6 → Phase 9 polish tracks.

Merges land behind a "v2" branch that is rebased onto `main` just before cutover.

---

## Notes

- [P] tasks = different files, no dependencies.
- [US#] = user story attribution.
- Each user story phase ends at a checkpoint; validate the story works end-to-end before starting the next.
- Tests precede implementation where they exist (TDD-adjacent): write contract tests first, confirm they fail, then implement until green. This is the constitution III + IV posture.
- Commit after each task or small logical group; do not batch.
- Any cross-story dependency that isn't explicitly called out here should be treated as a design smell and surfaced in PR review.
