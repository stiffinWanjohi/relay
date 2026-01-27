-- Outbox table for reliable event publishing
-- Events are written to this table in the same transaction as the events table
-- A background worker processes the outbox and publishes to Redis queue
CREATE TABLE outbox (
    id UUID PRIMARY KEY,
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    attempts INT DEFAULT 0,
    last_error TEXT
);

-- Index for finding unprocessed outbox entries
CREATE INDEX idx_outbox_unprocessed ON outbox(created_at) WHERE processed_at IS NULL;

-- Index for cleanup of processed entries
CREATE INDEX idx_outbox_processed ON outbox(processed_at) WHERE processed_at IS NOT NULL;
