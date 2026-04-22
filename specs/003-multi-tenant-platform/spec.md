# Feature Specification: Multi-Tenant Platform

**Feature Branch**: `003-multi-tenant-platform`
**Created**: 2026-04-21
**Last Updated**: 2026-04-21 (5 contracts surfaced by frontend-spec adversarial review folded in)
**Status**: Draft
**Input**: User description: "Multi-tenant platform: organizations, teams, members, tags, devices, rooms with ACL-based access control. See specs/008-platform-v2/architecture.md in sibling frontend repo."

## Clarifications

### Session 2026-04-21

- Q: Local dev database target (Docker Postgres vs Neon branch)? → A: Testing and local development use testcontainers/Docker Postgres with `golang-migrate`; production uses NeonDB. Neon branches are not required for local contributor flow.
- Q: Makefile → justfile migration strategy? → A: Replace the Makefile wholesale with a `justfile` in a single change; update docs, `.githooks` wiring, and any CI references in the same PR (no Makefile shim kept).
- Q: Production migration execution model beyond the v1→v2 cutover? → A: Migrations are an explicit operator-run step (`just migrate-prod`) executed pre-deploy against Neon's non-pooled endpoint; the API container never runs migrations on boot, so a failed migration cannot crash-loop the service.
- Q: Task-runner migration recipes and target selection? → A: Explicit per-target recipes (`just migrate-local`, `just migrate-prod`) with separate env vars (`DATABASE_URL` for local Postgres, `DATABASE_MIGRATION_URL` for Neon non-pooled). `just migrate-prod` refuses to run unless `DATABASE_MIGRATION_URL` is set and points at a Neon non-pooled host (rejects `-pooler` hostnames) to prevent accidental prod migrations and pooled-endpoint misuse.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Platform operator provisions a new organization (Priority: P1)

A platform super-admin provisions a new Search and Rescue organization on the platform. They create the organization (e.g., "King County Search and Rescue Association"), designate one or more organization administrators by email, and hand off to those administrators to complete setup.

**Why this priority**: Nothing else in the platform is reachable until at least one organization exists. This is the first step in onboarding any customer and the foundation of multi-tenancy.

**Independent Test**: Can be fully validated by creating a new organization with an initial org-admin email, confirming the designated org-admin can sign in and see the organization dashboard, and confirming no other authenticated user sees that organization's data.

**Acceptance Scenarios**:

1. **Given** a super-admin is authenticated, **When** they create an organization with a name and an initial org-admin email, **Then** the organization exists, the org-admin can sign in and see the organization as "their org", and no non-member sees the organization.
2. **Given** an organization exists with an org-admin, **When** the super-admin adds a second org-admin email, **Then** the second person signs in and gains the same organization-wide administrative reach as the first.
3. **Given** an organization exists, **When** a super-admin suspends the organization, **Then** all members lose access to the organization's resources until the super-admin unsuspends it.

---

### User Story 2 - Member auto-joins via verified Workspace domain (Priority: P1)

A volunteer at a SAR unit signs into the platform with their unit-issued Google account. They are automatically placed in the correct team inside the correct organization, without any manual admin action, and they land on their organization's dashboard on first sign-in.

**Why this priority**: This is the primary onboarding path for end users. Without it, every new volunteer requires manual admin action — which does not scale for SAR teams that recruit and rotate members frequently.

**Independent Test**: Configure a team with a Workspace domain. Sign in with a never-before-seen user on that domain. Verify that the user is placed in the right team and organization, and that attempting to sign in as a user whose domain matches no team and who is not pre-listed as an admin results in a well-defined "awaiting access" response rather than data exposure.

**Acceptance Scenarios**:

1. **Given** an organization has a team with Workspace domain `example.org`, **When** a user signs in with email `alice@example.org` (never seen before), **Then** the user is created, placed in that team as a `member`, and shown the organization dashboard.
2. **Given** no team claims the signing-in user's email domain and the user is not pre-listed as any organization's admin, **When** they complete sign-in, **Then** they are shown an "awaiting access" response that exposes no platform data.
3. **Given** a user is already a member of an organization, **When** they sign in again, **Then** they return to the same organization and team (no re-routing or duplicate membership).

