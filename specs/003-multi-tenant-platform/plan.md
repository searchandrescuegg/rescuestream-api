# Implementation Plan: Multi-Tenant Platform

**Branch**: `003-multi-tenant-platform` | **Date**: 2026-04-21 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/003-multi-tenant-platform/spec.md`

## Summary

Transform the RescueStream API from a single-tenant application into a multi-tenant platform with organizations, teams (Workspace-domain-scoped), members, org-level tags, devices (replacing broadcasters), and rooms with AND/OR-composable ACLs across teams/tags/members. Adds three role tiers (super-admin, org-admin, member), a server-side session store with admin-initiated invalidation, optimistic concurrency on room mutations, an ACL preview endpoint, and a Server-Sent Events push channel for stream-status updates. Retires the shared `API_SECRET` model in favor of per-user HMAC session keys, and stores device key credentials as peppered HMAC hashes. Simultaneously migrates the runtime off a GCP VM: API to Fly.io, Postgres to NeonDB. mediamtx stays on the VM, reached over a Tailscale tailnet.

Per the 2026-04-21 clarification session: **local dev and automated tests run against Docker/testcontainers Postgres 15** (not a Neon branch) using the same `golang-migrate` migrations that target NeonDB in production; **the Makefile is replaced wholesale by a `justfile`**; and **production schema migrations are operator-initiated pre-deploy** (`just migrate-prod`) against Neon's non-pooled endpoint, never on container boot.

## Technical Context

**Language/Version**: Go 1.25 (existing; see `go.mod`)

**Primary Dependencies** (existing — keep):

- `github.com/gorilla/mux` — HTTP router
- `github.com/jackc/pgx/v5/pgxpool` — PostgreSQL connection pool
- `github.com/golang-migrate/migrate/v4` — SQL migrations
- `alpineworks.io/gomediamtx` — MediaMTX control API client
- `alpineworks.io/ootel` — OpenTelemetry setup
- `github.com/alpineworks/rfc9457` — RFC 9457 error responses
- `github.com/caarlos0/env/v11` — env-var config
- `github.com/google/uuid` — UUID generation
- `log/slog` — structured logging

**Primary Dependencies** (new — introduce):

- `tailscale.com/tsnet` — embedded Tailscale client so the API can dial the mediamtx VM by tailnet hostname without running a separate daemon on Fly.
- `crypto/hmac` + `crypto/sha256` (stdlib) — device-key verification via HMAC-SHA256 with a server-side pepper (indexed lookup). See `research.md` §1.
- `net/http` SSE support — no new dep; stdlib flush semantics are sufficient, plus an in-memory pub/sub hub (built in-house, ~100 LOC) and Postgres LISTEN/NOTIFY fan-out across Fly machines.
- Postgres-backed session store (no Redis) — see `research.md` §3.

**Tooling**:

- **Task runner**: `just` (replaces the existing `Makefile`; see `research.md` §12). Justfile ships with per-target migration recipes: `just migrate-local` (uses `DATABASE_URL`) and `just migrate-prod` (uses `DATABASE_MIGRATION_URL`, refuses hostnames that are not Neon non-pooled).
- **Git hooks** continue to be wired via the old `make hooks` workflow, now as `just hooks`.

**Storage**:

- **Production**: NeonDB (PostgreSQL 15+ compatible). Connection string pooled via Neon's pooler endpoint (`*-pooler.neon.tech`) for request handlers; non-pooled endpoint used by `just migrate-prod`.
- **Local development**: Postgres 15 via `docker compose up postgres`, accessed through `DATABASE_URL` (no pooler split needed locally).
- **Automated tests**: `testcontainers-go` spins up Postgres 15 per test run; the same migrations execute, giving schema parity with prod.
- Neon-specific features are explicitly avoided so the Docker-local and Neon-prod schemas remain bit-compatible.

**Testing**:

- `github.com/testcontainers/testcontainers-go` — spins up Postgres 15 for integration tests.
- `net/http/httptest` — handler tests.
- `github.com/stretchr/testify` — `assert` / `require` / `mock`.
- Coverage targets per constitution: >80% business logic, >60% overall.

**Target Platform**: Fly.io (Linux container). Single Fly app `rescuestream-api`, primary region `sea`.

**Project Type**: Single API service (unchanged).

**Performance Goals**:

- Existing: p95 <50ms for simple/cached operations; <100ms for DB operations.
- **New**: p95 <500ms for ACL preview (SC-012); <5s end-to-end for stream-status SSE event delivery (SC-013); <5s for session-invalidation take-effect (SC-011); 100% concurrency-conflict detection (SC-014).
- Long-lived SSE connections MAY exceed p95 request duration targets — exempt them from the generic p95 SLO; track separately (connection count, event-delivery latency).

**Constraints**:

- All API-held secrets MUST be provisioned via Fly secrets; none in source.
- mediamtx webhook path traverses a Tailscale tailnet; webhook bearer secret adds defense-in-depth.
- Multi-tenant isolation: every resource-touching query MUST be scoped by `org_id` (or by caller role for cross-org super-admin paths).
- Device key plaintexts are shown exactly once in the API response at mint/rotation time and never re-emitted (enforced in handlers and logs).
- **Production migrations are operator-initiated**: the API container MUST NOT execute migrations on boot. A failed migration blocks the deploy rather than crash-looping the service.
- **`just migrate-prod` MUST refuse pooled Neon hostnames** (any host containing `-pooler`) — `golang-migrate` requires a non-pooled session for advisory locks.

**Scale/Scope** (first-year targets):

- 10–50 organizations.
- Up to 500 members per organization (baseline for the ACL-preview SLO).
- Dozens of concurrent streams per organization.
- Low hundreds of active SSE consumers across the platform.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Go Standards Compliance | PASS | All new code follows existing `internal/` package conventions; passes `go vet` / `golangci-lint` / `go fmt` in CI (justfile recipes carry the same checks as the retired Makefile). |
| II. Functional Options Pattern | PASS | New clients (Tailscale-aware HTTP client for mediamtx, session store, SSE hub, peppered-hash verifier) all use functional options. |
| III. Comprehensive Testing | PASS | testcontainers covers Postgres (Neon-compatible via the `postgres:15` image). SSE tested via a flushing `httptest.Recorder` wrapper and a test hub. Tailscale integration stubbed in tests behind an interface. Local-dev and test share the same migration path (no Neon-branch dependency for CI or contributor loops). |
| IV. RFC 9457 Error Responses | PASS | New canonical error `type` URIs: `/problems/no-org-membership`, `/not-in-org`, `/acl-denied`, `/room-archived`, `/device-key-revoked`, `/stale-room-version`, `/session-invalidated`. |
| V. JSON-Only Protocol | PASS for REST endpoints. **Exception** for the SSE push channel which uses `text/event-stream` per SSE spec — flagged in Complexity Tracking. |
| VI. Performance Requirements | PASS with caveats: ACL preview endpoint has its own p95 <500ms budget (higher than the <50ms default because it evaluates a draft rule set). SSE connections are long-lived and exempt from the p95 SLO (tracked separately). |

**Technical Constraints Check**:

- Authentication & Security: Per-user-session HMAC replaces the shared `API_SECRET`. Secrets in Fly secrets. HTTPS enforced by Fly at the edge.
- Dependencies: `tsnet` is new, vendored via Go modules. `golang-migrate` continues. Redis is NOT introduced (Postgres-backed sessions).
- Observability: slog + OTel continue. OTel exporter repointed at Grafana Cloud. Session invalidation and SSE event counts get dedicated counters.
- Development Workflow: the `just`-based workflow satisfies existing CI gates (lint, unit, integration) by exposing recipes with the same semantics as the retired Makefile targets.

## Project Structure

### Documentation (this feature)

```text
specs/003-multi-tenant-platform/
├── plan.md                         # This file
├── research.md                     # Phase 0 output
├── data-model.md                   # Phase 1 output
├── quickstart.md                   # Phase 1 output
├── contracts/
│   └── api-routes.md               # HTTP API surface
├── checklists/
│   └── requirements.md             # Existing (from spec phase)
├── spec.md                         # Existing
└── tasks.md                        # Phase 2 output (speckit.tasks)
```

### Source Code (repository root)

```text
cmd/
└── rescuestream-api/
    └── main.go                     # Unchanged entry point; wires new packages

