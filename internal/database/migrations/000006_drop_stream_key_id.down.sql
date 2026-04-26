-- Reverse 000006_drop_stream_key_id.up.sql.
-- Recreates the column as nullable; the original FK to stream_keys cannot be
-- restored because that table was dropped at cutover (FR-034).

ALTER TABLE streams ADD COLUMN IF NOT EXISTS stream_key_id UUID;