---

### User Story 3 - Org-admin manages rooms with access rules (Priority: P2)

An org-admin creates a room for an ongoing incident, chooses who can see it using a combination of team membership, member skill tags, and specific individuals, and verifies the result reflects what they intended before sharing it with the field.

**Why this priority**: Rooms are the primary runtime surface (they are what members and devices interact with), but they depend on P1 organizations and memberships existing. Without room creation there is nothing for devices to publish into or for members to view.

**Independent Test**: With a pre-provisioned organization, team, and members carrying at least two distinct tags, create a room scoped to the organization with an ACL combining one team, one tag, and one explicit member using an AND combinator; verify that only users satisfying the AND rule see the room.

**Acceptance Scenarios**:

1. **Given** an organization exists with members on two teams and two tags, **When** the org-admin creates an organization-scoped room with ACL `team=Alpha OR tag=drone-pilot`, **Then** all members of team Alpha see the room, all members tagged `drone-pilot` see the room (regardless of team), and no other member sees it.
2. **Given** the same room, **When** the org-admin switches the combinator from OR to AND, **Then** only members who are both in team Alpha and tagged `drone-pilot` see the room.
3. **Given** a team-scoped room is created under team Bravo, **When** a member of team Alpha tries to access it, **Then** access is denied even if the member would otherwise satisfy the ACL rules.
4. **Given** a private room exists, **When** an org-admin of that organization views it, **Then** they see it regardless of whether they are on the ACL.

---

### User Story 4 - Device authenticates and streams into a room (Priority: P2)

An org-admin registers a drone as a device in the organization and mints a device key. A field operator loads the key into the drone's ingest configuration; at incident time, the drone connects and publishes video into a pre-created incident room where authorized members watch it.

**Why this priority**: This is the platform's core product function — streaming — but it depends on P1 organizations/memberships and P2 rooms. Rooms without publishers have no content.

**Independent Test**: With a pre-provisioned organization and an active room, register a device, mint its key (capturing the one-time plaintext), and simulate an ingest request with that key targeting that room; verify the publish is authorized and that a second ingest request with a revoked key is rejected.

**Acceptance Scenarios**:

1. **Given** a device is registered with an active primary key, **When** the device authenticates with the correct key against an active room it is allowed to publish to, **Then** the publish is authorized and the room records the start of a stream from that device.
2. **Given** an active device key, **When** the org-admin revokes it, **Then** subsequent authentication attempts with that key are rejected within 30 seconds (per SC-005).
3. **Given** a device has two active keys (primary and secondary), **When** a client presents either one, **Then** both are accepted, enabling zero-downtime rotation.
4. **Given** a device attempts to publish into a room in a different organization, **Then** the publish is rejected even if the key is valid.
5. **Given** a device attempts to publish into an archived room, **Then** the publish is rejected.

---

### User Story 5 - Org-admin uses tags to describe members' capabilities (Priority: P3)

An org-admin defines organization-specific attribute tags (e.g., `drone-pilot`, `incident-commander`, `developer`), assigns them to members, and uses those tags to target room access without having to enumerate individual members. Tags reflect skill, role, or training status and can evolve over time.

**Why this priority**: Tags are an access-control convenience that reduces the drudgery of large-room ACL management, but they can be worked around in a pinch by listing individuals or teams. Ship P1+P2 first; tags are additive.

**Independent Test**: Create two tags, assign them independently to three members (one with both, one with each single tag), create a room whose ACL targets one tag, and verify only tagged members see the room.

**Acceptance Scenarios**:

