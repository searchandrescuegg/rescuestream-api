-- 003-multi-tenant-platform: backfill v1 data into v2 tenancy and drop v1 tables.
-- This migration is one-way per FR-034 ("drop all pre-existing broadcaster
-- and stream-key records; any device-based replacement is re-registered by
-- org-admins after cutover"). The matching .down.sql is intentionally a no-op.

-- 5.2 step 1: associate every existing audit_logs row with the default org
-- (sentinel UUID seeded in 000003) and resolve actor_user_id from the email
-- backfill done in 000003. Rows whose actor doesn't look like an email keep
-- actor_user_id NULL — that's the "actor cannot be resolved" case noted in
-- the data-model.
UPDATE audit_logs
SET organization_id = '00000000-0000-4000-8000-000000000002';

UPDATE audit_logs a
SET actor_user_id = u.id
FROM users u
WHERE u.email = lower(a.actor)
  AND a.actor_user_id IS NULL;

-- 5.2 step 2: per FR-034, broadcasters and stream_keys go away wholesale.
-- The streams table is preserved structurally but truncated, since no v2
-- stream row can carry the (organization_id, room_id, device_id) columns
-- without a matching v2 device — and v2 devices are re-registered after
-- cutover by org-admins.
TRUNCATE TABLE streams;

DROP TABLE IF EXISTS stream_keys CASCADE;
DROP TABLE IF EXISTS broadcasters CASCADE;
