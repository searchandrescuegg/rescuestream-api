-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Broadcasters table
CREATE TABLE broadcasters (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    display_name VARCHAR(255) NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Stream keys table
CREATE TABLE stream_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    key_value VARCHAR(64) NOT NULL UNIQUE,
    broadcaster_id UUID NOT NULL REFERENCES broadcasters(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked', 'expired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ
);

CREATE INDEX idx_stream_keys_broadcaster ON stream_keys(broadcaster_id);
CREATE INDEX idx_stream_keys_status ON stream_keys(status);

-- Streams table
CREATE TABLE streams (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    stream_key_id UUID NOT NULL REFERENCES stream_keys(id) ON DELETE CASCADE,
    path VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'ended')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    source_type VARCHAR(50),
    source_id VARCHAR(255),
    metadata JSONB NOT NULL DEFAULT '{}',
    recording_ref VARCHAR(512)
);

CREATE INDEX idx_streams_stream_key ON streams(stream_key_id);
CREATE INDEX idx_streams_status ON streams(status);
CREATE INDEX idx_streams_path ON streams(path);
CREATE INDEX idx_streams_active_ordered ON streams(status, started_at DESC) WHERE status = 'active';

-- Ensure only one active stream per key
CREATE UNIQUE INDEX idx_streams_one_active_per_key
    ON streams(stream_key_id)
    WHERE status = 'active';

-- Updated_at trigger for broadcasters
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_broadcasters_updated_at
    BEFORE UPDATE ON broadcasters
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
