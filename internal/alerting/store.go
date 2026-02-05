package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store provides PostgreSQL persistence for alert rules.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new alerting store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// SaveRule persists a rule to PostgreSQL.
func (s *Store) SaveRule(ctx context.Context, clientID string, rule *Rule) error {
	conditionJSON, err := json.Marshal(rule.Condition)
	if err != nil {
		return err
	}

	actionJSON, err := json.Marshal(rule.Action)
	if err != nil {
		return err
	}

	cooldownStr := rule.Cooldown.Duration().String()

	query := `
		INSERT INTO alert_rules (id, client_id, name, description, enabled, condition, action, cooldown, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			enabled = EXCLUDED.enabled,
			condition = EXCLUDED.condition,
			action = EXCLUDED.action,
			cooldown = EXCLUDED.cooldown,
			updated_at = NOW()
	`

	_, err = s.pool.Exec(ctx, query,
		rule.ID,
		clientID,
		rule.Name,
		nullString(rule.Description),
		rule.Enabled,
		conditionJSON,
		actionJSON,
		cooldownStr,
		rule.CreatedAt,
		rule.UpdatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "unique_client_alert_rule") {
			return errors.New("alert rule with this name already exists")
		}
		return err
	}

	log.Debug("rule persisted", "rule_id", rule.ID, "name", rule.Name)
	return nil
}

// DeleteRule removes a rule from PostgreSQL.
func (s *Store) DeleteRule(ctx context.Context, id uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM alert_rules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrRuleNotFound
	}
	log.Debug("rule deleted from store", "rule_id", id)
	return nil
}

// LoadRules loads all rules from PostgreSQL for a client.
func (s *Store) LoadRules(ctx context.Context, clientID string) ([]*Rule, error) {
	query := `
		SELECT id, name, description, enabled, condition, action, cooldown, created_at, updated_at
		FROM alert_rules
		WHERE client_id = $1
		ORDER BY name ASC
	`

	rows, err := s.pool.Query(ctx, query, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules, err := s.scanRules(rows)
	if err != nil {
		return nil, err
	}

	log.Info("loaded rules from store", "count", len(rules))
	return rules, nil
}

// LoadAllRules loads all rules from PostgreSQL (all clients).
func (s *Store) LoadAllRules(ctx context.Context) ([]*Rule, error) {
	query := `
		SELECT id, name, description, enabled, condition, action, cooldown, created_at, updated_at
		FROM alert_rules
		ORDER BY name ASC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules, err := s.scanRules(rows)
	if err != nil {
		return nil, err
	}

	log.Info("loaded all rules from store", "count", len(rules))
	return rules, nil
}

// GetRule retrieves a single rule from PostgreSQL.
func (s *Store) GetRule(ctx context.Context, id uuid.UUID) (*Rule, error) {
	query := `
		SELECT id, name, description, enabled, condition, action, cooldown, created_at, updated_at
		FROM alert_rules
		WHERE id = $1
	`

	rule, err := s.scanRule(s.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRuleNotFound
	}
	return rule, err
}

// SetEnabled enables or disables a rule.
func (s *Store) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	query := `UPDATE alert_rules SET enabled = $2, updated_at = NOW() WHERE id = $1`
	result, err := s.pool.Exec(ctx, query, id, enabled)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrRuleNotFound
	}
	log.Info("rule enabled state changed", "id", id, "enabled", enabled)
	return nil
}

// RecordAlert records a fired alert in the history.
func (s *Store) RecordAlert(ctx context.Context, clientID string, alert *Alert) error {
	query := `
		INSERT INTO alert_history (id, client_id, rule_id, rule_name, metric, value, threshold, message, fired_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := s.pool.Exec(ctx, query,
		alert.ID,
		clientID,
		alert.RuleID,
		alert.RuleName,
		alert.Metric,
		alert.Value,
		alert.Threshold,
		alert.Message,
		alert.FiredAt,
	)
	if err != nil {
		return err
	}

	log.Debug("alert recorded", "alert_id", alert.ID, "rule_name", alert.RuleName)
	return nil
}

// GetAlertHistory retrieves alert history for a client.
func (s *Store) GetAlertHistory(ctx context.Context, clientID string, limit int) ([]*Alert, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, rule_id, rule_name, metric, value, threshold, message, fired_at
		FROM alert_history
		WHERE client_id = $1
		ORDER BY fired_at DESC
		LIMIT $2
	`

	rows, err := s.pool.Query(ctx, query, clientID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []*Alert
	for rows.Next() {
		var a Alert
		var ruleID *uuid.UUID

		err := rows.Scan(
			&a.ID,
			&ruleID,
			&a.RuleName,
			&a.Metric,
			&a.Value,
			&a.Threshold,
			&a.Message,
			&a.FiredAt,
		)
		if err != nil {
			return nil, err
		}

		if ruleID != nil {
			a.RuleID = *ruleID
		}

		alerts = append(alerts, &a)
	}

	return alerts, rows.Err()
}

func (s *Store) scanRule(row pgx.Row) (*Rule, error) {
	var r Rule
	var description *string
	var conditionJSON, actionJSON []byte
	var cooldownStr string

	err := row.Scan(
		&r.ID,
		&r.Name,
		&description,
		&r.Enabled,
		&conditionJSON,
		&actionJSON,
		&cooldownStr,
		&r.CreatedAt,
		&r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if description != nil {
		r.Description = *description
	}

	if err := json.Unmarshal(conditionJSON, &r.Condition); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(actionJSON, &r.Action); err != nil {
		return nil, err
	}

	if cooldownDur, err := time.ParseDuration(cooldownStr); err == nil {
		r.Cooldown = Duration(cooldownDur)
	}

	return &r, nil
}

func (s *Store) scanRules(rows pgx.Rows) ([]*Rule, error) {
	var rules []*Rule
	for rows.Next() {
		var r Rule
		var description *string
		var conditionJSON, actionJSON []byte
		var cooldownStr string

		err := rows.Scan(
			&r.ID,
			&r.Name,
			&description,
			&r.Enabled,
			&conditionJSON,
			&actionJSON,
			&cooldownStr,
			&r.CreatedAt,
			&r.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if description != nil {
			r.Description = *description
		}

		if err := json.Unmarshal(conditionJSON, &r.Condition); err != nil {
			return nil, err
		}

		if err := json.Unmarshal(actionJSON, &r.Action); err != nil {
			return nil, err
		}

		if cooldownDur, err := time.ParseDuration(cooldownStr); err == nil {
			r.Cooldown = Duration(cooldownDur)
		}

		rules = append(rules, &r)
	}

	return rules, rows.Err()
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
