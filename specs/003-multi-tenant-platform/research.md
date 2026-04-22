# Research: Multi-Tenant Platform (API)

**Feature**: 003-multi-tenant-platform
**Phase**: 0
**Date**: 2026-04-21

## Scope

No `NEEDS CLARIFICATION` markers remain in `spec.md` — cross-cutting product decisions are recorded in `../../../rescuestream-frontend/specs/008-platform-v2/architecture.md`. This document covers the remaining technology-choice questions surfaced by the plan's Technical Context.

---

## 1. Device-key hashing scheme

**Decision**: HMAC-SHA256 with a server-side pepper, stored as a 32-byte lowercase hex string. Lookup is performed by computing `hmac(pepper, submitted_key_plaintext)` and indexing on the result.

**Rationale**:

- Device keys are high-entropy (generated as 32-byte crypto/rand values). The threat model is database compromise, not brute-forced weak secrets — so we do not need argon2/bcrypt work-factor protection.
- Deterministic hashing (HMAC) lets us index the hash column and do an O(log N) lookup on the mediamtx publish-auth hot path, where the budget is <50ms p95.
- A server-side pepper (kept as a Fly secret, never in the DB) means a database dump alone is insufficient to authenticate; the attacker needs both the DB and the pepper.
- The stdlib `crypto/hmac` + `crypto/sha256` has zero new dependencies.

**Alternatives considered**:

- **bcrypt or argon2 on the key**: incompatible with indexed lookup (would require scanning and comparing every active key per auth attempt). Rejected on latency.
- **Plaintext with DB-level encryption-at-rest only**: reliant entirely on the managed DB's key custody; a dump would still reveal plaintexts to anyone with DB access. Rejected on blast-radius.
- **Per-key salt**: adds a second column and prevents indexed lookup. Overkill for high-entropy random keys. Rejected.

**Pepper rotation**: store current and previous pepper in Fly secrets (`DEVICE_KEY_PEPPER` / `DEVICE_KEY_PEPPER_PREV`). Verify against both during a rotation window; rewrite hashes during a background rekey pass; remove previous after the window. Rotation is manual/operational; not on the hot path.

---

## 2. Per-user HMAC session-key model

**Decision**: Replace the single shared `API_SECRET` with per-user-session HMAC keys, minted on Google OAuth callback and persisted in the server-side session store (see §3). Existing request signature header set (`X-API-Key`, `X-Signature`, `X-Timestamp`) is preserved; the key identifier resolves to a session row from which the secret and user identity are loaded.

**Rationale**:

- Retains the proven HMAC-SHA256 signing, timestamp-drift check, and body-hashing logic already in `handler/middleware.go`. Minimal code change in the signature path.
- Binds every call to a specific user identity and session, enabling immediate invalidation on role change or force-logout (see §3) without changing the signature protocol.
- Frontend's Next.js server side (which is the only caller) holds the per-user secret just long enough for the session; no secret leaves the server.

**Alternatives considered**:

- **JWT access tokens**: common and tooling-rich, but stateless JWTs are hostile to server-side invalidation. Would require a denylist anyway, at which point we're close to stateful sessions.
- **OAuth bearer tokens alone**: pushes the identity+authorization burden onto Google's tokens; harder to add fine-grained session state (force-logout, role freshness).

**Header format** (unchanged from v1):

```
X-API-Key: <session_key_id>
X-Signature: <hex hmac-sha256(secret, METHOD "\n" PATH "\n" TIMESTAMP "\n" BODY)>
X-Timestamp: <unix_seconds>
X-User-Id: <user_uuid>
```

`X-User-Id` is redundant-but-useful: it's authoritatively derived from the session lookup, but emitting it makes log traces and trace spans more readable without a JOIN.

---

## 3. Server-side session store

