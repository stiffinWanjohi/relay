-- Drop event types table and related objects
DROP TRIGGER IF EXISTS update_event_types_updated_at ON event_types;
DROP TABLE IF EXISTS event_types;
