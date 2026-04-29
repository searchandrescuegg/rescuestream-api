-- 003-multi-tenant-platform: drop the now-orphaned streams.stream_key_id
-- column. The 000004 migration removed the stream_keys table CASCADE, which
-- dropped the FK constraint but left the column in place. This migration
-- finishes the column removal per data-model §2.1 ("Drop: stream_key_id").

ALTER TABLE streams DROP COLUMN IF EXISTS stream_key_id;

-- The v1 partial unique index `idx_streams_one_active_per_key` was attached
-- to the now-departed stream_key_id column; CASCADE in 000004 removed it
-- transitively. The matching v2 invariant (one active stream per ROOM) is
-- enforced by an index added in a future room-stream wiring migration.
