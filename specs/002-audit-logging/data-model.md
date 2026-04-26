# Data Model: Audit Logging

**Feature**: 002-audit-logging | **Date**: 2026-02-05

## Entities

### AuditLogEntry

Represents a single recorded action in the system.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| id | UUID | Yes | Unique identifier (auto-generated) |
| timestamp | Timestamp | Yes | When the action occurred (auto-generated) |
| actor | String(255) | Yes | API key identifier that performed the action |
| action | String(50) | Yes | Action type: create, update, delete, revoke, or custom event type |
| resource_type | String(50) | No | Entity type: broadcaster, stream, stream_key (null for custom events) |
| resource_id | UUID | No | ID of the affected resource (null for custom events) |
| request_method | String(10) | Yes | HTTP method: GET, POST, PATCH, DELETE |
| request_path | String(1024) | Yes | Request URL path |
| ip_address | String(45) | Yes | Client IP address (IPv4 or IPv6) |
| outcome | String(20) | Yes | Result: success or failure |
| failure_reason | Text | No | Error message if outcome is failure |
| metadata | JSON | Yes | Additional context (default: empty object) |
| request_id | String(36) | No | Correlation ID from X-Request-ID header |

**Constraints**:
- `action` must be one of: create, update, delete, revoke, login, logout, started_stream, or custom string
- `outcome` must be one of: success, failure
- `resource_type` when present must be one of: broadcaster, stream, stream_key

**Indexes**:
- Primary key on `id`
- `idx_audit_logs_timestamp` on `timestamp DESC` (recent logs query)
- `idx_audit_logs_actor` on `actor` (user-specific audit trail)
- `idx_audit_logs_resource` on `resource_type, resource_id` (entity history)
- `idx_audit_logs_action` on `action` (action-type filtering)
- `idx_audit_logs_composite` on `resource_type, action, timestamp DESC` (filtered queries)

---

### APIKey

Extends existing authentication to support admin authorization.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| id | UUID | Yes | Unique identifier (auto-generated) |
| key_identifier | String(255) | Yes | The API key value used in X-API-Key header |
| description | String(500) | No | Human-readable description of key purpose |
| is_admin | Boolean | Yes | Whether this key has admin privileges (default: false) |
| created_at | Timestamp | Yes | When the key was created (auto-generated) |
| last_used_at | Timestamp | No | Last time the key was used for authentication |

**Constraints**:
- `key_identifier` must be unique

**Indexes**:
- Primary key on `id`
- Unique index on `key_identifier`

---

## Relationships

```
┌─────────────┐         ┌──────────────────┐
│   APIKey    │         │  AuditLogEntry   │
├─────────────┤         ├──────────────────┤
│ id          │◄────────│ actor            │ (logical reference by key_identifier)
│ key_identifier │      │ id               │
│ is_admin    │         │ timestamp        │
│ ...         │         │ action           │
└─────────────┘         │ resource_type    │
                        │ resource_id ─────┼────► Broadcaster, Stream, or StreamKey
                        │ ...              │
                        └──────────────────┘
```

**Notes**:
- `actor` stores the key_identifier string, not a foreign key (for resilience if keys are deleted)
- `resource_id` is a logical reference to the affected entity (no foreign key constraint for historical integrity)

---

## State Transitions

### AuditLogEntry

Audit log entries are **immutable** once created (FR-008). No state transitions apply.

```
┌─────────────┐
│  Created    │ (final state)
└─────────────┘
```

---

## Validation Rules

### AuditLogEntry Creation

From Spec Requirements (FR-001 through FR-010):

1. **Actor Required**: Must have non-empty actor from authenticated context
2. **Action Required**: Must be a valid action type string
3. **Resource Consistency**: If resource_type is present, resource_id should also be present (and vice versa) - except for custom events where both are null
4. **IP Address Required**: Must capture client IP from request
5. **Outcome Required**: Must be either "success" or "failure"
6. **Failure Reason**: Required if outcome is "failure"
7. **No Sensitive Data**: Metadata must not contain stream key values, passwords, or secrets

### Custom Event Submission (POST /audit-events)

1. **Event Type Required**: Must provide event_type in request body
2. **Valid Event Type**: Event type should be a non-empty string (max 50 chars)
3. **Metadata Optional**: If provided, must be valid JSON object
4. **Authenticated**: Request must have valid API key (any key, not admin-only)

### Audit Log Retrieval (GET /audit-logs)

1. **Admin Required**: API key must have is_admin = true
2. **Pagination Limits**: limit must be 1-100, default 50
3. **Date Format**: from/to must be RFC 3339 format if provided
4. **Valid Filters**: resource_type must be known type if provided

---

## Database Migration

### Migration 000002_add_audit_logs.up.sql

```sql
-- API Keys table for admin authorization
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    key_identifier VARCHAR(255) NOT NULL UNIQUE,
    description VARCHAR(500),
    is_admin BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);

CREATE INDEX idx_api_keys_identifier ON api_keys(key_identifier);

-- Audit logs table
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    actor VARCHAR(255) NOT NULL,
    action VARCHAR(50) NOT NULL,
    resource_type VARCHAR(50),
    resource_id UUID,
    request_method VARCHAR(10) NOT NULL,
    request_path VARCHAR(1024) NOT NULL,
    ip_address VARCHAR(45) NOT NULL,
    outcome VARCHAR(20) NOT NULL DEFAULT 'success',
    failure_reason TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    request_id VARCHAR(36)
);

-- Indexes for query performance
CREATE INDEX idx_audit_logs_timestamp ON audit_logs(timestamp DESC);
CREATE INDEX idx_audit_logs_actor ON audit_logs(actor);
CREATE INDEX idx_audit_logs_resource ON audit_logs(resource_type, resource_id);
CREATE INDEX idx_audit_logs_action ON audit_logs(action);
CREATE INDEX idx_audit_logs_composite ON audit_logs(resource_type, action, timestamp DESC);

-- Partial index for filtering by outcome
CREATE INDEX idx_audit_logs_failures ON audit_logs(timestamp DESC) WHERE outcome = 'failure';
```

### Migration 000002_add_audit_logs.down.sql

```sql
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS api_keys;
```

---

## Metadata Schema Examples

### Update Operation (before/after state)

```json
{
    "before": {
        "display_name": "Old Name",
        "metadata": {"tag": "original"}
    },
    "after": {
        "display_name": "New Name",
        "metadata": {"tag": "updated"}
    }
}
```

### Stream Key Revocation

```json
{
    "broadcaster_id": "550e8400-e29b-41d4-a716-446655440000",
    "reason": "security_concern"
}
```

### Custom Event (login)

```json
{
    "session_id": "sess_abc123",
    "browser": "Chrome",
    "os": "macOS"
}
```

### Custom Event (started_stream)

```json
{
    "stream_id": "550e8400-e29b-41d4-a716-446655440000",
    "broadcaster_name": "Rescue Team Alpha"
}
```
