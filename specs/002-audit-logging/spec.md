# Feature Specification: Audit Logging

**Feature Branch**: `002-audit-logging`
**Created**: 2026-02-05
**Status**: Draft
**Input**: User description: "I would like to set up an audit logging endpoint to send all user actions to the database, as well as a way to retrieve them as an admin user (likely for display in the dashboard)"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Admin Reviews Recent Activity (Priority: P1)

An administrator needs to monitor system activity to ensure proper usage, investigate incidents, and maintain security oversight. They access the audit log through the dashboard to see a chronological list of all user actions in the system.

**Why this priority**: Core functionality - without the ability to view audit logs, the entire feature provides no value. This enables security monitoring, compliance, and incident investigation.

**Independent Test**: Can be fully tested by making API calls to create/modify resources, then retrieving the audit log and verifying entries appear with correct details.

**Acceptance Scenarios**:

1. **Given** an admin is authenticated, **When** they request the audit log, **Then** they see a list of recent actions sorted by most recent first
2. **Given** multiple actions have occurred, **When** an admin views the audit log, **Then** each entry shows who performed the action, what action was taken, when it occurred, and the outcome
3. **Given** an admin needs to investigate a specific time period, **When** they filter by date range, **Then** only entries within that range are returned

---

### User Story 2 - System Records All API Actions (Priority: P1)

The system automatically records all authenticated API actions (create, update, delete operations) to provide a complete audit trail without requiring manual logging by developers.

**Why this priority**: Equal to P1 as recording is the prerequisite for viewing. The system must capture actions before they can be reviewed.

**Independent Test**: Can be tested by performing various API operations (create broadcaster, revoke stream key, etc.) and verifying corresponding audit entries are created in the database.

**Acceptance Scenarios**:

1. **Given** a user creates a broadcaster, **When** the action completes, **Then** an audit log entry is recorded with action type "create", resource type "broadcaster", and the resource ID
2. **Given** a user revokes a stream key, **When** the action completes, **Then** an audit log entry captures the action with all relevant context
3. **Given** an API action fails, **When** the failure occurs, **Then** the audit log records the attempted action with a failure status and reason

---

### User Story 3 - Filter and Search Audit Logs (Priority: P2)

An administrator investigating a specific incident needs to filter audit logs by various criteria to quickly find relevant entries among potentially thousands of records.

**Why this priority**: Enhances usability of P1 functionality. Basic viewing works without this, but filtering significantly improves the admin experience for real-world usage.

**Independent Test**: Can be tested by creating entries with different actors, resources, and actions, then verifying filters return only matching entries.

**Acceptance Scenarios**:

1. **Given** an admin wants to see actions by a specific API key, **When** they filter by actor, **Then** only entries from that actor are returned
2. **Given** an admin investigates changes to broadcasters, **When** they filter by resource type "broadcaster", **Then** only broadcaster-related entries are shown
3. **Given** an admin looks for deletions, **When** they filter by action type "delete", **Then** only delete actions are returned

---

### User Story 4 - Paginate Large Audit Logs (Priority: P2)

When the audit log contains thousands of entries, administrators need to navigate through pages of results efficiently without overwhelming the dashboard or API responses.

**Why this priority**: Essential for production systems with high activity. Without pagination, the feature becomes unusable at scale.

**Independent Test**: Can be tested by creating many audit entries and verifying pagination parameters return correct subsets with proper metadata (total count, page info).

**Acceptance Scenarios**:

1. **Given** there are 500 audit entries, **When** an admin requests the first page with limit 50, **Then** they receive 50 entries and metadata indicating more pages exist
2. **Given** an admin is on page 2, **When** they request the next page, **Then** they see entries 101-150 (with default limit of 50)
3. **Given** an admin specifies a custom page size, **When** the request is made, **Then** that number of entries is returned (up to a maximum limit)

---

### Edge Cases

- What happens when the audit log database table grows very large? Entries older than the retention period should be automatically purged or archived.
- How does the system handle audit logging failures? The primary action should still succeed, but the failure should be logged to application logs for monitoring.
- What happens if an unauthenticated request is made? Only authenticated requests are logged; health checks and similar public endpoints are excluded.
- How are bulk operations logged? Each affected resource gets its own audit entry with a shared correlation ID.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST record an audit log entry synchronously (within the same request transaction) for every authenticated API action that creates, updates, or deletes a resource
- **FR-002**: Each audit log entry MUST include: timestamp, actor (API key identifier), action type, resource type, resource ID, request path, and outcome (success/failure)
- **FR-003**: Audit log entries MUST include the IP address of the requester
- **FR-004**: Audit log entries SHOULD include relevant before/after data for update operations (stored as metadata)
- **FR-005**: System MUST provide an API endpoint for retrieving audit logs, accessible only to API keys with the admin flag set to true
- **FR-006**: The retrieval endpoint MUST support filtering by: date range, actor, action type, resource type, and resource ID
- **FR-007**: The retrieval endpoint MUST support pagination with configurable page size (default 50, maximum 100)
- **FR-008**: Audit log entries MUST be immutable once created (no updates or deletes through the API)
- **FR-009**: Failed API actions MUST also be logged with the failure reason
- **FR-010**: System MUST NOT log sensitive data such as stream key values, passwords, or authentication secrets in audit entries
- **FR-011**: Audit entries MUST be retained for a minimum of 90 days
- **FR-012**: System MUST provide an API endpoint accessible to any authenticated API key for submitting custom audit events (e.g., login, logout, started_stream) with event type and optional metadata

### Key Entities

- **AuditLogEntry**: Represents a single recorded action in the system
  - Unique identifier
  - Timestamp of when the action occurred
  - Actor identifier (API key or system)
  - Action type (create, update, delete, revoke, login, logout, started_stream, or custom)
  - Resource type (broadcaster, stream, stream_key)
  - Resource identifier
  - Request path and method
  - Source IP address
  - Outcome (success, failure)
  - Failure reason (if applicable)
  - Metadata (additional context, before/after state for updates)

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of authenticated mutating API actions are captured in the audit log
- **SC-002**: Admins can retrieve the last 24 hours of audit logs in under 2 seconds
- **SC-003**: Audit log queries with filters return results in under 1 second for logs containing up to 100,000 entries
- **SC-004**: The audit logging process adds less than 50ms latency to API requests
- **SC-005**: Audit log entries are available for retrieval within 1 second of the action completing

## Clarifications

### Session 2026-02-05

- Q: Who can access audit logs - all API keys or restricted? → A: Introduce a new "admin" flag/role on API keys to restrict audit log access
- Q: Sync or async audit logging? → A: Synchronous - write audit entry before returning API response (guarantees capture)
- Q: Should read operations be logged? → A: Mutating only, plus accept custom events from frontend (login, logout, started stream)
- Q: Who can submit custom events? → A: Any authenticated API key can submit custom events (actor is recorded)

## Assumptions

- All API authentication uses the existing HMAC-SHA256 signature mechanism; the API key header identifies the actor
- API keys will have an "admin" boolean flag; only API keys with admin=true can access the audit log retrieval endpoint
- The existing PostgreSQL database will store audit logs
- 90-day retention period is acceptable for compliance needs; older logs may be archived or purged
- The dashboard mentioned by the user is a separate frontend application that will consume this API
