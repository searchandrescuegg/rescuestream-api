# Feature Specification: Stream Orchestration API

**Feature Branch**: `001-stream-orchestration`
**Created**: 2026-01-17
**Status**: Draft
**Input**: User description: "I need to create an API that interacts with streams of video, specifically not the streams themselves but the orchestration, via a 3rd party API. We will be responsible for keeping track of active streams and their stream keys. We will also have an auth endpoint to accept or reject credentials (the stream key) because that functionality is deferred from the 3rd party to our application. We will be providing relevant data back to our frontend application, such as active streams, their video links, and other relevant parameters."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Stream Key Authentication (Priority: P1)

A broadcaster attempts to start streaming using their assigned stream key. The third-party
streaming service (MediaMTX) calls our authentication endpoint to validate the stream key
before allowing the connection. The system verifies the key is valid, active, and not
already in use, then permits or denies the stream connection.

**Why this priority**: This is the foundational security gate. Without stream key
authentication, unauthorized users could broadcast content. All other features depend
on knowing which streams are legitimate.

**Independent Test**: Can be fully tested by sending authentication requests with various
stream keys and verifying accept/reject responses. Delivers the core security value of
controlling who can broadcast.

**Acceptance Scenarios**:

1. **Given** a valid, active stream key that is not currently in use, **When** the
   third-party service requests authentication, **Then** the system returns an approval
   response and records the stream as active.

2. **Given** an invalid or unknown stream key, **When** the third-party service requests
   authentication, **Then** the system returns a rejection response with an appropriate
   error code.

3. **Given** a valid stream key that is already in use by an active stream, **When**
   another authentication request arrives for the same key, **Then** the system rejects
   the duplicate connection attempt.

4. **Given** a valid stream key that has been revoked or disabled, **When** authentication
   is requested, **Then** the system returns a rejection response.

---

### User Story 2 - Active Stream Listing for Frontend (Priority: P2)

A frontend application user wants to see all currently active video streams. The system
provides a list of active streams including their video playback URLs, stream metadata,
and status information so viewers can select and watch broadcasts.

**Why this priority**: Once streams can be authenticated, the frontend needs visibility
into what's available. This enables the core viewing experience.

**Independent Test**: Can be fully tested by querying the streams endpoint and verifying
the response contains expected stream data with playback URLs. Delivers viewer discovery
value.

**Acceptance Scenarios**:

1. **Given** multiple active streams exist, **When** the frontend requests the stream
   list, **Then** the system returns all active streams with their video URLs, titles,
   and relevant metadata.

2. **Given** no active streams exist, **When** the frontend requests the stream list,
   **Then** the system returns an empty list with appropriate messaging.

3. **Given** a stream becomes inactive (broadcaster disconnects), **When** the frontend
   requests the stream list, **Then** that stream no longer appears in the active list.

---

### User Story 3 - Stream Key Management (Priority: P3)

An administrator needs to create, view, and revoke stream keys for broadcasters. The
system allows management of stream keys including generating new keys, listing existing
keys with their status, and revoking keys that should no longer have access.

**Why this priority**: Operational management of who can broadcast. Required for
onboarding new broadcasters and removing access when needed.

**Independent Test**: Can be fully tested by creating stream keys, listing them,
and revoking them, then verifying authentication behavior changes accordingly.

**Acceptance Scenarios**:

1. **Given** an administrator needs to onboard a new broadcaster, **When** they request
   a new stream key, **Then** the system generates a unique, secure key and returns it.

2. **Given** multiple stream keys exist, **When** the administrator requests the key
   list, **Then** the system returns all keys with their status (active, revoked, in-use).

3. **Given** a stream key needs to be revoked, **When** the administrator revokes it,
   **Then** the key immediately becomes invalid and any active stream using it is
   terminated.

---

### User Story 4 - Individual Stream Details (Priority: P4)

A frontend user or administrator wants to view detailed information about a specific
stream including its video URLs (multiple formats/qualities), broadcaster information,
stream duration, viewer count, and technical parameters.

**Why this priority**: Enhanced viewing experience and operational monitoring after
core functionality is established.

**Independent Test**: Can be fully tested by requesting details for a specific stream
and verifying all expected fields are returned.

**Acceptance Scenarios**:

1. **Given** an active stream exists, **When** details are requested for that stream,
   **Then** the system returns comprehensive stream information including all available
   video URLs, stream start time, and broadcaster details.

2. **Given** a stream identifier that does not exist, **When** details are requested,
   **Then** the system returns an appropriate not-found error.