internal/
├── config/                         # Extended: SUPER_ADMIN_EMAILS, DEVICE_KEY_PEPPER, MEDIAMTX_WEBHOOK_SECRET, TAILSCALE_AUTHKEY, SSE_MAX_CONNS
├── database/
│   ├── migrations/                 # NEW: 000003_v2_structure.up.sql, 000004_v2_backfill.up.sql, 000005_v2_constraints.up.sql (+ .down.sql)
│   ├── orgrepo/                    # NEW
│   ├── teamrepo/                   # NEW
│   ├── userrepo/                   # NEW
│   ├── membershiprepo/             # NEW
│   ├── superadminrepo/             # NEW
│   ├── tagrepo/                    # NEW
│   ├── devicerepo/                 # NEW (replaces broadcaster_repo.go)
│   ├── devicekeyrepo/              # NEW (replaces streamkey_repo.go)
│   ├── roomrepo/                   # NEW (includes ACL rules)
│   ├── sessionrepo/                # NEW (Postgres-backed sessions)
│   ├── streamrepo/                 # Modified: add org_id, room_id, device_id
│   └── auditrepo/                  # Modified: add org_id, actor_user_id
├── domain/
│   ├── org/                        # NEW: Organization entity + invariants
│   ├── team/                       # NEW
│   ├── user/                       # NEW
│   ├── membership/                 # NEW
│   ├── tag/                        # NEW
│   ├── device/                     # NEW (+ DeviceKey)
│   ├── room/                       # NEW (+ ACLRule, evaluator)
│   └── session/                    # NEW
├── service/
│   ├── org/                        # NEW — org lifecycle, admin management
│   ├── membership/                 # NEW — domain-match auto-join, role transitions
│   ├── tag/                        # NEW
│   ├── device/                     # NEW — device + key minting/rotation/revoke
│   ├── room/                       # NEW — includes optimistic concurrency, ACL preview
│   ├── acl/                        # NEW — pure evaluator (teams/tags/members, AND/OR)
│   ├── session/                    # NEW — server-side session store operations
│   ├── forcelogout/                # NEW — narrowly scoped action
│   ├── streamevents/               # NEW — SSE hub + event publish on stream ready/not-ready
│   ├── auditlog/                   # Modified — org scoping, new resource types
│   ├── auth/                       # Modified — peppered device-key verification, per-user HMAC
│   └── mediamtx/                   # Modified — dial over tsnet, bearer header on control-API calls
├── handler/
│   ├── org/                        # NEW
│   ├── team/                       # NEW
│   ├── membership/                 # NEW
│   ├── tag/                        # NEW
│   ├── device/                     # NEW
│   ├── room/                       # NEW
│   ├── session/                    # NEW (force-logout endpoint)
│   ├── streamevents/               # NEW (SSE endpoint)
│   ├── middleware/                 # Modified — per-user HMAC, tenancy enforcement, session validation
│   ├── auditlog/                   # Modified — org-scoped reads
│   ├── auth.go                     # Modified — peppered hash lookup
│   ├── webhook.go                  # Modified — bearer auth, org resolution via device key
│   └── broadcaster.go, streamkey.go  # DELETED
└── tsnet/                          # NEW — embedded Tailscale bootstrap

