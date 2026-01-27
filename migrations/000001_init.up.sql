-- Events table stores all webhook events
CREATE TABLE events (
    id UUID PRIMARY KEY,
    idempotency_key VARCHAR(255) UNIQUE NOT NULL,
    destination TEXT NOT NULL,
    payload BYTEA NOT NULL,
    headers JSONB DEFAULT '{}',
    status VARCHAR(20) DEFAULT 'queued',
    attempts INT DEFAULT 0,
    max_attempts INT DEFAULT 10,
    next_attempt_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Delivery attempts table stores each delivery attempt for an event
CREATE TABLE delivery_attempts (
    id UUID PRIMARY KEY,
    event_id UUID REFERENCES events(id) ON DELETE CASCADE,
    status_code INT,
    response_body TEXT,
    error TEXT,
    duration_ms BIGINT,
    attempt_number INT,
    attempted_at TIMESTAMPTZ DEFAULT NOW()
);

-- Index for querying events by status
CREATE INDEX idx_events_status ON events(status);

-- Index for querying events ready for retry
CREATE INDEX idx_events_next_attempt ON events(next_attempt_at) WHERE status IN ('queued', 'failed');

-- Index for querying delivery attempts by event
CREATE INDEX idx_delivery_attempts_event_id ON delivery_attempts(event_id);

-- Index for idempotency key lookups
CREATE INDEX idx_events_idempotency_key ON events(idempotency_key);

-- Function to update the updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Trigger to automatically update updated_at
CREATE TRIGGER update_events_updated_at
    BEFORE UPDATE ON events
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
