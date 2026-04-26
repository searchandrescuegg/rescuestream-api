-- API Keys table for admin authorization
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    key_identifier VARCHAR(255) NOT NULL UNIQUE,
    description VARCHAR(500),
    is_admin BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);

CREATE INDEX idx_api_keys_identifier ON api_keys(key_identifier);

-- Audit logs table
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    actor VARCHAR(255) NOT NULL,
    action VARCHAR(50) NOT NULL,
    resource_type VARCHAR(50),
    resource_id UUID,
    request_method VARCHAR(10) NOT NULL,
    request_path VARCHAR(1024) NOT NULL,
    ip_address VARCHAR(45) NOT NULL,
    outcome VARCHAR(20) NOT NULL DEFAULT 'success',
    failure_reason TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    request_id VARCHAR(36)
);

-- Indexes for query performance
CREATE INDEX idx_audit_logs_timestamp ON audit_logs(timestamp DESC);
CREATE INDEX idx_audit_logs_actor ON audit_logs(actor);
CREATE INDEX idx_audit_logs_resource ON audit_logs(resource_type, resource_id);
CREATE INDEX idx_audit_logs_action ON audit_logs(action);
CREATE INDEX idx_audit_logs_composite ON audit_logs(resource_type, action, timestamp DESC);

-- Partial index for filtering by outcome
CREATE INDEX idx_audit_logs_failures ON audit_logs(timestamp DESC) WHERE outcome = 'failure';