1. **Given** a tag `drone-pilot` exists, **When** an org-admin assigns it to a member, **Then** that member is treated as matching `tag=drone-pilot` for all future ACL evaluations.
2. **Given** a tag is in use by one or more rooms' ACLs, **When** the org-admin deletes the tag, **Then** the ACL rules referencing it are removed cleanly and no room is left in an inconsistent state.
3. **Given** a tag is assigned to a member, **When** that member is removed from the organization, **Then** the tag assignment is dropped as part of the removal.

---

### User Story 6 - Org-admin reviews scoped audit log (Priority: P3)

An org-admin investigates a change they did not make — for example, a member who suddenly has access to a sensitive room — by reviewing the audit log of administrative events in their organization and seeing when, by whom, and to what the change was made.

**Why this priority**: Audit is required for trust and forensics but is not on the critical path for day-one usage. The prior audit logging work is already in place and just needs to be scoped per-organization.

**Independent Test**: Make a series of changes (create an org, add a team, assign a tag, add a user to an ACL, rotate a device key), then have an org-admin retrieve the audit log for their org and verify every change is recorded with who, when, what, and enough detail to reconstruct it; confirm a different org's audit log is inaccessible.

**Acceptance Scenarios**:

1. **Given** administrative changes have occurred in an organization, **When** an org-admin retrieves the audit log, **Then** they see every change scoped to their organization with actor, timestamp, action, and sufficient metadata to understand what changed.
2. **Given** an org-admin in organization A, **When** they attempt to retrieve audit log entries for organization B, **Then** access is denied.
3. **Given** a super-admin retrieves the audit log, **When** no organization filter is specified, **Then** entries from all organizations are returned; with a filter, only that organization's entries are returned.

---

### Edge Cases

- **Workspace domain contested by two organizations**: the platform must prevent two different organizations from claiming the same Workspace domain to avoid ambiguity on auto-join. The first claim wins; a subsequent attempt is rejected with a descriptive error to the org-admin attempting the claim.
- **Org-admin whose email's domain matches a team in a different org**: the user's explicit org-admin membership takes precedence; they are not silently cross-joined to the other organization.
- **Team deletion while members are in it**: members whose only affiliation was the deleted team are removed from the organization as part of the deletion and returned to the "awaiting access" state on their next sign-in.
- **Organization deletion**: cascades to team, tag, device, room, ACL, and stream records; the deletion itself produces a final audit entry retained for super-admin review.
- **User sign-in during an organization suspension**: the user is shown a suspended-organization response and cannot see data.
- **Last super-admin attempts self-removal**: blocked; at least one super-admin must exist at all times.
- **Device registered while the org is suspended**: rejected.
- **Simultaneous stream-start on two keys of the same device**: always permitted. Both the primary and secondary device keys are valid credentials whose entire purpose (FR-016, FR-025) is to support zero-downtime rotation where an old and new publisher may briefly run concurrently. Each concurrent publish opens its own `streams` row, and the room's device policy (FR-026: "any device in the organization" vs explicit allowlist) governs *which devices* may publish, not how many concurrent streams a single device may open.
- **Pre-v2 audit log entries after migration**: preserved and attributed to the default organization created at cutover; actor fields are preserved as recorded.
- **Room with a large ACL rule set**: room listing and per-request access checks against a stored ACL MUST inherit the standard DB-operation SLO (constitution VI: p95 <100 ms) even with hundreds of rules on a single room. The ACL-preview endpoint retains its own dedicated budget (SC-012: p95 <500 ms); the preview budget does NOT apply to the on-request access path. Evaluator is the same pure Go function in both paths (research §8), so meeting SC-012 at 500 members × handful of rules is a strong leading indicator the <100 ms budget holds at "hundreds of rules × one caller".
- **Concurrent room edits**: when two admins save ACL or metadata changes to the same room within a short window, optimistic concurrency MUST cause the second save to be rejected with a stale-version error; no silent overwrite is acceptable.
- **Force-logout during active stream**: a device stream already in progress is unaffected by a human session being force-revoked — device auth is independent of human session state. The invalidated user's active browser sessions land on sign-in within 5 seconds (per SC-011) on their next request.
- **Push channel disconnect**: a dropped SSE connection MUST be re-establishable without replaying historical events; callers MUST NOT rely on historical event replay through this channel.

