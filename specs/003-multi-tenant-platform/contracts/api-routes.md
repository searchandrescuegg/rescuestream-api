# HTTP API Contracts: Multi-Tenant Platform

**Feature**: 003-multi-tenant-platform
**Phase**: 1
**Date**: 2026-04-21

## Conventions

**Authentication**: every endpoint (except the mediamtx webhooks and `/health`) requires per-user-session HMAC (see research §2). Required headers on authenticated requests:

```
X-API-Key:    <session_key_id>     (16 bytes base32)
X-Signature:  <hex hmac-sha256(secret, METHOD "\n" PATH "\n" TIMESTAMP "\n" BODY)>
X-Timestamp:  <unix_seconds>       (±5 min drift window)
X-User-Id:    <user_uuid>
```

**Authorization column values** below:

- `super-admin` — caller must be in `super_admins`.
- `org-admin(org)` — caller is org-admin of the target org (resource's org).
- `member(org)` — caller is a member of the target org (includes org-admin).
- `any-signed-in` — caller has a valid session; no org requirement.
- `none` — public (mediamtx webhooks + health).

**Content types**: JSON for all REST endpoints (per constitution V); SSE endpoint uses `text/event-stream`.

**Error responses**: RFC 9457 problem details (`application/problem+json`) on every non-2xx. New `type` URIs used by this feature:

- `https://rescue.stream/problems/no-org-membership`
- `https://rescue.stream/problems/not-in-org`
- `https://rescue.stream/problems/acl-denied`
- `https://rescue.stream/problems/room-archived`
- `https://rescue.stream/problems/stale-room-version` (409; `instance` metadata carries `current_version`)
- `https://rescue.stream/problems/device-key-revoked`
- `https://rescue.stream/problems/session-invalidated`
- `https://rescue.stream/problems/workspace-domain-taken`
- `https://rescue.stream/problems/last-super-admin`

**Date/time**: RFC 3339 timestamps with `Z` suffix.

**Pagination**: cursor-style for list endpoints — `?limit=<n>&cursor=<opaque>`; responses include `next_cursor` (null when exhausted).

---

## 1. Health & mediamtx webhooks (unchanged shape; modified internals)

### `GET /health`

**Authz**: none. **Response**: `200 { "status": "ok", "database": "ok" }`.

### `POST /auth`

**Called by**: mediamtx. **Authz**: bearer header `Authorization: Bearer <MEDIAMTX_WEBHOOK_SECRET>`.

**Request** (unchanged mediamtx JSON):

```json
{
  "user": "...",
  "password": "<device_key_plaintext>",
  "ip": "...",
  "action": "publish|read|playback",
  "path": "<room_slug>",
  "protocol": "rtmpConn|rtspSession|...",
  "id": "...",
  "query": "..."
}
```

**Response**: `200` (allow) | `401` (deny). No body.

**Internal change from v1**: lookup moves from `stream_keys.key_value = ?` to `device_keys.key_hash = hmac(pepper, password)` + validate room existence / lifecycle / device-policy.

### `POST /webhook/ready`

**Called by**: mediamtx. **Authz**: bearer. **Request**: `{"path":"...","source_type":"...","source_id":"..."}`. **Response**: 204. **Effect**: insert `streams` row; emit `stream.started` event via Postgres NOTIFY (research §4).

### `POST /webhook/not-ready`

**Called by**: mediamtx. **Authz**: bearer. **Request**: `{"path":"..."}`. **Response**: 204. **Effect**: end `streams` row; emit `stream.ended` event.

---

## 2. Organizations

### `POST /orgs`

| | |
|---|---|
| Authz | super-admin |
| Request | `{ "name": "...", "slug": "...", "initial_admin_emails": ["..."] }` |
| Response | `201 { "id": "...", "name": "...", "slug": "...", "status": "active", "created_at": "..." }` |
| Errors | `workspace-domain-taken` (409) if any initial admin's email domain collides with an existing team domain in a *different* org |

### `GET /orgs`

| | |
|---|---|
| Authz | super-admin |
| Query | `?limit=&cursor=&status=&q=` (typeahead search on name/slug) |
| Response | `200 { "orgs": [...], "next_cursor": "..." }` |

### `GET /orgs/{org_id}`

| | |
|---|---|
| Authz | super-admin OR org-admin(org_id) |
| Response | `200 { "id":..., "name":..., "slug":..., "status":..., "counts":{"teams":N,"members":N,"admins":N,"devices":N,"rooms":N}, "created_at":... }` |

### `PATCH /orgs/{org_id}`

| | |
|---|---|
| Authz | super-admin OR org-admin(org_id) (name only; super-admin for slug/status changes) |
| Request | `{ "name": "...", "slug": "...", "status": "active|suspended" }` |
| Response | `200 <org>` |

### `DELETE /orgs/{org_id}`

| | |
|---|---|
| Authz | super-admin |
| Response | `204` |
| Effect | cascades: archives all active rooms, force-revokes all sessions for the org's members, then hard-deletes dependent rows per FK cascade. Emits `org.deleted` audit at platform scope. |

### `POST /orgs/{org_id}/admins`

| | |
|---|---|
| Authz | super-admin |
| Request | `{ "email": "..." }` |
| Response | `201 { "user_id": "...", "organization_id": "...", "role": "org-admin", "joined_at": "..." }` |

### `DELETE /orgs/{org_id}/admins/{user_id}`

| | |
|---|---|
| Authz | super-admin |
| Response | `204` |
| Effect | downgrades membership to `member` (if the user also has a team match) or removes membership entirely. Revokes all sessions. |

---

## 3. Super-admins

### `GET /super-admins`

| | |
|---|---|
| Authz | super-admin |
| Response | `200 { "super_admins": [{"user_id":...,"email":...,"granted_at":...,"seeded_from_env":bool}] }` |

### `POST /super-admins`

| | |
|---|---|
| Authz | super-admin |
| Request | `{ "email": "..." }` |
| Response | `201 <super_admin>` |

### `DELETE /super-admins/{user_id}`

| | |
|---|---|
| Authz | super-admin |
| Response | `204` |
| Errors | `last-super-admin` (409) if this would leave zero super-admins |

---

## 4. Teams

### `POST /orgs/{org_id}/teams`

| | |
|---|---|
| Authz | org-admin(org_id) |
| Request | `{ "name": "...", "workspace_domain": "example.org" }` |
| Response | `201 <team>` |
| Errors | `workspace-domain-taken` (409) |

### `GET /orgs/{org_id}/teams`

| | |
|---|---|
| Authz | member(org_id) |
| Response | `200 { "teams": [<team>...] }` |

### `PATCH /teams/{team_id}`

| | |
|---|---|
| Authz | org-admin of the team's org |
| Request | `{ "name": "...", "workspace_domain": "..." }` |
| Response | `200 <team>` |

### `DELETE /teams/{team_id}`

| | |
|---|---|
| Authz | org-admin of the team's org |
| Response | `204` |
| Effect | cascades: memberships in this team are removed; affected members lose org access on next request |

---

## 5. Members (memberships)

### `GET /orgs/{org_id}/members`

| | |
|---|---|
| Authz | org-admin(org_id) |
| Query | `?limit=&cursor=&team_id=&tag_id=&q=` |
| Response | `200 { "members": [{"user_id":...,"email":...,"display_name":...,"team_id":...,"role":...,"tag_ids":[...]}], "next_cursor": "..." }` |

### `GET /orgs/{org_id}/members/{user_id}`

| | |
|---|---|
| Authz | org-admin(org_id) OR self (a member viewing themselves) |
| Response | `200 <member>` |

### `DELETE /orgs/{org_id}/members/{user_id}`

| | |
|---|---|
| Authz | org-admin(org_id) |
| Response | `204` |
| Effect | removes membership + tag assignments; revokes all sessions (FR-030a). |

### `POST /orgs/{org_id}/members/{user_id}/revoke-sessions`

| | |
|---|---|
| Authz | org-admin(org_id) |
| Response | `202 { "revoked_count": N }` |
| Effect | force-logout per FR-030b. |

---

## 6. Tags

### `POST /orgs/{org_id}/tags`

| | |
|---|---|
| Authz | org-admin(org_id) |
| Request | `{ "key": "drone-pilot", "label": "Drone Pilot", "description": "..." }` |
| Response | `201 <tag>` |

### `GET /orgs/{org_id}/tags`

| | |
|---|---|
| Authz | member(org_id) |
| Response | `200 { "tags": [<tag>...] }` |

### `PATCH /tags/{tag_id}`

| | |
|---|---|
| Authz | org-admin of the tag's org |
| Request | `{ "label": "...", "description": "..." }` (key is immutable) |
| Response | `200 <tag>` |

### `DELETE /tags/{tag_id}`

| | |
|---|---|
| Authz | org-admin of the tag's org |
| Response | `204` |
| Effect | cascades: removes all user_tag_assignments and all room_acl_rules with type='tag' and target_id=this tag, bumping affected rooms' versions. |

### `POST /orgs/{org_id}/members/{user_id}/tags`

| | |
|---|---|
| Authz | org-admin(org_id) |
| Request | `{ "tag_id": "..." }` |
| Response | `201 <user_tag_assignment>` |

### `DELETE /orgs/{org_id}/members/{user_id}/tags/{tag_id}`

| | |
|---|---|
| Authz | org-admin(org_id) |
| Response | `204` |

---

## 7. Devices

### `POST /orgs/{org_id}/devices`

| | |
|---|---|
| Authz | org-admin(org_id) |
| Request | `{ "name": "...", "description": "...", "owner_user_id": "... (optional)", "metadata": {} }` |
| Response | `201 { "device": <device>, "primary_key": { "id": "...", "plaintext": "<SHOW-ONCE>" } }` |
| Notes | `plaintext` is included only in this response; never in any subsequent GET, list, or audit entry. |

### `GET /orgs/{org_id}/devices`

| | |
|---|---|
| Authz | member(org_id) (read-only view for context; org-admin gets management surface) |
| Query | `?limit=&cursor=&status=&q=` |
| Response | `200 { "devices": [{"id":...,"name":...,"owner_user_id":...,"key_status":"active|rotating|revoked","last_seen_at":...}], "next_cursor": "..." }` |

### `GET /devices/{device_id}`

| | |
|---|---|
| Authz | member of the device's org |
| Response | `200 { "device": <device>, "keys": [{"id":...,"slot":"primary|secondary","status":"active|revoked","created_at":...,"last_used_at":...}] }` |

### `PATCH /devices/{device_id}`

| | |
|---|---|
| Authz | org-admin of the device's org |
| Request | `{ "name": "...", "description": "...", "owner_user_id": "...", "metadata": {} }` |
| Response | `200 <device>` |

### `DELETE /devices/{device_id}`

| | |
|---|---|
| Authz | org-admin of the device's org |
| Response | `204` |

### `POST /devices/{device_id}/keys/rotate`

| | |
|---|---|
| Authz | org-admin of the device's org |
| Request | `{ "slot": "secondary" }` (default) |
| Response | `201 { "id": "...", "slot": "secondary", "plaintext": "<SHOW-ONCE>" }` |
| Effect | creates a new active key in the target slot. If a key already exists in that slot, the call is rejected with 409 (caller must revoke the prior slot first). |

### `DELETE /devices/{device_id}/keys/{key_id}`

| | |
|---|---|
| Authz | org-admin of the device's org |
| Response | `204` |

---

## 8. Rooms

### `POST /orgs/{org_id}/rooms`

| | |
|---|---|
| Authz | org-admin(org_id) |
| Request | `{ "name": "...", "description": "...", "scope": "org|team", "team_id": "... (required when scope=team)", "archive_after_days": 30, "default_device_policy": "any-org-device|allowlist", "acl": { "combinator": "and|or", "rules": [{"type":"team","target_id":"..."}, {"type":"tag","target_id":"..."}, {"type":"user","target_id":"..."}] }, "device_allowlist": ["device_id", ...] }` |
| Response | `201 <room>` |

### `GET /orgs/{org_id}/rooms`

| | |
|---|---|
| Authz | member(org_id). Response is filtered to rooms the caller can access. |
| Query | `?limit=&cursor=&lifecycle_state=active|archived|all&scope=` |
| Response | `200 { "rooms": [<room>...], "next_cursor": "..." }` |

### `GET /rooms/{room_id}`

| | |
|---|---|
| Authz | user who has access per the evaluator in research §8 |
| Response | `200 { "room": <room>, "active_streams": [...], "version": N }` |

### `PATCH /rooms/{room_id}`

| | |
|---|---|
| Authz | org-admin of the room's org |
| Request | `{ "expected_version": N, "name": "...", "description": "...", "archive_after_days": N, "default_device_policy": "...", "acl": {...}, "device_allowlist": [...] }` |
| Response | `200 <room>` (with new `version`) |
| Errors | `stale-room-version` (409) when `expected_version != current`; `instance.metadata = { "current_version": N }` |

### `POST /rooms/{room_id}/archive`

| | |
|---|---|
| Authz | org-admin of the room's org |
| Request | `{ "expected_version": N }` |
| Response | `200 <room>` |

### `POST /rooms/{room_id}/unarchive`

| | |
|---|---|
| Authz | org-admin of the room's org |
| Request | `{ "expected_version": N }` |
| Response | `200 <room>` |

### `POST /orgs/{org_id}/rooms/acl-preview`

| | |
|---|---|
| Authz | org-admin(org_id) |
| Request | `{ "scope": "org|team", "team_id": "... (if scope=team)", "acl": { "combinator": "and|or", "rules": [...] } }` |
| Response | `200 { "match_count": N, "sample_members": [{"user_id":...,"email":...}] }` (sample capped at e.g. 20) |
| Notes | Does not mutate state. Drives FR-024b + SC-012 (<500 ms p95 at 500-member orgs). |

---

## 9. Streams

### `GET /rooms/{room_id}/streams`

| | |
|---|---|
| Authz | user who has access to the room |
| Query | `?status=active|all&limit=&cursor=` |
| Response | `200 { "streams": [{"id":...,"device_id":...,"path":...,"status":...,"started_at":...,"ended_at":...,"urls":{"hls":"...","webrtc":"..."}}], "next_cursor":"..." }` |

### `GET /streams/{stream_id}`

| | |
|---|---|
| Authz | user with access to the stream's room |
| Response | `200 <stream>` |

---

## 10. Stream-status push

### `GET /stream-events` (SSE)

| | |
|---|---|
| Authz | any-signed-in |
| Response | `200 text/event-stream` — long-lived |
| Events | `stream.started` / `stream.ended` — see below |

**Event format** (SSE frames):

```
event: stream.started
id: <monotonic>
data: {"room_id":"...","stream_id":"...","device_id":"...","timestamp":"..."}

event: stream.ended
id: <monotonic>
data: {"room_id":"...","stream_id":"...","device_id":"...","timestamp":"..."}
```

**Delivery guarantees**: per research §4 — at-most-once within the lifetime of a single connection; no replay after reconnect. Filtered per caller's room access on every publish.

**Heartbeat**: `:\n\n` comment every 30 s to keep proxies alive.

---

## 11. Sessions

### `POST /sessions/login-complete`

| | |
|---|---|
| Authz | none (body authenticated by server-side OAuth callback token) |
| Purpose | frontend server-side exchanges a Google OAuth token for a session key pair |
| Request | `{ "id_token": "<google id_token>" }` |
| Response | `201 { "session_key_id": "...", "session_secret": "<SHOW-ONCE>", "user": {...}, "role": "super-admin|org-admin|member|none", "org_id": "..." }` |

### `POST /sessions/logout`

| | |
|---|---|
| Authz | any-signed-in |
| Response | `204` |
| Effect | revokes the caller's current session (only). |

---

## 12. Audit log

### `GET /audit-logs`

| | |
|---|---|
| Authz | super-admin OR org-admin (implicit filter to their org) |
| Query | `?limit=&cursor=&org_id=&actor_user_id=&action=&resource_type=&resource_id=&since=&until=` |
| Response | `200 { "entries": [<audit_log>], "next_cursor": "..." }` |
| Notes | Super-admin may omit `org_id` to see all; org-admin's queries are force-filtered to their own org. A 403 is returned if an org-admin sets `org_id` to a different org. |

### `POST /audit-events`

Retained from 007 for custom event insertion by internal tooling; authz now requires super-admin.

---

## 13. Retired endpoints (v1)

All removed; HTTP 410 Gone for one deprecation cycle, then removed entirely:

- `GET|POST|GET/{id}|PATCH|DELETE /broadcasters[/{id}]`
- `GET|POST|GET/{id}|DELETE /stream-keys[/{id}]`

Frontend's FR-040 redirects these at the UI level; API returns 410 with a `type: .../problems/retired-endpoint` problem body pointing to the replacement.

---

## 14. Endpoint-to-FR traceability

| FR(s) | Endpoint(s) |
|-------|-------------|
| FR-003, FR-004 | `POST /orgs`, `POST /orgs/{id}/admins`, `DELETE /orgs/{id}/admins/{user_id}` |
| FR-005 | `GET/POST/DELETE /super-admins[/{user_id}]` |
| FR-006, FR-022-style | `POST /orgs/{id}/teams`, `PATCH|DELETE /teams/{id}` |
| FR-007 | (Internal check in team-create) |
| FR-008, FR-009 | `POST /sessions/login-complete` |
| FR-010, FR-011, FR-012 | `POST/GET/PATCH/DELETE` tags + member-tag assignments |
| FR-013, FR-014, FR-015, FR-016, FR-017 | `POST /orgs/{id}/devices`, `POST /devices/{id}/keys/rotate`, `DELETE /devices/{id}/keys/{key_id}` |
| FR-018, FR-019, FR-024 | `POST /orgs/{id}/rooms`, `POST /rooms/{id}/archive`, `POST /rooms/{id}/unarchive` |
| FR-020, FR-021, FR-022, FR-023 | `GET /orgs/{id}/rooms`, `GET /rooms/{id}` (access-gated) |
| FR-024a (concurrency) | All `PATCH /rooms/{id}` and `POST /rooms/{id}/archive(_unarchive)` require `expected_version` |
| FR-024b (ACL preview) | `POST /orgs/{id}/rooms/acl-preview` |
| FR-025, FR-026, FR-027 | `POST /auth` + internal room/policy check |
| FR-027a (stream push) | `GET /stream-events` SSE |
| FR-028 | middleware enforcement across all tenant-scoped endpoints |
| FR-029 | middleware returns `no-org-membership` problem |
| FR-030 | middleware returns suspended problem |
| FR-030a (session store) | `POST /sessions/login-complete`, `POST /sessions/logout`, middleware |
| FR-030b (force-logout) | `POST /orgs/{id}/members/{user_id}/revoke-sessions` |
| FR-031, FR-032 | audit middleware + `GET /audit-logs` |
| FR-033, FR-034, FR-035 | migration command (not an HTTP endpoint) |
