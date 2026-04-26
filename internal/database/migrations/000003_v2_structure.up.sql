-- 003-multi-tenant-platform: v2 schema structure.
-- Creates organizations, teams, users, memberships, super_admins, tags,
-- devices, device_keys, rooms, room ACLs, room device allowlists, sessions.
-- Adds (nullable for now) tenancy columns to audit_logs and streams; the
-- 000004 backfill populates them and 000005 flips them to NOT NULL where
-- the data model demands it (audit_logs.organization_id stays nullable for
-- super-admin platform events; see specs/003-multi-tenant-platform/data-model.md §5.3).

-- 1.3 users -----------------------------------------------------------------
CREATE TABLE users (
    id              UUID PRIMARY KEY,
    email           VARCHAR(320) NOT NULL,
    google_subject  VARCHAR(128),
    display_name    VARCHAR(200),
    avatar_url      VARCHAR(500),
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX users_email_uniq          ON users (email);
CREATE UNIQUE INDEX users_google_subject_uniq ON users (google_subject) WHERE google_subject IS NOT NULL;

CREATE TRIGGER users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- 1.1 organizations ---------------------------------------------------------
CREATE TABLE organizations (
    id                  UUID PRIMARY KEY,
    name                VARCHAR(200) NOT NULL,
    slug                VARCHAR(64)  NOT NULL,
    status              VARCHAR(16)  NOT NULL DEFAULT 'active'
                          CHECK (status IN ('active', 'suspended')),
    created_by_user_id  UUID         NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX organizations_slug_uniq      ON organizations (slug);
CREATE        INDEX organizations_suspended_idx  ON organizations (status) WHERE status = 'suspended';

CREATE TRIGGER organizations_updated_at
    BEFORE UPDATE ON organizations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- 1.2 teams -----------------------------------------------------------------
CREATE TABLE teams (
    id                UUID PRIMARY KEY,
    organization_id   UUID         NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name              VARCHAR(200) NOT NULL,
    workspace_domain  VARCHAR(253) NOT NULL,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX teams_workspace_domain_uniq ON teams (workspace_domain);
CREATE        INDEX teams_organization_idx      ON teams (organization_id);

CREATE TRIGGER teams_updated_at
    BEFORE UPDATE ON teams
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- 1.4 organization_memberships ---------------------------------------------
CREATE TABLE organization_memberships (
    id               UUID PRIMARY KEY,
    user_id          UUID        NOT NULL REFERENCES users(id)         ON DELETE CASCADE,
    organization_id  UUID        NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    team_id          UUID                 REFERENCES teams(id)         ON DELETE RESTRICT,
    role             VARCHAR(16) NOT NULL CHECK (role IN ('org-admin', 'member')),
    joined_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX organization_memberships_user_uniq    ON organization_memberships (user_id);
CREATE        INDEX organization_memberships_org_team_idx ON organization_memberships (organization_id, team_id);
CREATE        INDEX organization_memberships_org_role_idx ON organization_memberships (organization_id, role);

-- The role/team_id CHECK ('member' => team_id IS NOT NULL) is added in 000005,
-- after the backfill has had a chance to populate any historical members.

-- 1.5 super_admins ----------------------------------------------------------
CREATE TABLE super_admins (
    id                  UUID PRIMARY KEY,
    user_id             UUID        NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    granted_by_user_id  UUID                 REFERENCES users(id),
    granted_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    seeded_from_env     BOOLEAN     NOT NULL DEFAULT FALSE
);

CREATE UNIQUE INDEX super_admins_user_uniq ON super_admins (user_id);

-- 1.6 tags ------------------------------------------------------------------
CREATE TABLE tags (
    id              UUID PRIMARY KEY,
    organization_id UUID         NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    key             VARCHAR(64)  NOT NULL,
    label           VARCHAR(200) NOT NULL,
    description     VARCHAR(500),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT tags_key_format
        CHECK (key ~ '^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$')
);

CREATE UNIQUE INDEX tags_org_key_uniq ON tags (organization_id, key);

CREATE TRIGGER tags_updated_at
    BEFORE UPDATE ON tags
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- 1.7 user_tag_assignments --------------------------------------------------
CREATE TABLE user_tag_assignments (
    id                   UUID PRIMARY KEY,
    user_id              UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tag_id               UUID        NOT NULL REFERENCES tags(id)  ON DELETE CASCADE,
    assigned_by_user_id  UUID        NOT NULL REFERENCES users(id),
    assigned_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX user_tag_assignments_user_tag_uniq ON user_tag_assignments (user_id, tag_id);
CREATE        INDEX user_tag_assignments_tag_idx       ON user_tag_assignments (tag_id);

-- 1.8 devices ---------------------------------------------------------------
CREATE TABLE devices (
    id                  UUID PRIMARY KEY,
    organization_id     UUID         NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name                VARCHAR(200) NOT NULL,
    description         VARCHAR(1000),
    owner_user_id       UUID                  REFERENCES users(id) ON DELETE SET NULL,
    metadata            JSONB        NOT NULL DEFAULT '{}',
    created_by_user_id  UUID         NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    last_seen_at        TIMESTAMPTZ
);

CREATE UNIQUE INDEX devices_org_name_uniq ON devices (organization_id, name);

CREATE TRIGGER devices_updated_at
    BEFORE UPDATE ON devices
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- 1.9 device_keys -----------------------------------------------------------
CREATE TABLE device_keys (
    id            UUID PRIMARY KEY,
    device_id     UUID        NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    key_hash      VARCHAR(64) NOT NULL,
    slot          VARCHAR(16) NOT NULL CHECK (slot   IN ('primary',  'secondary')),
    status        VARCHAR(16) NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'revoked')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ,
    last_used_at  TIMESTAMPTZ
);

CREATE UNIQUE INDEX device_keys_hash_uniq            ON device_keys (key_hash);
CREATE UNIQUE INDEX device_keys_active_slot_uniq     ON device_keys (device_id, slot) WHERE status = 'active';

-- 1.10 rooms ----------------------------------------------------------------
CREATE TABLE rooms (
    id                      UUID PRIMARY KEY,
    organization_id         UUID         NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    team_id                 UUID                  REFERENCES teams(id)         ON DELETE RESTRICT,
    name                    VARCHAR(200) NOT NULL,
    description             VARCHAR(1000),
    scope                   VARCHAR(16)  NOT NULL CHECK (scope           IN ('org', 'team')),
    lifecycle_state         VARCHAR(16)  NOT NULL DEFAULT 'active'
                              CHECK (lifecycle_state IN ('active', 'archived')),
    archive_after           INTERVAL     NOT NULL DEFAULT '30 days'::interval,
    last_activity_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    default_device_policy   VARCHAR(32)  NOT NULL DEFAULT 'any-org-device'
                              CHECK (default_device_policy IN ('any-org-device', 'allowlist')),
    acl_combinator          VARCHAR(8)   NOT NULL DEFAULT 'or'
                              CHECK (acl_combinator IN ('and', 'or')),
    version                 BIGINT       NOT NULL DEFAULT 1,
    created_by_user_id      UUID         NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    -- scope/team_id consistency: 'org' rooms have no team, 'team' rooms have one.
    CONSTRAINT rooms_scope_team_consistency
        CHECK ((scope = 'org' AND team_id IS NULL) OR (scope = 'team' AND team_id IS NOT NULL))
);

CREATE INDEX rooms_org_lifecycle_activity_idx ON rooms (organization_id, lifecycle_state, last_activity_at DESC);
CREATE INDEX rooms_org_team_idx               ON rooms (organization_id, team_id);

CREATE TRIGGER rooms_updated_at
    BEFORE UPDATE ON rooms
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- 1.11 room_acl_rules -------------------------------------------------------
-- Polymorphic target: type ∈ ('team','tag','user'). Per-row FK can't express
-- this; the matching trigger-based CHECK is added in 000005 as
-- belt-and-suspenders. Service layer is the authoritative validator.
CREATE TABLE room_acl_rules (
    id          UUID PRIMARY KEY,
    room_id     UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    type        VARCHAR(8)  NOT NULL CHECK (type IN ('team', 'tag', 'user')),
    target_id   UUID        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX room_acl_rules_room_idx          ON room_acl_rules (room_id);
CREATE INDEX room_acl_rules_type_target_idx   ON room_acl_rules (type, target_id);

-- 1.12 room_devices ---------------------------------------------------------
CREATE TABLE room_devices (
    id         UUID PRIMARY KEY,
    room_id    UUID        NOT NULL REFERENCES rooms(id)   ON DELETE CASCADE,
    device_id  UUID        NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX room_devices_room_device_uniq ON room_devices (room_id, device_id);
CREATE        INDEX room_devices_device_idx       ON room_devices (device_id);

-- 1.13 sessions -------------------------------------------------------------
CREATE TABLE sessions (
    id                UUID PRIMARY KEY,
    user_id           UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hmac_key_id       VARCHAR(32) NOT NULL,
    hmac_secret_hash  VARCHAR(64) NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at        TIMESTAMPTZ NOT NULL,
    last_used_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at        TIMESTAMPTZ,
    revoked_reason    VARCHAR(64),
    client_ip         INET,
    user_agent        VARCHAR(500)
);

CREATE UNIQUE INDEX sessions_hmac_key_id_uniq         ON sessions (hmac_key_id);
CREATE        INDEX sessions_user_active_idx          ON sessions (user_id)    WHERE revoked_at IS NULL;
CREATE        INDEX sessions_expires_active_idx       ON sessions (expires_at) WHERE revoked_at IS NULL;

-- 2. Modified tables --------------------------------------------------------

-- 2.1 streams: nullable now; flipped to NOT NULL in 000005 after backfill.
ALTER TABLE streams
    ADD COLUMN organization_id UUID REFERENCES organizations(id),
    ADD COLUMN room_id         UUID REFERENCES rooms(id),
    ADD COLUMN device_id       UUID REFERENCES devices(id);

-- 2.2 audit_logs: organization_id intentionally remains nullable (super-admin
-- platform events legitimately carry NULL — see data-model §5.3 step 1).
ALTER TABLE audit_logs
    ADD COLUMN organization_id UUID REFERENCES organizations(id),
    ADD COLUMN actor_user_id   UUID REFERENCES users(id);

-- 5.1 step 4: seed the platform placeholder user + the default organization.
-- These rows back-stop FKs from audit_logs (when 000004 backfills) and from
-- any historical actor_user_id derivation. Both rows use deterministic v4-shaped
-- sentinel UUIDs so subsequent migrations and the migrate binary can reference
-- them without an extra SELECT.
INSERT INTO users (id, email, display_name)
VALUES (
    '00000000-0000-4000-8000-000000000001',
    'platform@rescue.stream',
    'Platform'
);

INSERT INTO organizations (id, name, slug, created_by_user_id)
VALUES (
    '00000000-0000-4000-8000-000000000002',
    'RescueStream Default',
    'default',
    '00000000-0000-4000-8000-000000000001'
);

-- 5.1 step 5: backfill users from distinct non-null audit_logs.actor values
-- where the actor looks like an email. These rows have google_subject NULL;
-- the login flow upgrades them on first sign-in by matching email.
INSERT INTO users (id, email)
SELECT uuid_generate_v4(), lower(actor)
FROM (
    SELECT DISTINCT actor
    FROM audit_logs
    WHERE actor IS NOT NULL
      AND actor LIKE '%@%'
) src
ON CONFLICT (email) DO NOTHING;