## Requirements *(mandatory)*

### Functional Requirements

**Identity & hierarchy**

- **FR-001**: System MUST support super-admin, org-admin, and member as the only three role tiers.
- **FR-002**: System MUST restrict every user to exactly one organization membership at any time.
- **FR-003**: System MUST allow super-admins to create, rename, suspend, unsuspend, and delete organizations.
- **FR-004**: System MUST allow super-admins to designate and remove org-admins for any organization by email.
- **FR-005**: System MUST bootstrap an initial super-admin set from platform configuration at first run, and MUST thereafter permit super-admins to add and remove other super-admins in-app, guaranteeing that at least one super-admin always exists.
- **FR-006**: System MUST allow org-admins to create, rename, and delete teams within their own organization.
- **FR-007**: System MUST permit each team to be associated with exactly one Google Workspace domain, and MUST prevent two teams anywhere on the platform from claiming the same domain.
- **FR-008**: System MUST auto-provision membership for a user signing in with a Workspace email whose domain matches a team, placing that user in the corresponding team as a `member`.
- **FR-009**: System MUST permit org-admin memberships whose associated email does not match any team domain.

**Tags (attribute-based access)**

- **FR-010**: System MUST allow org-admins to create, rename, and delete organization-scoped tags.
- **FR-011**: System MUST allow org-admins to assign and revoke tags from members of their organization.
- **FR-012**: System MUST ensure a tag deletion removes the tag from all member assignments and from all room ACL rules referencing it, with no orphan state.

**Devices**

- **FR-013**: System MUST allow org-admins to register, edit, and remove devices within their organization. Each device MUST carry at least a human-readable name, an optional owning user, and optional free-form attributes.
- **FR-014**: System MUST mint an opaque device key at device creation, and MUST present the plaintext credential exactly once (at mint or rotation time).
- **FR-015**: System MUST store device key credentials using a one-way transform such that the plaintext cannot be recovered from the datastore.
- **FR-016**: System MUST support up to two concurrent active keys per device to enable zero-downtime rotation.
- **FR-017**: System MUST allow org-admins to revoke individual device keys independently of the device itself; revoked keys MUST be rejected on subsequent authentication within 30 seconds (per SC-005).

**Rooms & access control**

- **FR-018**: System MUST allow org-admins to create rooms scoped to either the whole organization or to a specific team within the organization.
- **FR-019**: Each room MUST carry an access control rule set whose items reference teams, tags, or specific members, and an admin-chosen combinator (AND or OR) applied across the rule set.
- **FR-020**: System MUST grant access to a room as follows: (a) super-admins always, (b) org-admins within the room's organization always, (c) for a team-scoped room, only to current members of the parent team — subject additionally to (d), (d) for any private room, only to users satisfying the ACL under the chosen combinator.
- **FR-021**: System MUST maintain a `last activity` timestamp per room, updated whenever a stream starts, a stream ends, or a member is granted entry.
- **FR-022**: System MUST auto-archive a room after a configurable inactivity interval (default 30 days) following its last activity.
- **FR-023**: Archived rooms MUST reject new streams and new entries but remain visible for audit and retrospective review.
- **FR-024**: System MUST allow org-admins to manually archive or unarchive rooms in their own organization.
- **FR-024a**: All room mutations (metadata, scope, ACL replacement, archive state) MUST use optimistic concurrency. Every room MUST carry a monotonic version. Mutation requests MUST include the expected version; the server MUST reject requests whose expected version is not current with a stale-version error and MUST NOT silently overwrite a newer version.
- **FR-024b**: System MUST expose an ACL-preview capability that accepts a draft rule set (teams, tags, members) and combinator (AND or OR) for a target organization (and optionally a target team scope), evaluates it against the current organization membership, and returns the count of members the rule set would admit. The preview MUST NOT mutate any state and MUST respond fast enough to support an interactive editor (see SC-012).

