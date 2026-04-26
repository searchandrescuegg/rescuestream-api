-- Reverse 000003_v2_structure.up.sql.
-- Drops the new tables in reverse FK order and removes the columns added to
-- streams and audit_logs. Does NOT attempt to repopulate the dropped tables
-- — running this destroys all multi-tenant state.

-- Reverse 2.x: column drops on existing tables.
ALTER TABLE audit_logs
    DROP COLUMN IF EXISTS actor_user_id,
    DROP COLUMN IF EXISTS organization_id;

ALTER TABLE streams
    DROP COLUMN IF EXISTS device_id,
    DROP COLUMN IF EXISTS room_id,
    DROP COLUMN IF EXISTS organization_id;

-- Reverse 1.x: drop new tables in dependency-respecting order.
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS room_devices;
DROP TABLE IF EXISTS room_acl_rules;
DROP TABLE IF EXISTS rooms;
DROP TABLE IF EXISTS device_keys;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS user_tag_assignments;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS super_admins;
DROP TABLE IF EXISTS organization_memberships;
DROP TABLE IF EXISTS teams;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS users;