tests/
├── contract/                       # NEW — endpoint contract assertions against `contracts/api-routes.md`
├── integration/                    # Existing — extend with multi-tenant isolation tests, session invalidation, SSE event delivery
└── unit/                           # Existing — extend with ACL evaluator property tests, version-check tests

# Repo-root tooling
justfile                            # NEW — replaces Makefile (build, run, test, lint, fmt, migrate-local, migrate-local-down, migrate-create, migrate-prod, hooks, verify, clean)
Makefile                            # DELETED in the same PR that introduces the justfile
```

**Structure Decision**: single-project Go service. Existing `internal/{config,database,domain,service,handler,logging,lifecycle}` conventions are retained and extended with the new multi-tenant surface. The `broadcaster.go` / `streamkey.go` pairs (domain, repo, service, handler) are deleted wholesale — clean-break migration per spec FR-034. Repo-root build/test tooling moves from `Makefile` to `justfile` in a single change.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|-----------|-------------------------------------|
| `text/event-stream` content type on `GET /stream-events` (violates constitution principle V "JSON-Only Protocol") | SSE is the standard for one-way server→client push over HTTP; required by FR-027a and SC-013 (≤5s event delivery). Frontend (FR-008a) needs push semantics, not polling. | Alternatives considered: (a) WebSocket — full-duplex is overkill for stream-start/end notifications; more infra to test; (b) long polling — worse latency, more reconnect churn; (c) polling at 5s intervals — misses the SC-013 bar and wastes battery/bandwidth on mobile. |
| Long-lived HTTP connections exempt from the generic <50ms / <100ms p95 SLO | SSE connections remain open for minutes-to-hours; aggregate p95 of request duration would pollute the API's dashboards and make the SLO meaningless. | Tracked as a separate metric ("active SSE connections" + "event delivery latency") so the primary SLO stays meaningful without letting SSE connections go unmonitored. |

No other principle is violated. All new clients use functional options (II), all non-SSE responses are JSON (V), and the per-endpoint latency budgets either match or explicitly justify divergence from VI.

## Phase 0 / Phase 1 outputs

See:

- [research.md](research.md) — technology decisions, rationale, alternatives.
- [data-model.md](data-model.md) — tables, columns, indexes, constraints, invariants.
- [contracts/api-routes.md](contracts/api-routes.md) — HTTP endpoints with request/response shapes.
- [quickstart.md](quickstart.md) — developer onboarding and local loop.
