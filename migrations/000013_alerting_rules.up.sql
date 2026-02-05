-- Alert rules table: configurable alerting rules for monitoring webhook delivery
-- Rules define conditions (metrics thresholds) and actions (notifications) when alerts fire
CREATE TABLE alert_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id VARCHAR(255) NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    enabled BOOLEAN DEFAULT true,
    
    -- Condition configuration
    -- metric: failure_rate, latency, queue_depth, error_count, success_rate, delivery_count
    -- operator: gt, gte, lt, lte, eq, ne
    -- value: threshold value
    -- window: time window for evaluation (stored as interval string, e.g., "5m", "1h")
    condition JSONB NOT NULL,
    
    -- Action configuration
    -- type: slack, email, webhook, pagerduty
    -- config: type-specific configuration (webhook_url, to emails, routing_key, etc.)
    -- message: optional custom message template
    action JSONB NOT NULL,
    
    -- Cooldown period between alerts (stored as interval string, e.g., "15m")
    cooldown VARCHAR(50) DEFAULT '15m',
    
    -- Timestamps
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    
    -- Unique constraint: rule name must be unique within a client
    CONSTRAINT unique_client_alert_rule UNIQUE (client_id, name)
);

-- Alert history table: records of fired alerts
CREATE TABLE alert_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id VARCHAR(255) NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    rule_id UUID REFERENCES alert_rules(id) ON DELETE SET NULL,
    rule_name VARCHAR(255) NOT NULL,
    metric VARCHAR(50) NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    threshold DOUBLE PRECISION NOT NULL,
    message TEXT,
    fired_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for alert_rules
CREATE INDEX idx_alert_rules_client ON alert_rules(client_id);
CREATE INDEX idx_alert_rules_enabled ON alert_rules(client_id) WHERE enabled = true;

-- Indexes for alert_history
CREATE INDEX idx_alert_history_client ON alert_history(client_id);
CREATE INDEX idx_alert_history_rule ON alert_history(rule_id);
CREATE INDEX idx_alert_history_fired_at ON alert_history(client_id, fired_at DESC);

-- Trigger for auto-updating updated_at timestamp
CREATE TRIGGER update_alert_rules_updated_at
    BEFORE UPDATE ON alert_rules
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Comments for documentation
COMMENT ON TABLE alert_rules IS 'Configurable alerting rules for monitoring webhook delivery metrics';
COMMENT ON COLUMN alert_rules.condition IS 'JSON condition: {metric, operator, value, window}';
COMMENT ON COLUMN alert_rules.action IS 'JSON action: {type, config, message}';
COMMENT ON COLUMN alert_rules.cooldown IS 'Minimum time between repeated alerts, e.g., "15m", "1h"';
COMMENT ON TABLE alert_history IS 'Historical record of fired alerts';
