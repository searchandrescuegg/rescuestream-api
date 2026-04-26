-- Reverse 000005_v2_constraints.up.sql.

DROP TRIGGER  IF EXISTS room_acl_rules_validate_target_trg ON room_acl_rules;
DROP FUNCTION IF EXISTS room_acl_rules_validate_target();

ALTER TABLE organization_memberships
    DROP CONSTRAINT IF EXISTS organization_memberships_member_requires_team;

DROP INDEX IF EXISTS audit_logs_actor_user_timestamp_idx;
DROP INDEX IF EXISTS audit_logs_org_timestamp_idx;
DROP INDEX IF EXISTS streams_device_status_idx;
DROP INDEX IF EXISTS streams_org_room_status_idx;

ALTER TABLE streams
    ALTER COLUMN device_id       DROP NOT NULL,
    ALTER COLUMN room_id         DROP NOT NULL,
    ALTER COLUMN organization_id DROP NOT NULL;
