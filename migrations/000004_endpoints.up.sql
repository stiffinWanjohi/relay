-- Endpoints table: webhook destinations that subscribe to event types
-- Each endpoint has its own delivery configuration (retries, timeout, rate limit, circuit breaker)
CREATE TABLE endpoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id VARCHAR(255) NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    description TEXT,
    event_types TEXT[] DEFAULT '{}',  -- Empty array means subscribe to all
    status VARCHAR(50) DEFAULT 'active' CHECK (status IN ('active', 'paused', 'disabled')),
    
    -- Retry configuration
    max_retries INT DEFAULT 10 CHECK (max_retries >= 0 AND max_retries <= 100),
    retry_backoff_ms INT DEFAULT 1000 CHECK (retry_backoff_ms >= 100),
    retry_backoff_max INT DEFAULT 86400000 CHECK (retry_backoff_max >= retry_backoff_ms),
    retry_backoff_mult FLOAT DEFAULT 2.0 CHECK (retry_backoff_mult >= 1.0 AND retry_backoff_mult <= 10.0),
    
    -- Delivery configuration
    timeout_ms INT DEFAULT 30000 CHECK (timeout_ms >= 1000 AND timeout_ms <= 300000),
    rate_limit_per_sec INT DEFAULT 0 CHECK (rate_limit_per_sec >= 0),  -- 0 = unlimited
    
    -- Circuit breaker configuration
    circuit_threshold INT DEFAULT 5 CHECK (circuit_threshold >= 1),
    circuit_reset_ms INT DEFAULT 300000 CHECK (circuit_reset_ms >= 1000),
    
    -- Custom headers (JSON object of key-value pairs)
    custom_headers JSONB DEFAULT '{}',
    
    -- Timestamps
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Index for client-based endpoint queries
CREATE INDEX idx_endpoints_client ON endpoints(client_id);

-- Index for filtering by status within a client
CREATE INDEX idx_endpoints_client_status ON endpoints(client_id, status);

-- GIN index for efficient event type matching (array containment queries)
CREATE INDEX idx_endpoints_event_types ON endpoints USING GIN(event_types);

-- Index for active endpoints (most common query)
CREATE INDEX idx_endpoints_active ON endpoints(client_id) WHERE status = 'active';

-- Add event_type column to events table for routing
ALTER TABLE events ADD COLUMN event_type VARCHAR(255);

-- Add endpoint_id column to events table for tracking which endpoint the event targets
ALTER TABLE events ADD COLUMN endpoint_id UUID REFERENCES endpoints(id) ON DELETE SET NULL;

-- Index for event type queries within a client
CREATE INDEX idx_events_event_type ON events(client_id, event_type) WHERE event_type IS NOT NULL;

-- Index for endpoint-based event queries
CREATE INDEX idx_events_endpoint ON events(endpoint_id, status) WHERE endpoint_id IS NOT NULL;

-- Index for endpoint delivery history (recent events per endpoint)
CREATE INDEX idx_events_endpoint_created ON events(endpoint_id, created_at DESC) WHERE endpoint_id IS NOT NULL;

-- Add foreign key constraint from events.client_id to clients.id (if not already exists)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints 
        WHERE constraint_name = 'fk_events_client' 
        AND table_name = 'events'
    ) THEN
        ALTER TABLE events ADD CONSTRAINT fk_events_client 
            FOREIGN KEY (client_id) REFERENCES clients(id) ON DELETE CASCADE;
    END IF;
END $$;
