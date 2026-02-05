-- Drop connectors table and related objects
DROP TRIGGER IF EXISTS update_connectors_updated_at ON connectors;
DROP TABLE IF EXISTS connectors;
