# Data Model: Stream Orchestration API

**Date**: 2026-01-17
**Feature**: 001-stream-orchestration

## Entity Relationship Diagram

```text
┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐
│   Broadcaster   │       │   StreamKey     │       │     Stream      │
├─────────────────┤       ├─────────────────┤       ├─────────────────┤
│ id (PK)         │──┐    │ id (PK)         │──┐    │ id (PK)         │
│ display_name    │  │    │ key_value       │  │    │ stream_key_id   │──┐
│ metadata (JSON) │  └───<│ broadcaster_id  │  └───<│ path            │  │
│ created_at      │       │ status          │       │ status          │  │
│ updated_at      │       │ created_at      │       │ started_at      │  │
└─────────────────┘       │ expires_at      │       │ ended_at        │  │
                          │ revoked_at      │       │ source_type     │  │
                          │ last_used_at    │       │ source_id       │  │
                          └─────────────────┘       │ metadata (JSON) │  │
                                                    │ recording_ref   │  │
                                                    └─────────────────┘  │
                                                              │          │
                                                              └──────────┘
                                                    (stream_key_id FK to StreamKey)
```

## Entities

### Broadcaster

Represents an entity authorized to create streams.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| `id` | UUID | PK | Unique identifier |
| `display_name` | VARCHAR(255) | NOT NULL | Human-readable name |
| `metadata` | JSONB | DEFAULT '{}' | Flexible metadata (contact info, etc.) |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation timestamp |
| `updated_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Last update timestamp |

**Indexes**:
- Primary key on `id`

**Notes**: Initially simple; can be extended with authentication if broadcaster
self-service is added later.

### StreamKey

A credential that authorizes a broadcaster to start a stream.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| `id` | UUID | PK | Unique identifier |
| `key_value` | VARCHAR(64) | NOT NULL, UNIQUE | The actual key (sk_...) |
| `broadcaster_id` | UUID | FK → Broadcaster, NOT NULL | Owner of this key |
| `status` | VARCHAR(20) | NOT NULL, DEFAULT 'active' | active, revoked, expired |
| `created_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation timestamp |
| `expires_at` | TIMESTAMPTZ | NULL | Optional expiration time |
| `revoked_at` | TIMESTAMPTZ | NULL | When key was revoked |
| `last_used_at` | TIMESTAMPTZ | NULL | Last successful authentication |

**Indexes**:
- Primary key on `id`
- Unique index on `key_value` (for auth lookups)
- Index on `broadcaster_id` (for listing keys by broadcaster)
- Index on `status` (for filtering active keys)

**Status transitions**:
```text
active ──────► revoked (via admin action)
   │
   └─────────► expired (automatic when expires_at < now())
```

**Validation rules**:
- `key_value` must match pattern `sk_[A-Za-z0-9_-]{43}`
- `expires_at` must be in the future when set
- Cannot transition from `revoked` or `expired` back to `active`

### Stream

Represents an active or historical video broadcast session.

| Field | Type | Constraints | Description |
|-------|------|-------------|-------------|
| `id` | UUID | PK | Unique identifier |
| `stream_key_id` | UUID | FK → StreamKey, NOT NULL | Key used to authenticate |
| `path` | VARCHAR(255) | NOT NULL | MediaMTX path name |
| `status` | VARCHAR(20) | NOT NULL, DEFAULT 'active' | active, ended |
| `started_at` | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | When stream started |
| `ended_at` | TIMESTAMPTZ | NULL | When stream ended |
| `source_type` | VARCHAR(50) | NULL | MediaMTX source type |
| `source_id` | VARCHAR(255) | NULL | MediaMTX connection ID |
| `metadata` | JSONB | DEFAULT '{}' | Additional stream info |
| `recording_ref` | VARCHAR(512) | NULL | Future: S3/GCS path |

**Indexes**:
- Primary key on `id`
- Index on `stream_key_id` (for key usage history)
- Index on `status` (for listing active streams)
- Index on `path` (for webhook lookups)
- Composite index on `(status, started_at)` (for ordered active list)

**Status transitions**:
```text
active ──────► ended (via webhook or key revocation)
```

**Validation rules**:
- Only one `active` stream per `stream_key_id` at a time
- `ended_at` must be set when status is `ended`
- `path` should be URL-safe (alphanumeric, hyphens, underscores)

## SQL Schema

