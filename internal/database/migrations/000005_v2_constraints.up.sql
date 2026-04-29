-- 003-multi-tenant-platform: lock down v2 invariants now that 000004 has
-- fully populated the new tenancy columns.

-- 5.3 step 2: streams must always belong to an org/room/device in v2.
-- audit_logs.organization_id intentionally remains nullable (super-admin
-- platform events legitimately carry NULL — see data-model §5.3 step 1).
ALTER TABLE streams
    ALTER COLUMN organization_id SET NOT NULL,
    ALTER COLUMN room_id         SET NOT NULL,
    ALTER COLUMN device_id       SET NOT NULL;

-- 5.3 step 3: indexes that depend on the now-NOT-NULL columns.
CREATE INDEX streams_org_room_status_idx ON streams (organization_id, room_id, status);
CREATE INDEX streams_device_status_idx   ON streams (device_id, status);

-- audit_logs lookup indexes (data-model §2.2).
CREATE INDEX audit_logs_org_timestamp_idx       ON audit_logs (organization_id, timestamp DESC);
CREATE INDEX audit_logs_actor_user_timestamp_idx ON audit_logs (actor_user_id,   timestamp DESC);

-- 5.3 step 4: 'member' rows MUST carry a team. Org-admins (and only org-admins)
-- may have NULL team_id (FR-009). Deferred from 000003 to avoid ordering
-- issues with the audit-actor backfill (which doesn't create memberships).
ALTER TABLE organization_memberships
    ADD CONSTRAINT organization_memberships_member_requires_team
        CHECK (role <> 'member' OR team_id IS NOT NULL);

-- Polymorphic CHECK on room_acl_rules: target_id must exist in the right
-- table for its type. Service layer is the primary validator; this trigger
-- is defense-in-depth so a buggy repo can't insert a dangling rule.
CREATE OR REPLACE FUNCTION room_acl_rules_validate_target() RETURNS TRIGGER AS $$
DECLARE
    found INTEGER;
BEGIN
    CASE NEW.type
        WHEN 'team' THEN
            SELECT 1 INTO found FROM teams WHERE id = NEW.target_id;
        WHEN 'tag' THEN
            SELECT 1 INTO found FROM tags  WHERE id = NEW.target_id;
        WHEN 'user' THEN
            SELECT 1 INTO found FROM users WHERE id = NEW.target_id;
        ELSE
            RAISE EXCEPTION 'unknown room_acl_rules.type: %', NEW.type;
    END CASE;
    IF found IS NULL THEN
        RAISE EXCEPTION 'room_acl_rules: target_id % not found in % table',
            NEW.target_id, NEW.type;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER room_acl_rules_validate_target_trg
    BEFORE INSERT OR UPDATE ON room_acl_rules
    FOR EACH ROW EXECUTE FUNCTION room_acl_rules_validate_target();
