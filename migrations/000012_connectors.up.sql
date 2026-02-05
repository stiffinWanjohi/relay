-- Connectors table: integration connectors for notifications and actions
-- Connectors define pre-built integrations for Slack, Discord, Teams, Email, etc.
CREATE TABLE connectors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id VARCHAR(255) NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL CHECK (type IN ('slack', 'discord', 'teams', 'email', 'webhook')),
    enabled BOOLEAN DEFAULT true,
    
    -- Configuration stored as JSON (contains webhook_url, smtp settings, etc.)
    -- Sensitive fields (passwords, tokens) should be encrypted at application level
    config JSONB NOT NULL DEFAULT '{}',
    
    -- Message template configuration
    template JSONB DEFAULT '{}',
    
    -- Timestamps
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    
    -- Unique constraint: connector name must be unique within a client
    CONSTRAINT unique_client_connector UNIQUE (client_id, name)
);

-- Index for client-based connector queries
CREATE INDEX idx_connectors_client ON connectors(client_id);

-- Index for filtering by type within a client
CREATE INDEX idx_connectors_client_type ON connectors(client_id, type);

-- Index for enabled connectors (most common query)
CREATE INDEX idx_connectors_enabled ON connectors(client_id) WHERE enabled = true;

-- Trigger for auto-updating updated_at timestamp
CREATE TRIGGER update_connectors_updated_at
    BEFORE UPDATE ON connectors
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments for documentation
COMMENT ON TABLE connectors IS 'Integration connectors for notifications (Slack, Discord, Teams, Email, Webhook)';
COMMENT ON COLUMN connectors.name IS 'Unique connector name within a client, e.g., "prod-slack", "ops-email"';
COMMENT ON COLUMN connectors.type IS 'Connector type: slack, discord, teams, email, webhook';
COMMENT ON COLUMN connectors.config IS 'JSON configuration (webhook_url, smtp_host, etc.)';
COMMENT ON COLUMN connectors.template IS 'Message template with text, title, body, color, subject fields';
