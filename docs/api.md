# RescueStream API Reference

API documentation for frontend application integration.

## Base URL

```
http://localhost:8080
```

## Authentication

All protected endpoints require HMAC-SHA256 signature authentication.

### Required Headers

| Header | Description |
|--------|-------------|
| `X-API-Key` | API key identifier (e.g., `admin`) |
| `X-Timestamp` | Unix epoch timestamp (seconds) |
| `X-Signature` | HMAC-SHA256 signature |

### Signature Generation

```
STRING_TO_SIGN = "{METHOD}\n{PATH}\n{TIMESTAMP}\n{BODY}"
SIGNATURE = HMAC-SHA256(STRING_TO_SIGN, API_SECRET)
```

### Example (using api-test.sh)

```bash
./scripts/api-test.sh GET /streams
./scripts/api-test.sh POST /broadcasters '{"display_name":"Test"}'
```

---

## Error Responses

All errors follow [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) Problem Details format.

### Error Response Structure

```json
{
  "type": "/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "The requested resource was not found",
  "instance": "/streams/invalid-id"
}
```

### Error Types

| Type | Status | Description |
|------|--------|-------------|
| `/errors/not-found` | 404 | Resource not found |
| `/errors/invalid-request` | 400 | Invalid request (bad input, validation failure) |
| `/errors/unauthorized` | 401 | Authentication failed |
| `/errors/conflict` | 409 | Resource conflict (duplicate) |
| `/errors/internal-error` | 500 | Server error |
| `/errors/invalid-stream-key` | 401 | Stream key is invalid |
| `/errors/stream-key-in-use` | 409 | Stream key already in use |
| `/errors/stream-key-revoked` | 401 | Stream key has been revoked |
| `/errors/stream-key-expired` | 401 | Stream key has expired |
| `/errors/forbidden` | 403 | Access denied (e.g., admin required) |

---

## Endpoints

### Health Check

#### GET /health

Check API and database health status. **No authentication required.**

**Request:**
```bash
./scripts/api-test.sh GET /health
```

**Success Response (200):**
```json
{
  "status": "ok",
  "database": "ok"
}
```

**Degraded Response (503):**
```json
{
  "status": "degraded",
  "database": "unreachable"
}
```

---

### Streams

#### GET /streams

List all active streams with playback URLs.

**Request:**
```bash
./scripts/api-test.sh GET /streams
```

**Success Response (200):**
```json
{
  "streams": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "stream_key_id": "660e8400-e29b-41d4-a716-446655440001",
      "path": "live/stream-abc123",
      "status": "active",
      "started_at": "2026-01-18T10:30:00Z",
      "ended_at": null,
      "source_type": "rtmp",
      "source_id": "192.168.1.100",
      "metadata": {},
      "recording_ref": null,
      "urls": {
        "hls": "http://localhost:8888/live/stream-abc123/index.m3u8",
        "webrtc": "http://localhost:8889/live/stream-abc123"
      }
    }
  ],
  "count": 1
}
```

**Empty Response (200):**
```json
{
  "streams": [],
  "count": 0
}
```

**Error Response (401):**
```json
{
  "type": "/errors/unauthorized",
  "title": "Unauthorized",
  "status": 401,
  "detail": "missing or invalid signature",
  "instance": "/streams"
}
```

**Error Response (500):**
```json
{
  "type": "/errors/internal-error",
  "title": "Internal Server Error",
  "status": 500,
  "detail": "failed to list streams",
  "instance": "/streams"
}
```

---

#### GET /streams/{id}

Get a specific stream by ID.

**Request:**
```bash
./scripts/api-test.sh GET /streams/550e8400-e29b-41d4-a716-446655440000
```

