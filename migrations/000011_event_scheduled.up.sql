-- Add scheduled_at column to events table
-- This allows events to be scheduled for future delivery

ALTER TABLE events ADD COLUMN scheduled_at TIMESTAMPTZ;

-- Partial index for efficient queries on scheduled events
CREATE INDEX idx_events_scheduled_at ON events(scheduled_at) WHERE scheduled_at IS NOT NULL;

COMMENT ON COLUMN events.scheduled_at IS 'When the event should be delivered (null = immediate delivery)';