**Streaming auth**

- **FR-025**: System MUST authenticate a device stream publish request by matching the submitted credential against active device keys.
- **FR-026**: On successful device auth, System MUST additionally validate that the target room exists, belongs to the authenticated device's organization, is in active lifecycle state, and permits the device per the room's device policy (either "any device in the organization" or an explicit device allowlist).
- **FR-027**: System MUST reject any publish attempt from a device into a room outside its own organization, irrespective of credential validity.

**Stream visibility**

- **FR-027a**: System MUST expose a push channel (Server-Sent Events or equivalent long-lived HTTP stream) that delivers stream-start and stream-end events to authenticated callers. Each event MUST carry room id, stream id, device id (where applicable), and a timestamp. The channel MUST be scoped to the caller's organization and MUST respect room ACLs: a caller MUST receive events only for rooms they have access to. Events MUST be delivered within 5 seconds of the underlying transition under normal conditions (see SC-013).

**Authorization & tenancy enforcement**

- **FR-028**: System MUST reject any request from an authenticated user to read or write a resource when the resource's organization does not match the user's organization membership, unless the user is a super-admin.
- **FR-029**: System MUST return a defined "awaiting access" response to any authenticated user with no active organization membership, and MUST NOT leak resource data through such responses.
- **FR-030**: System MUST deny all access to an organization's resources when the organization is suspended, for all non-super-admin users.
- **FR-030a**: System MUST maintain a server-side session store. Every authenticated session MUST have a server-assigned identifier that can be invalidated individually, all-at-once per user, or all-at-once for all users. Administrative changes to a user's role or organization membership, organization suspension, and an explicit force-logout action MUST cause all of the affected user's active sessions to be rejected on their next request, within the freshness budget stated in SC-011.
- **FR-030b**: Org-admins MUST be able to invoke a "revoke all sessions" action against any member of their organization. The action invalidates all active sessions for that member without removing them from the organization and MUST produce an audit log entry. Super-admins MUST be able to invoke the same action against any user on the platform.

**Audit**

- **FR-031**: System MUST record an audit log entry for every change to: organizations, teams, memberships, tags, tag assignments, devices, device keys, rooms, room ACL rules, and super-admin status. Entries MUST include actor user, target organization (where applicable), action, target resource, timestamp, and metadata sufficient to reconstruct the change.
- **FR-032**: System MUST restrict audit log retrieval such that super-admins may read any entry, org-admins may read only entries in their own organization, and members may not read the audit log.

**Migration from v1**

- **FR-033**: At cutover, System MUST seed all users present in the pre-existing allowlist into a single default organization as `member`.
- **FR-034**: At cutover, System MUST drop all pre-existing broadcaster and stream-key records; any device-based replacement is re-registered by org-admins after cutover.
- **FR-035**: At cutover, System MUST preserve all pre-existing audit log entries, associating them with the default organization.

### Key Entities *(include if feature involves data)*

- **Organization**: the top-level tenant (e.g., a SAR unit or association). Owns teams, tags, devices, rooms, and members.
- **Team**: a grouping inside an organization, identified by a unique Google Workspace domain; determines auto-join.
- **User**: a human identity (one Google account); carries at most one organization membership at a time.
- **Organization Membership**: the join record placing a user in an organization with a given role (`org-admin` or `member`) and, for members, an associated team.
- **Super-admin**: a platform-wide role that transcends any organization boundary.
- **Tag**: an organization-scoped attribute (e.g., "drone-pilot") assignable to members and referenceable by room ACLs.
- **Tag Assignment**: the join record between a member and a tag.
- **Device**: an organization-owned streaming endpoint (drone, body-cam, vehicle camera) with a human-readable name, optional owner, and metadata.
- **Device Key**: the credential used by a device to authenticate a publish request. Up to two active keys per device support rotation.
- **Room**: a destination for live streams, scoped to an organization or to a team inside it, with an ACL, a lifecycle state, and an auto-archive policy.
- **Room ACL Rule**: an entry in a room's access control list targeting a team, a tag, or an individual user.
- **Audit Log Entry**: an append-only record of a platform change with actor, target organization, action, resource reference, timestamp, and metadata.