---

### Edge Cases

- What happens when the third-party streaming service is unreachable during stream state
  synchronization? System MUST continue serving cached state and retry synchronization.

- What happens when a stream key authentication request times out? The third-party service
  will treat no response as rejection; system MUST respond within timeout threshold.

- What happens when the same broadcaster rapidly connects and disconnects? System MUST
  handle race conditions and maintain accurate stream state.

- How does the system handle stream keys that expire based on time? System MUST reject
  expired keys and mark associated streams as inactive.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST provide an authentication endpoint that accepts stream key
  credentials and returns accept/reject decisions to the third-party streaming service.

- **FR-002**: System MUST validate stream keys against the stored registry and return
  rejection for unknown, expired, revoked, or already-in-use keys.

- **FR-003**: System MUST track stream state (active/inactive) and update it in real-time
  when streams connect or disconnect.

- **FR-004**: System MUST provide an endpoint returning all currently active streams with
  their video playback URLs and metadata.

- **FR-005**: System MUST generate unique, cryptographically secure stream keys when
  requested by administrators.

- **FR-006**: System MUST allow administrators to revoke stream keys, immediately
  invalidating them for future authentication attempts.

- **FR-007**: System MUST provide video playback URLs in the formats supported by the
  third-party streaming service (HLS, RTMP source URL, WebRTC if available).

- **FR-008**: System MUST authenticate frontend requests (including administrative
  operations) using shared secret validation before returning stream data or allowing
  stream key management.

- **FR-009**: System MUST provide individual stream details including all available
  video URLs, stream metadata, and status information.

- **FR-010**: System MUST handle concurrent authentication requests without race conditions
  or duplicate stream activations.

- **FR-011**: System MUST provide webhook endpoints for MediaMTX stream lifecycle events
  (onPublish, onUnPublish) to track stream state changes in real-time.

### Key Entities

- **Stream Key**: A unique credential assigned to a broadcaster that authorizes them to
  start a stream. Attributes include: unique identifier, secret key value, status
  (active/revoked/expired), creation timestamp, expiration timestamp (optional),
  associated broadcaster information, and usage history.

- **Stream**: Represents an active or historical video broadcast session. Attributes
  include: unique identifier, associated stream key, status (active/inactive), start
  timestamp, end timestamp, video playback URLs (multiple formats), title/description,
  broadcaster metadata, and recording reference (for future correlation with object
  storage recordings in S3/GCS).

- **Broadcaster**: The entity (person or system) authorized to create streams. Attributes
  include: unique identifier, display name, associated stream keys, and contact/metadata.

## Clarifications

### Session 2026-01-17

- Q: How does MediaMTX notify our API of stream state changes? → A: Callback/webhook - MediaMTX notifies our API when streams start/stop (onPublish, onUnPublish hooks).
- Q: Should stream keys and stream history be persistently stored? → A: Persistent storage in durable database; stream history will later correlate with recorded video in object storage (S3/GCS).
- Q: How do administrators authenticate for stream key management? → A: Same shared secret as frontend; admin operations accessed through the trusted frontend application.

## Assumptions

- The third-party streaming service (MediaMTX) supports external authentication callbacks
  where it sends credentials to our endpoint for validation.
- Stream keys are the sole authentication mechanism for broadcasters (no additional
  username/password required from the streaming software).
- The frontend application is a trusted internal service authenticated via shared secrets.
- Video playback URLs are generated by/known from the third-party service and our system
  retrieves or constructs them based on stream identifiers.
- MediaMTX is configured to call webhook endpoints on our API for stream lifecycle events
  (onPublish for stream start, onUnPublish for stream end).
- Stream keys and stream history are persisted in a durable database to survive restarts,
  enable auditing, and support future correlation with recorded video in object storage.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Stream key authentication decisions are returned within 50 milliseconds
  in 95% of requests, ensuring broadcasters experience no perceptible delay when
  starting streams.

- **SC-002**: Active stream list queries return results within 50 milliseconds in 95%
  of requests, providing responsive frontend experience.

- **SC-003**: Stream state (active/inactive) is accurately reflected within 5 seconds
  of a broadcaster connecting or disconnecting.

- **SC-004**: System handles at least 100 concurrent stream authentication requests
  without degradation, supporting peak broadcasting periods.

- **SC-005**: Zero unauthorized streams are permitted - every active stream has a
  corresponding valid, authenticated stream key.

- **SC-006**: 100% of API error responses provide clear, actionable information following
  a standardized format that clients can programmatically interpret.