**Success Response (200):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "stream_key_id": "660e8400-e29b-41d4-a716-446655440001",
  "path": "live/stream-abc123",
  "status": "active",
  "started_at": "2026-01-18T10:30:00Z",
  "ended_at": null,
  "source_type": "rtmp",
  "source_id": "192.168.1.100",
  "metadata": {},
  "recording_ref": null,
  "urls": {
    "hls": "http://localhost:8888/live/stream-abc123/index.m3u8",
    "webrtc": "http://localhost:8889/live/stream-abc123"
  }
}
```

**Error Response (400) - Invalid ID:**
```json
{
  "type": "/errors/invalid-request",
  "title": "Invalid Request",
  "status": 400,
  "detail": "invalid stream ID",
  "instance": "/streams/not-a-uuid"
}
```

**Error Response (404):**
```json
{
  "type": "/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "The requested resource was not found",
  "instance": "/streams/550e8400-e29b-41d4-a716-446655440000"
}
```

---

### Broadcasters

#### GET /broadcasters

List all broadcasters.

**Request:**
```bash
./scripts/api-test.sh GET /broadcasters
```

**Success Response (200):**
```json
{
  "broadcasters": [
    {
      "id": "770e8400-e29b-41d4-a716-446655440002",
      "display_name": "Field Team Alpha",
      "metadata": {
        "region": "northeast",
        "team_size": 5
      },
      "created_at": "2026-01-15T08:00:00Z",
      "updated_at": "2026-01-15T08:00:00Z"
    }
  ],
  "count": 1
}
```

---

#### POST /broadcasters

Create a new broadcaster.

**Request:**
```bash
./scripts/api-test.sh POST /broadcasters '{"display_name":"Field Team Alpha","metadata":{"region":"northeast"}}'
```

**Request Body:**
```json
{
  "display_name": "Field Team Alpha",
  "metadata": {
    "region": "northeast"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `display_name` | string | Yes | Human-readable name |
| `metadata` | object | No | Arbitrary key-value data |

**Success Response (201):**
```json
{
  "id": "770e8400-e29b-41d4-a716-446655440002",
  "display_name": "Field Team Alpha",
  "metadata": {
    "region": "northeast"
  },
  "created_at": "2026-01-18T10:30:00Z",
  "updated_at": "2026-01-18T10:30:00Z"
}
```

**Error Response (400) - Missing display_name:**
```json
{
  "type": "/errors/invalid-request",
  "title": "Invalid Request",
  "status": 400,
  "detail": "display_name is required",
  "instance": "/broadcasters"
}
```

**Error Response (400) - Invalid JSON:**
```json
{
  "type": "/errors/invalid-request",
  "title": "Invalid Request",
  "status": 400,
  "detail": "invalid request body",
  "instance": "/broadcasters"
}
```

---

#### GET /broadcasters/{id}

Get a specific broadcaster by ID.

**Request:**
```bash
./scripts/api-test.sh GET /broadcasters/770e8400-e29b-41d4-a716-446655440002
```

**Success Response (200):**
```json
{
  "id": "770e8400-e29b-41d4-a716-446655440002",
  "display_name": "Field Team Alpha",
  "metadata": {
    "region": "northeast"
  },
  "created_at": "2026-01-15T08:00:00Z",
  "updated_at": "2026-01-15T08:00:00Z"
}
```

**Error Response (400) - Invalid ID:**
```json
{
  "type": "/errors/invalid-request",
  "title": "Invalid Request",
  "status": 400,
  "detail": "invalid broadcaster ID",
  "instance": "/broadcasters/not-a-uuid"
}
```

**Error Response (404):**
```json
{
  "type": "/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "The requested resource was not found",
  "instance": "/broadcasters/770e8400-e29b-41d4-a716-446655440002"
}
```

---

#### PATCH /broadcasters/{id}

Update a broadcaster.

**Request:**
```bash
./scripts/api-test.sh PATCH /broadcasters/770e8400-e29b-41d4-a716-446655440002 '{"display_name":"Field Team Beta"}'
```

**Request Body:**
```json
{
  "display_name": "Field Team Beta",
  "metadata": {
    "region": "northwest",
    "team_size": 8
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `display_name` | string | No | Updated name |
| `metadata` | object | No | Updated metadata (replaces existing) |

**Success Response (200):**
```json
{
  "id": "770e8400-e29b-41d4-a716-446655440002",
  "display_name": "Field Team Beta",
  "metadata": {
    "region": "northwest",
    "team_size": 8
  },
  "created_at": "2026-01-15T08:00:00Z",
  "updated_at": "2026-01-18T11:00:00Z"
}
```

**Error Response (404):**
```json
{
  "type": "/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "The requested resource was not found",
  "instance": "/broadcasters/770e8400-e29b-41d4-a716-446655440002"
}
```

---

#### DELETE /broadcasters/{id}

Delete a broadcaster.

**Request:**
```bash
./scripts/api-test.sh DELETE /broadcasters/770e8400-e29b-41d4-a716-446655440002
```

**Success Response (204):**
No content.

**Error Response (404):**
```json
{
  "type": "/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "The requested resource was not found",
  "instance": "/broadcasters/770e8400-e29b-41d4-a716-446655440002"
}
```

---

### Stream Keys

#### GET /stream-keys

List all stream keys.

**Request:**
```bash
./scripts/api-test.sh GET /stream-keys
```

**Success Response (200):**
```json
{
  "stream_keys": [
    {
      "id": "880e8400-e29b-41d4-a716-446655440003",
      "broadcaster_id": "770e8400-e29b-41d4-a716-446655440002",
      "status": "active",
      "created_at": "2026-01-15T09:00:00Z",
      "expires_at": "2026-02-15T09:00:00Z",
      "revoked_at": null,
      "last_used_at": "2026-01-18T10:30:00Z"
    }
  ],
  "count": 1
}
```

Note: `key_value` is not included in list responses for security.

---

#### POST /stream-keys

Create a new stream key for a broadcaster.

**Request:**
```bash
./scripts/api-test.sh POST /stream-keys '{"broadcaster_id":"770e8400-e29b-41d4-a716-446655440002","expires_at":"2026-02-15T09:00:00Z"}'
```

**Request Body:**
```json
{
  "broadcaster_id": "770e8400-e29b-41d4-a716-446655440002",
  "expires_at": "2026-02-15T09:00:00Z"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `broadcaster_id` | string (UUID) | Yes | Broadcaster to associate with |
| `expires_at` | string (RFC3339) | No | Expiration timestamp |

**Success Response (201):**
```json
{
  "id": "880e8400-e29b-41d4-a716-446655440003",
  "key_value": "sk_live_abc123def456ghi789jkl012mno345",
  "broadcaster_id": "770e8400-e29b-41d4-a716-446655440002",
  "status": "active",
  "created_at": "2026-01-18T10:30:00Z",
  "expires_at": "2026-02-15T09:00:00Z",
  "revoked_at": null,
  "last_used_at": null
}
```

Note: `key_value` is only returned on creation. Store it securely.

**Error Response (400) - Missing broadcaster_id:**
```json
{
  "type": "/errors/invalid-request",
  "title": "Invalid Request",
  "status": 400,
  "detail": "broadcaster_id is required",
  "instance": "/stream-keys"
}
```

**Error Response (400) - Invalid broadcaster_id:**
```json
{
  "type": "/errors/invalid-request",
  "title": "Invalid Request",
  "status": 400,
  "detail": "invalid broadcaster_id",
  "instance": "/stream-keys"
}
```

**Error Response (400) - Invalid expires_at format:**
```json
{
  "type": "/errors/invalid-request",
  "title": "Invalid Request",
  "status": 400,
  "detail": "invalid expires_at format, use RFC3339",
  "instance": "/stream-keys"
}
```

**Error Response (404) - Broadcaster not found:**
```json
{
  "type": "/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "The requested resource was not found",
  "instance": "/stream-keys"
}
```

---

#### GET /stream-keys/{id}

Get a specific stream key by ID.

**Request:**
```bash
./scripts/api-test.sh GET /stream-keys/880e8400-e29b-41d4-a716-446655440003
```

**Success Response (200):**
```json
{
  "id": "880e8400-e29b-41d4-a716-446655440003",
  "broadcaster_id": "770e8400-e29b-41d4-a716-446655440002",
  "status": "active",
  "created_at": "2026-01-15T09:00:00Z",
  "expires_at": "2026-02-15T09:00:00Z",
  "revoked_at": null,
  "last_used_at": "2026-01-18T10:30:00Z"
}
```

Note: `key_value` is not returned for security.

**Error Response (400) - Invalid ID:**
```json
{
  "type": "/errors/invalid-request",
  "title": "Invalid Request",
  "status": 400,
  "detail": "invalid stream key ID",
  "instance": "/stream-keys/not-a-uuid"
}
```

**Error Response (404):**
```json
{
  "type": "/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "The requested resource was not found",
  "instance": "/stream-keys/880e8400-e29b-41d4-a716-446655440003"
}
```

---

#### DELETE /stream-keys/{id}

Revoke a stream key.

**Request:**
```bash
./scripts/api-test.sh DELETE /stream-keys/880e8400-e29b-41d4-a716-446655440003
```

**Success Response (204):**
No content.

**Error Response (404):**
```json
{
  "type": "/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "The requested resource was not found",
  "instance": "/stream-keys/880e8400-e29b-41d4-a716-446655440003"
}
```

---

### Audit Logs

#### GET /audit-logs

List audit log entries with optional filtering and pagination. **Requires admin privileges.**

**Request:**
```bash
./scripts/api-test.sh GET /audit-logs
./scripts/api-test.sh GET '/audit-logs?action=create&limit=10'
./scripts/api-test.sh GET '/audit-logs?from=2026-01-01T00:00:00Z&to=2026-02-01T00:00:00Z'
```

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `from` | string (RFC3339) | No | Start timestamp (inclusive) |
| `to` | string (RFC3339) | No | End timestamp (exclusive) |
| `actor` | string | No | Filter by API key identifier |
| `action` | string | No | Filter by action type (create, update, delete, etc.) |
| `resource_type` | string | No | Filter by resource type (broadcaster, stream, stream_key) |
| `resource_id` | string (UUID) | No | Filter by specific resource ID |
| `limit` | integer | No | Max entries per page (1-100, default 50) |
| `offset` | integer | No | Number of entries to skip (default 0) |

**Success Response (200):**
```json
{
  "audit_logs": [
    {
      "id": "990e8400-e29b-41d4-a716-446655440004",
      "timestamp": "2026-01-18T10:30:00Z",
      "actor": "admin",
      "action": "create",
      "resource_type": "broadcaster",
      "resource_id": "770e8400-e29b-41d4-a716-446655440002",
      "request_method": "POST",
      "request_path": "/broadcasters",
      "ip_address": "192.168.1.100",
      "outcome": "success",
      "failure_reason": null,
      "metadata": {},
      "request_id": "abc123-def456"
    }
  ],
  "pagination": {
    "limit": 50,
    "offset": 0,
    "total": 1
  }
}
```

**Error Response (403) - Not Admin:**
```json
{
  "type": "/errors/forbidden",
  "title": "Forbidden",
  "status": 403,
  "detail": "Admin privileges required to access audit logs",
  "instance": "/audit-logs"
}
```

**Error Response (400) - Invalid Filter:**
```json
{
  "type": "/errors/invalid-request",
  "title": "Invalid Request",
  "status": 400,
  "detail": "invalid 'from' date format, expected RFC 3339 (e.g., 2026-01-01T00:00:00Z)",
  "instance": "/audit-logs"
}
```

---

#### POST /audit-events

Submit a custom audit event (e.g., login, logout, started_stream). **Any authenticated API key can submit.**

**Request:**
```bash
./scripts/api-test.sh POST /audit-events '{"event_type":"login","metadata":{"browser":"Chrome"}}'
```

**Request Body:**
```json
{
  "event_type": "login",
  "metadata": {
    "session_id": "abc123",
    "browser": "Chrome"
  }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `event_type` | string | Yes | Event type (max 50 chars). Common values: login, logout, started_stream |
| `metadata` | object | No | Additional context for the event |

**Success Response (201):**
```json
{
  "id": "aa0e8400-e29b-41d4-a716-446655440005",
  "timestamp": "2026-01-18T10:35:00Z",
  "actor": "api-key-user",
  "action": "login",
  "resource_type": null,
  "resource_id": null,
  "request_method": "POST",
  "request_path": "/audit-events",
  "ip_address": "192.168.1.100",
  "outcome": "success",
  "failure_reason": null,
  "metadata": {
    "session_id": "abc123",
    "browser": "Chrome"
  },
  "request_id": "def456-ghi789"
}
```

**Error Response (400) - Missing event_type:**
```json
{
  "type": "/errors/invalid-request",
  "title": "Invalid Request",
  "status": 400,
  "detail": "event_type is required",
  "instance": "/audit-events"
}
```

**Error Response (400) - event_type too long:**
```json
{
  "type": "/errors/invalid-request",
  "title": "Invalid Request",
  "status": 400,
  "detail": "event_type must be 50 characters or less",
  "instance": "/audit-events"
}
```

---

## Common Frontend Workflows

### 1. Display Live Streams

```javascript
// Fetch active streams
const response = await fetch('/streams', { headers: authHeaders });
const { streams } = await response.json();

// Each stream has playback URLs ready to use
streams.forEach(stream => {
  console.log(`HLS: ${stream.urls.hls}`);
  console.log(`WebRTC: ${stream.urls.webrtc}`);
});
```

### 2. Create Broadcaster and Stream Key

```javascript
// 1. Create broadcaster
const broadcaster = await fetch('/broadcasters', {
  method: 'POST',
  headers: authHeaders,
  body: JSON.stringify({ display_name: 'Field Team Alpha' })
}).then(r => r.json());

// 2. Create stream key for broadcaster
const streamKey = await fetch('/stream-keys', {
  method: 'POST',
  headers: authHeaders,
  body: JSON.stringify({
    broadcaster_id: broadcaster.id,
    expires_at: '2026-02-01T00:00:00Z'
  })
}).then(r => r.json());

// 3. Provide key_value to broadcaster for OBS/streaming software
console.log(`Stream Key: ${streamKey.key_value}`);
```

### 3. Error Handling

```javascript
const response = await fetch('/streams/invalid-id', { headers: authHeaders });

if (!response.ok) {
  const error = await response.json();

  switch (error.type) {
    case '/errors/not-found':
      showNotification('Stream not found');
      break;
    case '/errors/unauthorized':
      redirectToLogin();
      break;
    case '/errors/invalid-request':
      showValidationError(error.detail);
      break;
    default:
      showGenericError();
  }
}
```

---

## TypeScript Types

```typescript
interface Stream {
  id: string;
  stream_key_id: string;
  path: string;
  status: 'active' | 'ended';
  started_at: string;
  ended_at: string | null;
  source_type: string | null;
  source_id: string | null;
  metadata: Record<string, unknown>;
  recording_ref: string | null;
  urls: {
    hls: string;
    webrtc: string;
  };
}

interface Broadcaster {
  id: string;
  display_name: string;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

interface StreamKey {
  id: string;
  key_value?: string; // Only on creation
  broadcaster_id: string;
  status: 'active' | 'revoked' | 'expired';
  created_at: string;
  expires_at: string | null;
  revoked_at: string | null;
  last_used_at: string | null;
}

interface APIError {
  type: string;
  title: string;
  status: number;
  detail: string;
  instance: string;
}

interface AuditLogEntry {
  id: string;
  timestamp: string;
  actor: string;
  action: string;
  resource_type: string | null;
  resource_id: string | null;
  request_method: string;
  request_path: string;
  ip_address: string;
  outcome: 'success' | 'failure';
  failure_reason: string | null;
  metadata: Record<string, unknown>;
  request_id: string | null;
}

interface AuditLogListResponse {
  audit_logs: AuditLogEntry[];
  pagination: {
    limit: number;
    offset: number;
    total: number;
  };
}
```
