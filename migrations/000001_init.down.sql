-- Drop trigger
DROP TRIGGER IF EXISTS update_events_updated_at ON events;

-- Drop function
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop indexes
DROP INDEX IF EXISTS idx_events_idempotency_key;
DROP INDEX IF EXISTS idx_delivery_attempts_event_id;
DROP INDEX IF EXISTS idx_events_next_attempt;
DROP INDEX IF EXISTS idx_events_status;

-- Drop tables
DROP TABLE IF EXISTS delivery_attempts;
DROP TABLE IF EXISTS events;
