-- Remove scheduled_at column from events table

DROP INDEX IF EXISTS idx_events_scheduled_at;
ALTER TABLE events DROP COLUMN IF EXISTS scheduled_at;