**Decision**: Postgres table `sessions` in the same NeonDB instance. Rows carry id (uuid), user_id (fk), hmac_key_id (indexed unique), hmac_secret (hashed with the same pepper scheme as device keys), created_at, expires_at, last_used_at, revoked_at (nullable), revocation_reason (nullable). Admin-initiated invalidation is a single `UPDATE sessions SET revoked_at = now(), revocation_reason = $1 WHERE user_id = $2 AND revoked_at IS NULL`.

**Rationale**:

- One fewer moving piece than adding Redis/Upstash. NeonDB's free tier is ample at our scale.
- Durable across Fly machine restarts (a Redis alternative would need persistence configured).
- The hot path for session validation is already touching Postgres (membership lookup) on most authenticated requests; adding a `sessions` JOIN is cheap with proper indexing.
- Admin-initiated invalidation is a one-query operation with predictable latency (SC-011 bar of <5s is generous).

**Alternatives considered**:

- **Redis (via Fly Redis or Upstash)**: faster reads but introduces a second stateful service, new failure modes, and another secret to rotate. Reconsider if session-validation latency becomes a bottleneck at 10x current scale.
- **Stateless JWT + denylist**: see §2.

**Validation hot path**: on every authenticated request, the middleware looks up `sessions` by hmac_key_id, asserts not-revoked and not-expired, verifies the HMAC against the stored secret, then proceeds. Expected latency: <5ms on a warm Neon pool.

**Expiry**: 30-day sliding window (`expires_at = last_used_at + 30 days`) with rolling refresh on each authenticated request. Aligns with typical web-app session hygiene.

**Eviction**: a background sweeper deletes rows where `expires_at < now() - 7 days` (keep a week of tombstones for post-mortem / audit correlation).

---

## 4. SSE hub design

**Decision**: In-process pub/sub hub (map of user_id → set of `chan Event`), one hub per API process. On publish, the hub filters events through an ACL check for each subscriber before delivering.

**Rationale**:

- Single-region Fly deployment (per plan) with 1–3 machines at our scale. A single-process hub per machine is sufficient as long as each client holds its SSE connection to a single machine for the duration.
- Fly's built-in "sticky session" for HTTP is not needed for SSE — the client's TCP connection is already pinned to a machine for the connection's lifetime. New connections may land on any machine; all machines see all publish events because they all subscribe to the same source (`webhook/ready` / `webhook/not-ready` handlers).
- Actually: per-machine hubs only see events that land on *their* machine. To fan out cross-machine, we need either a DB NOTIFY/LISTEN or a lightweight Redis pub/sub.

**Refined decision**: use Postgres LISTEN/NOTIFY as the cross-machine fan-out.

- When `/webhook/ready` or `/webhook/not-ready` lands on any machine, the handler issues `NOTIFY stream_events, '<json_event>'`.
- Every machine runs a dedicated goroutine with `LISTEN stream_events` on a non-pooled Neon connection; it pushes the received event into its in-process hub.
- Per-connection SSE goroutine reads from its channel, filters by ACL (recomputed per publish — cheap — or cached with short TTL if needed), and writes to the flushing response.

**Alternatives considered**:

- **Redis pub/sub**: requires a new managed service; we've already decided against Redis for sessions (§3) on the same grounds. Reconsider if Postgres NOTIFY becomes a bottleneck (payload size limit ~8 KB — our events are ~200 bytes, lots of headroom).
- **Single-machine deployment**: sidesteps fan-out but loses Fly's auto-scale. Reject.

**Connection lifecycle**:

- 30s heartbeat comment (`: ping\n\n`) from server to keep proxies alive.
- Graceful shutdown on SIGTERM: close all SSE channels with a `data: {"type":"bye"}` event so clients reconnect cleanly.
- No historical replay: FR-027a's edge case explicitly says the channel does not replay history.

---

## 5. mediamtx integration over Tailscale

**Decision**: embed `tailscale.com/tsnet` into the API process. On startup, `tsnet.Server` brings up a virtual interface with an ephemeral auth key (from Fly secret `TAILSCALE_AUTHKEY`) tagged `tag:fly-api`. Outbound HTTP to mediamtx's control API and inbound HTTP for webhooks both traverse the tailnet.

