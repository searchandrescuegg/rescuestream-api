# Quickstart: Multi-Tenant Platform (API)

**Feature**: 003-multi-tenant-platform
**Audience**: Contributors running the API locally for development against the v2 surface.

---

## 1. Prerequisites

- Go 1.25+
- Docker (for `docker compose up` — local Postgres 15 stand-in for NeonDB; also used by `testcontainers-go` in tests)
- `just` (task runner; replaces the previous `make`-based workflow). Install with `brew install just` or see <https://just.systems>.
- An account on Google Cloud with an OAuth 2.0 client configured for `http://localhost:3000/api/auth/callback/google` (for end-to-end tests with the frontend)

Do not install `tailscaled` locally. `tsnet` is embedded and its local mode can be short-circuited in dev (see §5).

---

## 2. Environment variables (dev)

Create `.env` in repo root (gitignored):

```
# Core
API_PORT=8080
DATABASE_URL=postgres://postgres:postgres@localhost:5432/rescuestream?sslmode=disable
LOG_LEVEL=debug
LOCAL=true

# Super-admin bootstrap (FR-005)
SUPER_ADMIN_EMAILS=you@example.com,teammate@example.com

# Secrets (generate locally with: openssl rand -base64 32)
DEVICE_KEY_PEPPER=<32+ bytes base64>
SESSION_SECRET_PEPPER=<32+ bytes base64>
MEDIAMTX_WEBHOOK_SECRET=<32+ bytes base64>

# mediamtx integration (local dev uses docker-compose default; Tailscale disabled)
MEDIAMTX_API_URL=http://mediamtx:9997
MEDIAMTX_PUBLIC_URL=http://localhost:8889
TAILSCALE_ENABLED=false

# OTel (can skip in dev)
METRICS_ENABLED=false
TRACING_ENABLED=false
```

---

## 3. Bring the stack up

```bash
docker compose up -d postgres mediamtx
just migrate-local  # runs golang-migrate forward through 000005 against DATABASE_URL (Docker Postgres)
just run            # starts the API on :8080
```

> Production migrations use a separate recipe, `just migrate-prod`, which reads `DATABASE_MIGRATION_URL` (Neon non-pooled host) and refuses pooled (`-pooler.neon.tech`) hostnames or non-Neon hosts. Never set `DATABASE_MIGRATION_URL` in `.env` — it belongs only in the operator's deploy shell.

The first `just migrate-local` after pulling this feature seeds:

- The default organization (`slug: default`).
- Super-admin rows for each email in `SUPER_ADMIN_EMAILS`.
- User placeholder rows for any historical audit-log actors (non-interactive; no email sent).

Subsequent migrations are idempotent.

---

## 4. Smoke test: super-admin provisions an org

```bash
# 1. Log in via the frontend on http://localhost:3000 with a super-admin email.
#    The frontend exchanges the Google id_token for a session key pair
#    via POST /sessions/login-complete; the resulting session is httpOnly.

# 2. From the frontend's super-admin console, create an organization:
#    name="KCSARA"  slug="kcsara"  initial_admin_emails=["alice@kcesar.org"]

# 3. Add a team under that org:
#    name="KCESAR"  workspace_domain="kcesar.org"

# 4. Sign out. Sign back in as alice@kcesar.org (Workspace domain matches) →
#    lands on the KCSARA org dashboard as a member of KCESAR.

# 5. Promote alice to org-admin (via super-admin only, POST /orgs/{kcsara}/admins).
#    alice's existing sessions are invalidated; on next request she re-authenticates
#    and lands in the admin console.
```

## 5. Smoke test: device registration + stream ingest

```bash
# As alice (org-admin of KCSARA):
# 1. Navigate to Devices → New Device. Name="Drone 03", owner=alice, description.
# 2. Capture the primary key shown exactly once in the reveal modal.
# 3. Create a room (scope=org, ACL=empty for simplicity).
# 4. Point a test RTMP source at rtmp://localhost:1935/<room_slug> with the
#    captured key as the stream name (or as ?key=... depending on chosen form,
#    per research §X — decided at implementation time).
# 5. Verify in the room view that the stream appears within 5s.
# 6. Verify the stream-events SSE channel receives a stream.started event
#    for the room (open DevTools → Network → EventStream on /stream-events).
# 7. Stop the source. Verify stream.ended event within 5s.
```

## 6. Smoke test: optimistic concurrency

```bash
# 1. Open the same room's ACL editor in two browser tabs (as alice).
# 2. In tab A: add a rule (team=KCESAR), save. Version bumps from 1 → 2.
# 3. In tab B (still holding version=1): attempt to save different rules.
# 4. Tab B MUST receive a 409 with problem type .../problems/stale-room-version
#    and instance.metadata.current_version = 2.
# 5. Tab B refreshes, sees the new state, can retry.
```

## 7. Smoke test: force-logout

```bash
# 1. Alice signs in in browser A. Session is active.
# 2. From browser B (as another org-admin or super-admin), call
#    POST /orgs/{kcsara}/members/{alice}/revoke-sessions.
# 3. Browser A's next API call (within 5s) MUST return 401 with
#    problem type .../problems/session-invalidated; the frontend MUST
#    redirect to sign-in.
```

---

## 8. Running tests

```bash
just test              # unit + integration (testcontainers spins up Postgres)
just test-unit         # unit only
just test-integration  # integration only (requires Docker)
just test-contract     # HTTP contract tests against contracts/api-routes.md
```

Contract tests exercise every endpoint in §1–13 of `contracts/api-routes.md` with:

- Happy-path assertions (status code, response shape).
- Authorization denials for the wrong role (super-admin vs org-admin vs member).
- Tenancy denials across orgs.
- Idempotency where applicable.

---

## 9. Common local-dev tips

- **Reset DB**: `just migrate-local-down` then `just migrate-local`. Dev DB only.
- **List recipes**: `just --list` (or just `just`) prints every available recipe with its one-line summary.
- **Create a test user without Google OAuth**: seed `users`, `organization_memberships`, and `sessions` manually via `psql`; then hit the API with manually-signed HMAC requests. Only for backend-only debugging.
- **Tail audit log**: `psql $DATABASE_URL -c "SELECT action, resource_type, actor_user_id, timestamp FROM audit_logs ORDER BY timestamp DESC LIMIT 20;"`
- **Inspect current sessions**: `SELECT id, user_id, revoked_at, revoked_reason FROM sessions WHERE user_id = '<user_uuid>' ORDER BY created_at DESC;`

---

## 10. What this quickstart does NOT cover

- Fly.io deploy (see `fly.toml` when implemented; `flyctl deploy` from repo root).
- NeonDB production branching (handled by the ops playbook, not local dev).
- Tailscale enrollment (production-only; dev uses plain HTTP to `mediamtx:9997`).
- Grafana Cloud OTel export (local dev uses stdout exporters).
