-- API keys table for authentication
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id VARCHAR(255) NOT NULL,
    key_hash VARCHAR(64) NOT NULL,  -- SHA-256 hash of the API key
    key_prefix VARCHAR(8) NOT NULL,  -- First 8 chars for identification
    name VARCHAR(255),
    scopes TEXT[] DEFAULT '{}',
    rate_limit INT DEFAULT 1000,  -- Requests per minute
    is_active BOOLEAN DEFAULT TRUE,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    
    CONSTRAINT unique_key_hash UNIQUE (key_hash),
    CONSTRAINT unique_key_prefix_client UNIQUE (client_id, key_prefix)
);

-- Index for fast key lookup by hash
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash) WHERE is_active = TRUE;

-- Index for client lookups
CREATE INDEX idx_api_keys_client_id ON api_keys(client_id);

-- Clients table for multi-tenancy
CREATE TABLE clients (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255),
    webhook_url_patterns TEXT[] DEFAULT '{}',  -- Allowed URL patterns
    max_events_per_day BIGINT DEFAULT 100000,
    is_active BOOLEAN DEFAULT TRUE,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Add client_id foreign key reference
ALTER TABLE api_keys ADD CONSTRAINT fk_api_keys_client 
    FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE;

-- Add client_id to events table for multi-tenancy
ALTER TABLE events ADD COLUMN client_id VARCHAR(255);

-- Index for client-based event queries (multi-tenancy)
CREATE INDEX idx_events_client_id ON events(client_id);
CREATE INDEX idx_events_client_status ON events(client_id, status, created_at DESC) WHERE client_id IS NOT NULL;
CREATE INDEX idx_events_client_created ON events(client_id, created_at DESC) WHERE client_id IS NOT NULL;

-- Partial index for failed events requiring attention (last 7 days)
CREATE INDEX idx_events_failed_recent ON events(client_id, created_at DESC) WHERE status = 'failed';
