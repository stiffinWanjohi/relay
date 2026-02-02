-- Event types table: registry for event types with optional JSONSchema validation
-- Event types define the structure and documentation for events that can be sent
-- through the webhook system.
CREATE TABLE event_types (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id VARCHAR(255) NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    schema JSONB,  -- JSONSchema for payload validation (optional)
    schema_version VARCHAR(20),  -- Schema version, e.g., "1.0", "1.1"
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    
    -- Unique constraint: event type name must be unique within a client
    CONSTRAINT unique_client_event_type UNIQUE (client_id, name)
);

-- Index for client-based event type queries
CREATE INDEX idx_event_types_client ON event_types(client_id);

-- Index for event type name lookup within a client
CREATE INDEX idx_event_types_client_name ON event_types(client_id, name);

-- Trigger for auto-updating updated_at timestamp
CREATE TRIGGER update_event_types_updated_at
    BEFORE UPDATE ON event_types
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comment on table and columns for documentation
COMMENT ON TABLE event_types IS 'Registry of event types with optional JSONSchema validation';
COMMENT ON COLUMN event_types.name IS 'Event type name, e.g., "order.created", "user.signup"';
COMMENT ON COLUMN event_types.schema IS 'Optional JSONSchema for validating event payloads';
COMMENT ON COLUMN event_types.schema_version IS 'Version of the schema, e.g., "1.0", "1.1"';
