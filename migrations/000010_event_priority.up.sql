-- Add priority column to events table
-- Priority ranges from 1-10 where 1 is highest priority and 10 is lowest
-- Default priority is 5 (normal)

ALTER TABLE events ADD COLUMN priority INT NOT NULL DEFAULT 5;

-- Index for efficient priority-based queries
CREATE INDEX idx_events_priority ON events(priority);

-- Add constraint to ensure priority is within valid range
ALTER TABLE events ADD CONSTRAINT chk_events_priority CHECK (priority >= 1 AND priority <= 10);

COMMENT ON COLUMN events.priority IS 'Event priority 1-10 (1=highest, 10=lowest, 5=default)';