```sql
-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Broadcasters table
CREATE TABLE broadcasters (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    display_name VARCHAR(255) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Stream keys table
CREATE TABLE stream_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    key_value VARCHAR(64) NOT NULL UNIQUE,
    broadcaster_id UUID NOT NULL REFERENCES broadcasters(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked', 'expired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ
);

CREATE INDEX idx_stream_keys_broadcaster ON stream_keys(broadcaster_id);
CREATE INDEX idx_stream_keys_status ON stream_keys(status);

-- Streams table
CREATE TABLE streams (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    stream_key_id UUID NOT NULL REFERENCES stream_keys(id) ON DELETE CASCADE,
    path VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'ended')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    source_type VARCHAR(50),
    source_id VARCHAR(255),
    metadata JSONB NOT NULL DEFAULT '{}',
    recording_ref VARCHAR(512)
);

CREATE INDEX idx_streams_stream_key ON streams(stream_key_id);
CREATE INDEX idx_streams_status ON streams(status);
CREATE INDEX idx_streams_path ON streams(path);
CREATE INDEX idx_streams_active_ordered ON streams(status, started_at DESC) WHERE status = 'active';

-- Ensure only one active stream per key
CREATE UNIQUE INDEX idx_streams_one_active_per_key
    ON streams(stream_key_id)
    WHERE status = 'active';

-- Updated_at trigger for broadcasters
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_broadcasters_updated_at
    BEFORE UPDATE ON broadcasters
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
```

## Go Types

```go
// domain/broadcaster.go
type Broadcaster struct {
    ID          uuid.UUID              `json:"id"`
    DisplayName string                 `json:"display_name"`
    Metadata    map[string]interface{} `json:"metadata"`
    CreatedAt   time.Time              `json:"created_at"`
    UpdatedAt   time.Time              `json:"updated_at"`
}

// domain/streamkey.go
type StreamKeyStatus string

const (
    StreamKeyStatusActive  StreamKeyStatus = "active"
    StreamKeyStatusRevoked StreamKeyStatus = "revoked"
    StreamKeyStatusExpired StreamKeyStatus = "expired"
)

type StreamKey struct {
    ID            uuid.UUID       `json:"id"`
    KeyValue      string          `json:"key_value,omitempty"` // Omit in list responses
    BroadcasterID uuid.UUID       `json:"broadcaster_id"`
    Status        StreamKeyStatus `json:"status"`
    CreatedAt     time.Time       `json:"created_at"`
    ExpiresAt     *time.Time      `json:"expires_at,omitempty"`
    RevokedAt     *time.Time      `json:"revoked_at,omitempty"`
    LastUsedAt    *time.Time      `json:"last_used_at,omitempty"`
}

// domain/stream.go
type StreamStatus string

const (
    StreamStatusActive StreamStatus = "active"
    StreamStatusEnded  StreamStatus = "ended"
)

type Stream struct {
    ID           uuid.UUID              `json:"id"`
    StreamKeyID  uuid.UUID              `json:"stream_key_id"`
    Path         string                 `json:"path"`
    Status       StreamStatus           `json:"status"`
    StartedAt    time.Time              `json:"started_at"`
    EndedAt      *time.Time             `json:"ended_at,omitempty"`
    SourceType   *string                `json:"source_type,omitempty"`
    SourceID     *string                `json:"source_id,omitempty"`
    Metadata     map[string]interface{} `json:"metadata"`
    RecordingRef *string                `json:"recording_ref,omitempty"`
}

// Computed field for API responses
type StreamWithURLs struct {
    Stream
    URLs StreamURLs `json:"urls"`
}

type StreamURLs struct {
    HLS    string `json:"hls"`
    WebRTC string `json:"webrtc"`
}
```

## Repository Interfaces

```go
// domain/streamkey.go
type StreamKeyRepository interface {
    Create(ctx context.Context, key *StreamKey) error
    GetByID(ctx context.Context, id uuid.UUID) (*StreamKey, error)
    GetByKeyValue(ctx context.Context, keyValue string) (*StreamKey, error)
    ListByBroadcaster(ctx context.Context, broadcasterID uuid.UUID) ([]StreamKey, error)
    ListAll(ctx context.Context) ([]StreamKey, error)
    UpdateStatus(ctx context.Context, id uuid.UUID, status StreamKeyStatus) error
    UpdateLastUsed(ctx context.Context, id uuid.UUID) error

    // Atomic operation for auth
    GetAndLockByKeyValue(ctx context.Context, keyValue string) (*StreamKey, error)
}

// domain/stream.go
type StreamRepository interface {
    Create(ctx context.Context, stream *Stream) error
    GetByID(ctx context.Context, id uuid.UUID) (*Stream, error)
    GetActiveByPath(ctx context.Context, path string) (*Stream, error)
    GetActiveByStreamKeyID(ctx context.Context, keyID uuid.UUID) (*Stream, error)
    ListActive(ctx context.Context) ([]Stream, error)
    EndStream(ctx context.Context, id uuid.UUID) error
    EndStreamByPath(ctx context.Context, path string) error
}

// domain/broadcaster.go
type BroadcasterRepository interface {
    Create(ctx context.Context, broadcaster *Broadcaster) error
    GetByID(ctx context.Context, id uuid.UUID) (*Broadcaster, error)
    Update(ctx context.Context, broadcaster *Broadcaster) error
    Delete(ctx context.Context, id uuid.UUID) error
    List(ctx context.Context) ([]Broadcaster, error)
}
```