### Assumptions

- Workspace domain ownership is not independently verified by the platform; the platform trusts the super-admin to vet organizations at creation and trusts org-admins to register accurate domains for their teams.
- Every end-user identity is a Google account; no other identity provider is in scope for v1.
- The pre-v2 allowlist used at cutover contains email addresses only (no richer identity metadata).
- The platform's super-admin role is operated by the platform provider, not by any customer organization.
- Organization resources are strictly isolated; no cross-organization sharing primitives are in scope for v1.
- The default auto-archive window (30 days of inactivity) is acceptable as a starting value and is adjustable per-room by the org-admin.
- Local development and automated tests run against a Postgres 15 container (testcontainers in tests, docker-compose locally) with the same `golang-migrate` migrations that target NeonDB in production. Neon-specific features are avoided so the two environments stay schema-compatible.
- Contributor task runner is `just` (a `justfile` replaces the existing `Makefile` in this feature's work; quickstart, `.githooks` setup, and any CI references migrate in the same change).
- Production schema migrations are operator-initiated (not auto-on-boot). The deploy runbook runs `just migrate-prod` against Neon's non-pooled endpoint before rolling the Fly app; a failed migration blocks the deploy rather than crash-looping the service.
- Justfile migration recipes are target-explicit: `just migrate-local` reads `DATABASE_URL` (Docker Postgres), `just migrate-prod` reads `DATABASE_MIGRATION_URL` and refuses hostnames that are not Neon non-pooled (no `-pooler`), preventing accidental prod migrations and pooled-endpoint misuse.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A super-admin can provision a new organization (organization + at least one org-admin + at least one team with a Workspace domain) in under 5 minutes from a standing start.
- **SC-002**: A user on a registered team's Workspace domain who has never signed in before reaches their organization dashboard within 10 seconds of completing sign-in, with zero administrator action required between sign-in and dashboard access.
- **SC-003**: An org-admin can compose and save a room ACL combining at least one team, one tag, and one individual under a chosen combinator, and verify the resulting match count matches their expectation, in under 2 minutes.
- **SC-004**: 100% of device key credentials are presented in plaintext exactly once and are not retrievable from the datastore in plaintext form thereafter.
- **SC-005**: A revoked device key is rejected on a subsequent publish attempt within 30 seconds of revocation in at least 99% of cases.
- **SC-006**: A room that has exceeded its inactivity window transitions to archived and rejects new streams within 60 seconds of its scheduled archival time.
- **SC-007**: An authenticated user with no active organization membership receives the "awaiting access" response on every resource request and at no time sees platform data.
- **SC-008**: 100% of changes to identity, membership, tag, device, room, and ACL state produce a retrievable audit log entry attributed to the user that made the change.
- **SC-009**: At cutover, 100% of pre-existing allowlisted users who attempt to sign in land in the default organization without additional administrator action.
- **SC-010**: An org-admin retrieving the audit log for their organization never sees an entry attributable to another organization; a super-admin with no filter sees entries from every organization.
- **SC-011**: An affected user's active sessions are invalidated within 5 seconds (95th percentile) of any of: role change, membership removal, organization suspension, or a force-logout action, measured by the next in-flight request from the affected browser receiving an auth rejection.
- **SC-012**: The ACL-preview capability returns a match count for an organization of up to 500 members in under 500 ms (95th percentile), sustaining an interactive editing experience on the frontend.
- **SC-013**: The stream-status push channel delivers a stream-start or stream-end event to a listening authorized client within 5 seconds of the underlying transition in 95% of observed cases.
- **SC-014**: Concurrent mutations of the same room are detected by the version check 100% of the time; zero non-version-checked overwrites are possible in production.
