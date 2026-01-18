# Research: Stream Orchestration API

**Date**: 2026-01-17
**Feature**: 001-stream-orchestration

## MediaMTX Integration

### Decision: Use gomediamtx client with functional options wrapper

**Rationale**: The `alpineworks.io/gomediamtx` package provides auto-generated client
code for the MediaMTX HTTP API. We'll wrap it with a functional options pattern to
satisfy constitution requirements and add observability.

**Alternatives considered**:
- Direct HTTP calls: More control but duplicates existing client functionality
- Raw gomediamtx without wrapper: Doesn't satisfy functional options requirement

### MediaMTX External Authentication

**Decision**: Implement POST `/auth` endpoint matching MediaMTX's `authHTTPAddress` format

**Request format** (from MediaMTX):
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

**Response format**:
- HTTP 200: Authentication successful (allow stream)
- HTTP 401: Authentication failed (reject stream)

**Notes**:
- MediaMTX sends stream key as `password` field (user typically empty for RTMP)
- The `path` becomes the stream identifier in our system
- Must respond within MediaMTX timeout (default 10s, we target <50ms)

### MediaMTX Lifecycle Webhooks

**Decision**: Use `runOnReady` and `runOnNotReady` hooks via HTTP endpoints

**Rationale**: These hooks fire when a stream becomes available/unavailable for reading,
which is the correct signal for frontend visibility. The `runOnPublish`/`runOnUnPublish`
hooks fire on connection but stream may not be ready yet.

**Environment variables available**:
- `MTX_PATH` - Stream path/name
- `MTX_SOURCE_TYPE` - Source type (rtmpConn, rtspSession, etc.)
- `MTX_SOURCE_ID` - Source connection ID

**Implementation**: Configure MediaMTX to call:
- `runOnReady: curl -X POST http://api:8080/webhook/ready -d '{"path":"$MTX_PATH","source_type":"$MTX_SOURCE_TYPE","source_id":"$MTX_SOURCE_ID"}'`
- `runOnNotReady: curl -X POST http://api:8080/webhook/not-ready -d '{"path":"$MTX_PATH"}'`

## Database Layer

### Decision: pgxpool with otelpgx instrumentation

**Rationale**:
- `pgxpool` provides connection pooling required for <50ms response times
- `otelpgx` adds OpenTelemetry tracing to all database operations
- Both recommended by user input

**Connection pool settings** (for ~100 concurrent requests):
- Min connections: 5
- Max connections: 25
- Max connection lifetime: 1 hour
- Health check period: 30 seconds

### Decision: Raw SQL with prepared statements (no ORM)

**Rationale**:
- Performance requirement (<50ms) favors prepared statements
- Simple schema doesn't benefit from ORM complexity
- Explicit SQL is more auditable and debuggable

**Alternatives considered**:
- sqlc: Good for type safety but adds build step complexity
- GORM: ORM overhead conflicts with performance requirements
- Raw SQL without preparation: Slower query planning on each request

## HTTP Router

### Decision: Standard library `net/http` with `http.ServeMux` (Go 1.22+)

**Rationale**:
- Go 1.22 added method-based routing (`POST /path`, `GET /path/{id}`)
- No external dependencies needed
- Constitution prefers stdlib where sufficient
- Performance is excellent for our scale

**Alternatives considered**:
- chi: Good routing but unnecessary dependency
- gin: Heavy framework, overkill for simple REST API
- gorilla/mux: Archived, not recommended for new projects

## Stream Key Generation

### Decision: crypto/rand with base64url encoding (32 bytes = 43 chars)

**Rationale**:
- `crypto/rand` provides cryptographically secure randomness
- 32 bytes = 256 bits of entropy (sufficient for stream keys)
- base64url encoding is URL-safe (important for RTMP URLs)

**Format**: `sk_` prefix + 43 character base64url string
**Example**: `sk_7Hj2kL9mN4pQ8rS1tU6vW3xY5zA0bC2dE4fG6hI8jK0`

## Error Handling

### Decision: alpineworks/rfc9457 for all error responses

**Rationale**: Constitution requirement (Principle IV)

**Error type URIs**: Use relative paths that could resolve to docs
- `/errors/invalid-stream-key`
- `/errors/stream-key-in-use`
- `/errors/stream-key-revoked`
- `/errors/stream-not-found`
- `/errors/unauthorized`

## Video URL Construction

### Decision: Construct URLs from MediaMTX base URL + stream path

**Rationale**: MediaMTX exposes streams at predictable URLs based on path name.

**URL formats** (assuming MediaMTX at `mediamtx.example.com`):
- HLS: `https://mediamtx.example.com/{path}/index.m3u8`
- WebRTC: `https://mediamtx.example.com/{path}/whep` (WHEP endpoint)
- RTMP: `rtmp://mediamtx.example.com/{path}` (for re-streaming, not frontend)

**Configuration**: MediaMTX public URL stored in environment variable.

## Concurrency & Race Conditions

### Decision: Database-level locking for stream key activation

**Rationale**: Multiple auth requests for same key could race. Use PostgreSQL
`SELECT ... FOR UPDATE` to ensure only one activation succeeds.

**Pattern**:
```sql
BEGIN;
SELECT * FROM stream_keys WHERE key_value = $1 FOR UPDATE;
-- Check if already in use
-- If not, mark as in use and create stream record
COMMIT;
```

## Configuration

### Decision: Extend existing env-based config with new fields

**New environment variables**:
- `DATABASE_URL` - PostgreSQL connection string
- `API_SECRET` - Shared secret for frontend authentication
- `MEDIAMTX_API_URL` - MediaMTX API endpoint (e.g., `http://mediamtx:9997`)
- `MEDIAMTX_PUBLIC_URL` - MediaMTX public URL for video URLs
- `API_PORT` - HTTP server port (default: 8080)
