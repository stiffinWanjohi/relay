-- Drop alerting tables and related objects
DROP TRIGGER IF EXISTS update_alert_rules_updated_at ON alert_rules;
DROP TABLE IF EXISTS alert_history;
DROP TABLE IF EXISTS alert_rules;