**Rationale**:

- `tsnet` avoids installing the `tailscaled` daemon in the container (which is awkward on Fly's rootless execution model).
- The tailnet hostname (`rescuestream-api.<tailnet>.ts.net` and `rescuestream-mediamtx.<tailnet>.ts.net`) replaces public URLs for server-to-server calls. The public API URL (`api.rescue.stream`) remains for the frontend.
- Webhook bearer secret (`MEDIAMTX_WEBHOOK_SECRET`) is still required even inside the tailnet — defense in depth against a compromised VM or Fly machine.

**Alternatives considered**:

- **Public HTTPS with allowlist**: Fly egress IPs rotate; IP allowlisting is fragile.
- **WireGuard tunnel managed manually**: more moving parts, no tag-based ACLs.

---

## 6. Multi-tenant isolation pattern

**Decision**: `org_id` column on every tenant-scoped table, with a repository-layer helper that requires an `orgID uuid.UUID` parameter for every query (not retrievable from handler context alone). Handler middleware loads the caller's org into context; service layer asserts resource.org_id matches caller.org_id before the query executes.

**Rationale**:

- Explicit scoping at the service boundary catches accidental cross-tenant reads at test time (a repo method that accepts org_id but isn't given one will fail to compile).
- Row-level security (RLS) is an appealing alternative but Postgres RLS is easy to mis-configure under connection pooling (pgx/Neon's session variables can leak between requests sharing a pooled connection). Skip for v1; consider for a hardening pass.
- Super-admin endpoints use separate repository methods that explicitly skip the org_id filter and are only called from super-admin-gated handlers.

**Alternatives considered**:

- **Postgres RLS**: see above — revisit after the first year of operation.
- **Schema-per-tenant**: operationally heavy at our scale; migrations become tricky; abandon.

---

## 7. Room optimistic concurrency

**Decision**: add a `version bigint NOT NULL DEFAULT 1` column to `rooms`. Every mutation of the room (metadata, scope, ACL replacement, archive state) executes `UPDATE rooms SET ..., version = version + 1 WHERE id = $1 AND version = $2` and uses `RowsAffected()` to detect a stale-version conflict. On conflict, return an RFC 9457 `stale-room-version` problem with the current version in `instance` metadata.

**Rationale**:

- Simplest possible optimistic-concurrency model; no trigger, no timestamp-comparison pitfalls.
- Predictable semantics for the frontend's "stale-version, refresh" message.
- ACL-rule-set updates piggyback on the same version bump (since the rule set replacement is a single logical mutation).

---

## 8. ACL evaluator

**Decision**: implement a pure Go evaluator, independent of the DB, that takes a normalized rule set + combinator and a user's attributes (org_id, team_id, tag_ids) and returns allow/deny. Used both at request time (access check against an existing room ACL) and at preview time (FR-024b — count members the rule set admits).

**Rationale**:

- Pure evaluator is trivial to test — property-based tests (via a small homegrown generator or `testing/quick`) sweep boolean corners.
- The preview endpoint loads the affected members' attributes once (`SELECT user_id, team_id, tags FROM members_with_tags WHERE org_id = $1`) and runs the evaluator in memory. For 500 members × a handful of rules, this is well under the 500ms p95 (SC-012).

**Materialized view or view**: `members_with_tags` as a simple view over `organization_memberships` LEFT JOIN `user_tag_assignments` grouped by user. No materialization needed at our scale.

**Evaluator API**:

```go
type Attrs struct {
    UserID  uuid.UUID
    TeamID  *uuid.UUID  // nil for org-admins without a team
    TagIDs  []uuid.UUID
}

type Rule struct {
    Type   string    // "team", "tag", "user"
    Target uuid.UUID
}

type RuleSet struct {
    Combinator string // "and" | "or"
    Rules      []Rule
}

func Evaluate(rs RuleSet, a Attrs) bool
```

Access check for a room:

1. If caller is super-admin → allow.
2. If caller is org-admin of room.org_id → allow.
3. If room.scope = "team" and a.TeamID != room.team_id → deny.
4. If rs is empty → allow (public within scope).
5. Else `Evaluate(rs, a)`.

---

## 9. Audit log extension

**Decision**: extend the existing `audit_logs` table with `org_id uuid NULL` (NULL means platform-level event by a super-admin) and `actor_user_id uuid NULL` (NULL only when the actor can't be resolved — a migration edge case). Add a partial index `(org_id, timestamp DESC)` for the common org-admin retrieval path.

**Action vocabulary** (extended from 007-audit-logging): add verbs `org.created`, `org.suspended`, `team.created`, `team.domain_set`, `membership.granted`, `membership.revoked`, `tag.created`, `tag.assigned`, `tag.revoked`, `device.created`, `device_key.minted`, `device_key.revoked`, `room.created`, `room.archived`, `room.acl_replaced`, `session.revoked`, `session.force_revoked`, `super_admin.granted`, `super_admin.revoked`.

**Before/after state**: for mutations where the prior state is non-trivial (ACL replacement, room metadata edit), `metadata` carries `{"before": {...}, "after": {...}}`. Keys rely on existing JSON redaction if anything sensitive (device key plaintext — never) ever enters.

---

## 10. Migration strategy (v1 → v2 schema)

**Decision**: three forward migrations (`000003_v2_structure.up.sql`, `000004_v2_backfill.up.sql`, `000005_v2_constraints.up.sql`) in that order. Down migrations drop new structure but do not attempt to reconstruct `broadcasters`/`stream_keys` — the clean-break migration is explicitly one-way (spec FR-034).

**000003_v2_structure**: create all new tables with `org_id` columns nullable initially; seed the default organization (name "RescueStream Default", slug "default"); seed super-admins from `SUPER_ADMIN_EMAILS`; insert a `users` row for each distinct `audit_logs.actor` value (so audit history has an actor FK target).

**000004_v2_backfill**: update all existing `audit_logs` rows to `org_id = <default org>` and `actor_user_id = <matching users.id>`; drop `broadcasters` and `stream_keys` tables (per FR-034); remove FK from `streams` to `stream_keys`.

**000005_v2_constraints**: flip `org_id` to `NOT NULL` on `audit_logs`, `streams`, and other extended tables; add `CHECK` constraints for polymorphic `room_acl_rules.target_id` (present in the correct target table depending on type); add the new indexes.

**Run-mode (cutover and all subsequent releases)**: migrations are executed by the operator via `just migrate-prod`, not automatically on API boot. Enforcement lives in the justfile recipe (see §12), which additionally refuses to run against Neon's pooled hostname. Rationale:

- `golang-migrate` uses Postgres advisory locks, which require a session-scoped (non-pooled) connection; a pooled connection can silently break locking semantics.
- A failed migration blocks the deploy step (and is loud) rather than crash-looping the API container after a rollout and forcing a revert under pressure.
- Keeps cutover and every subsequent release on the same one-command path — no special-case for "the first migration ever."

The CI/deploy flow therefore looks like: (a) ship the image, (b) `just migrate-prod` from a trusted operator workstation (or a short-lived `flyctl ssh console` session), (c) `flyctl deploy` rolls the app machines. A successful migration is a precondition for rolling the app; a failure stops the operator before any machine is replaced.

---

## 11. Observability for new surfaces

**Decision**: add OpenTelemetry metrics for:

- `sse_active_connections` (gauge, labels: `org_id`).
- `sse_event_delivery_latency_seconds` (histogram, labels: `event_type`).
- `session_validations_total` (counter, labels: `outcome = ok|revoked|expired|invalid_signature`).
- `session_invalidations_total` (counter, labels: `reason`).
- `acl_preview_duration_seconds` (histogram, labels: `org_size_bucket`).
- `room_version_conflicts_total` (counter, labels: `org_id`).
- `device_auth_duration_seconds` (histogram, labels: `outcome`).

All emit to the existing OTel pipeline, which is being repointed from self-hosted LGTM to Grafana Cloud (deployment-side concern; see ansible migration doc).

---

## 12. Task runner and local-dev database

**Decision**: Replace the existing `Makefile` with a repo-root `justfile` and standardize local development / automated tests on Docker Postgres 15 (via `docker-compose` for the dev loop; via `testcontainers-go` for test runs). Neon branches are not used for the contributor loop.

**Rationale**:

- `just` gives us stronger recipe composition (dependencies, parameters, per-recipe shells) without inheriting `make`'s tab-sensitivity and implicit-rule footguns.
- Neon is Postgres 15+ wire-compatible, so the Docker-local ↔ Neon-prod schema/behavior delta is small as long as we avoid Neon-specific SQL features (none are needed for this feature).
- `testcontainers-go` is already the mandated integration-test harness per constitution principle III; reusing the same Docker image in the contributor loop keeps the two environments aligned.
- A Neon-branch-per-contributor model adds account coupling, billing, and offline-unfriendliness — all hostile to fast onboarding at our scale.

**Justfile recipe inventory** (initial set; mirrors retired Makefile targets):

```just
build                 # go build -o bin/rescuestream-api ./cmd/rescuestream-api
run                   # go run ./cmd/rescuestream-api
test                  # go test -v -race -coverprofile=coverage.out ./...
test-unit             # go test -v -race ./internal/...
test-integration      # go test -v -race ./tests/integration/...
test-contract         # go test -v -race ./tests/contract/...
lint                  # golangci-lint run ./...
fmt                   # go fmt ./... && goimports -w -local github.com/searchandrescuegg/rescuestream-api .
migrate-local         # go run ./cmd/migrate up   (reads DATABASE_URL; Docker Postgres)
migrate-local-down    # go run ./cmd/migrate down
migrate-create name   # migrate create -ext sql -dir internal/database/migrations -seq {{name}}
migrate-prod          # guarded: refuses unless DATABASE_MIGRATION_URL is set AND is a Neon non-pooled host
hooks                 # git config --local core.hooksPath .githooks/
verify                # lint + test
clean                 # rm -rf bin/ coverage.out
setup                 # npm install -g @commitlint/config-conventional @commitlint/cli
```

**`migrate-prod` guard** (shell pseudocode embedded in the recipe):

```sh
: "${DATABASE_MIGRATION_URL:?DATABASE_MIGRATION_URL must be set for prod migrations}"
case "$DATABASE_MIGRATION_URL" in
  *-pooler.*neon.tech*)
    echo "refusing: DATABASE_MIGRATION_URL points at a Neon pooled endpoint; use the non-pooled host"; exit 2 ;;
  *neon.tech*) ;;
  *) echo "refusing: DATABASE_MIGRATION_URL is not a neon.tech host"; exit 2 ;;
esac
DATABASE_URL="$DATABASE_MIGRATION_URL" go run ./cmd/migrate up
```

The guard is deliberately mechanical (exit-code based) so CI and operator shells fail the same way.

**Alternatives considered**:

- **Keep the Makefile**: lowest churn, but leaves us with `make`'s whitespace/POSIX-shell quirks and no natural place to put the non-trivial `migrate-prod` guard without drifting into bash-heredoc territory inside `.PHONY` targets.
- **Bash scripts under `scripts/`**: workable but loses the discoverability of `just --list`; contributors still default to `make` / `just` muscle memory.
- **Neon branch per contributor for local dev**: highest prod parity, but adds account/billing coupling, breaks offline work, and complicates testcontainers (which doesn't plug into Neon). Revisit only if Postgres-Docker drift causes real bugs.

---

## Resolved — no further clarifications required

All twelve decisions above are implementation-ready. Any remaining "how to structure the X package" choices are deferred to the implementation phase (speckit.tasks / speckit.implement) and do not block Phase 1 (data-model.md, contracts/, quickstart.md).
