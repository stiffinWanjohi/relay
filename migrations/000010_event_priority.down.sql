-- Remove priority column from events table

ALTER TABLE events DROP CONSTRAINT IF EXISTS chk_events_priority;
DROP INDEX IF EXISTS idx_events_priority;
ALTER TABLE events DROP COLUMN IF EXISTS priority;
