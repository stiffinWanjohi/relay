package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRule_Validate(t *testing.T) {
	tests := []struct {
		name    string
		rule    *Rule
		wantErr bool
	}{
		{
			name: "valid slack rule",
			rule: &Rule{
				Name: "High Failure Rate",
				Condition: Condition{
					Metric:   ConditionTypeFailureRate,
					Operator: OperatorGreaterThan,
					Value:    0.1,
					Window:   Duration(5 * time.Minute),
				},
				Action: Action{
					Type: ActionTypeSlack,
					Config: ActionConfig{
						WebhookURL: "https://hooks.slack.com/xxx",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			rule: &Rule{
				Condition: Condition{
					Metric:   ConditionTypeFailureRate,
					Operator: OperatorGreaterThan,
					Value:    0.1,
				},
				Action: Action{
					Type: ActionTypeSlack,
					Config: ActionConfig{
						WebhookURL: "https://hooks.slack.com/xxx",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid metric",
			rule: &Rule{
				Name: "Test",
				Condition: Condition{
					Metric:   "invalid",
					Operator: OperatorGreaterThan,
					Value:    0.1,
				},
				Action: Action{
					Type: ActionTypeSlack,
					Config: ActionConfig{
						WebhookURL: "https://hooks.slack.com/xxx",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "slack without webhook",
			rule: &Rule{
				Name: "Test",
				Condition: Condition{
					Metric:   ConditionTypeFailureRate,
					Operator: OperatorGreaterThan,
					Value:    0.1,
				},
				Action: Action{
					Type:   ActionTypeSlack,
					Config: ActionConfig{},
				},
			},
			wantErr: true,
		},
		{
			name: "email without recipients",
			rule: &Rule{
				Name: "Test",
				Condition: Condition{
					Metric:   ConditionTypeFailureRate,
					Operator: OperatorGreaterThan,
					Value:    0.1,
				},
				Action: Action{
					Type:   ActionTypeEmail,
					Config: ActionConfig{},
				},
			},
			wantErr: true,
		},
		{
			name: "valid email rule",
			rule: &Rule{
				Name: "Test",
				Condition: Condition{
					Metric:   ConditionTypeFailureRate,
					Operator: OperatorGreaterThan,
					Value:    0.1,
				},
				Action: Action{
					Type: ActionTypeEmail,
					Config: ActionConfig{
						To: []string{"admin@example.com"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid webhook rule",
			rule: &Rule{
				Name: "Test",
				Condition: Condition{
					Metric:   ConditionTypeQueueDepth,
					Operator: OperatorGreaterThan,
					Value:    1000,
				},
				Action: Action{
					Type: ActionTypeWebhook,
					Config: ActionConfig{
						URL: "https://example.com/webhook",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid pagerduty rule",
			rule: &Rule{
				Name: "Test",
				Condition: Condition{
					Metric:   ConditionTypeLatency,
					Operator: OperatorGreaterThan,
					Value:    5000,
				},
				Action: Action{
					Type: ActionTypePagerDuty,
					Config: ActionConfig{
						RoutingKey: "abc123",
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.rule.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRule_Evaluate(t *testing.T) {
	tests := []struct {
		name      string
		operator  Operator
		threshold float64
		value     float64
		expected  bool
	}{
		{"gt true", OperatorGreaterThan, 10, 15, true},
		{"gt false", OperatorGreaterThan, 10, 5, false},
		{"gt equal false", OperatorGreaterThan, 10, 10, false},
		{"gte true", OperatorGreaterThanOrEqual, 10, 10, true},
		{"gte above", OperatorGreaterThanOrEqual, 10, 15, true},
		{"lt true", OperatorLessThan, 10, 5, true},
		{"lt false", OperatorLessThan, 10, 15, false},
		{"lte true", OperatorLessThanOrEqual, 10, 10, true},
		{"eq true", OperatorEqual, 10, 10, true},
		{"eq false", OperatorEqual, 10, 5, false},
		{"ne true", OperatorNotEqual, 10, 5, true},
		{"ne false", OperatorNotEqual, 10, 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &Rule{
				Condition: Condition{
					Operator: tt.operator,
					Value:    tt.threshold,
				},
			}
			result := rule.Evaluate(tt.value)
			if result != tt.expected {
				t.Errorf("Evaluate(%f) = %v, expected %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestRule_Cooldown(t *testing.T) {
	rule := &Rule{
		Cooldown: Duration(100 * time.Millisecond),
	}

	// Initially should be able to fire
	if !rule.CanFire() {
		t.Error("expected to be able to fire initially")
	}

	// Mark as fired
	rule.MarkFired()

	// Should not be able to fire immediately
	if rule.CanFire() {
		t.Error("expected cooldown to prevent firing")
	}

	// Wait for cooldown
	time.Sleep(150 * time.Millisecond)

	// Should be able to fire again
	if !rule.CanFire() {
		t.Error("expected to be able to fire after cooldown")
	}
}

type mockMetricsProvider struct {
	failureRate   float64
	latency       float64
	queueDepth    int64
	errorCount    int64
	successRate   float64
	deliveryCount int64
}

func (m *mockMetricsProvider) GetFailureRate(ctx context.Context, window time.Duration) (float64, error) {
	return m.failureRate, nil
}

func (m *mockMetricsProvider) GetAverageLatency(ctx context.Context, window time.Duration) (float64, error) {
	return m.latency, nil
}

func (m *mockMetricsProvider) GetQueueDepth(ctx context.Context) (int64, error) {
	return m.queueDepth, nil
}

func (m *mockMetricsProvider) GetErrorCount(ctx context.Context, window time.Duration) (int64, error) {
	return m.errorCount, nil
}

func (m *mockMetricsProvider) GetSuccessRate(ctx context.Context, window time.Duration) (float64, error) {
	return m.successRate, nil
}

func (m *mockMetricsProvider) GetDeliveryCount(ctx context.Context, window time.Duration) (int64, error) {
	return m.deliveryCount, nil
}

func TestEngine_AddRemoveRule(t *testing.T) {
	engine := NewEngine(&mockMetricsProvider{}, DefaultEngineConfig())

	rule := NewRule("Test Rule", Condition{
		Metric:   ConditionTypeFailureRate,
		Operator: OperatorGreaterThan,
		Value:    0.1,
	}, Action{
		Type: ActionTypeSlack,
		Config: ActionConfig{
			WebhookURL: "https://hooks.slack.com/xxx",
		},
	})

	// Add rule
	if err := engine.AddRule(rule); err != nil {
		t.Fatalf("AddRule() error = %v", err)
	}

	// Get rule
	retrieved, err := engine.GetRule(rule.ID)
	if err != nil {
		t.Fatalf("GetRule() error = %v", err)
	}
	if retrieved.Name != rule.Name {
		t.Errorf("expected name %s, got %s", rule.Name, retrieved.Name)
	}

	// List rules
	rules := engine.ListRules()
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}

	// Remove rule
	if err := engine.RemoveRule(rule.ID); err != nil {
		t.Fatalf("RemoveRule() error = %v", err)
	}

	// Should not find rule
	_, err = engine.GetRule(rule.ID)
	if err != ErrRuleNotFound {
		t.Errorf("expected ErrRuleNotFound, got %v", err)
	}
}

func TestEngine_EnableDisableRule(t *testing.T) {
	engine := NewEngine(&mockMetricsProvider{}, DefaultEngineConfig())

	rule := NewRule("Test Rule", Condition{
		Metric:   ConditionTypeFailureRate,
		Operator: OperatorGreaterThan,
		Value:    0.1,
	}, Action{
		Type: ActionTypeSlack,
		Config: ActionConfig{
			WebhookURL: "https://hooks.slack.com/xxx",
		},
	})

	_ = engine.AddRule(rule)

	// Disable
	if err := engine.DisableRule(rule.ID); err != nil {
		t.Fatalf("DisableRule() error = %v", err)
	}

	retrieved, _ := engine.GetRule(rule.ID)
	if retrieved.Enabled {
		t.Error("expected rule to be disabled")
	}

	// Enable
	if err := engine.EnableRule(rule.ID); err != nil {
		t.Fatalf("EnableRule() error = %v", err)
	}

	retrieved, _ = engine.GetRule(rule.ID)
	if !retrieved.Enabled {
		t.Error("expected rule to be enabled")
	}
}

func TestEngine_FireSlackAlert(t *testing.T) {
	var receivedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	metrics := &mockMetricsProvider{
		failureRate: 0.5, // 50% failure rate - should trigger
	}

	engine := NewEngine(metrics, DefaultEngineConfig())

	rule := NewRule("High Failure Rate", Condition{
		Metric:   ConditionTypeFailureRate,
		Operator: OperatorGreaterThan,
		Value:    0.1,
		Window:   Duration(5 * time.Minute),
	}, Action{
		Type: ActionTypeSlack,
		Config: ActionConfig{
			WebhookURL: server.URL,
			Channel:    "#alerts",
		},
	})

	_ = engine.AddRule(rule)

	// Evaluate
	ctx := context.Background()
	engine.EvaluateNow(ctx)

	// Check that Slack received the alert
	if receivedPayload == nil {
		t.Fatal("expected Slack to receive payload")
	}
	if receivedPayload["channel"] != "#alerts" {
		t.Errorf("expected channel #alerts, got %v", receivedPayload["channel"])
	}
	if receivedPayload["text"] == nil {
		t.Error("expected text in payload")
	}
}

func TestEngine_FireWebhookAlert(t *testing.T) {
	var receivedAlert Alert
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedAlert)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	metrics := &mockMetricsProvider{
		queueDepth: 5000, // Should trigger
	}

	engine := NewEngine(metrics, DefaultEngineConfig())

	rule := NewRule("High Queue Depth", Condition{
		Metric:   ConditionTypeQueueDepth,
		Operator: OperatorGreaterThan,
		Value:    1000,
	}, Action{
		Type: ActionTypeWebhook,
		Config: ActionConfig{
			URL: server.URL,
			Headers: map[string]string{
				"X-Custom": "header",
			},
		},
	})

	_ = engine.AddRule(rule)

	ctx := context.Background()
	engine.EvaluateNow(ctx)

	if receivedAlert.RuleName != "High Queue Depth" {
		t.Errorf("expected rule name 'High Queue Depth', got %s", receivedAlert.RuleName)
	}
	if receivedAlert.Value != 5000 {
		t.Errorf("expected value 5000, got %f", receivedAlert.Value)
	}
}

func TestEngine_AlertHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	metrics := &mockMetricsProvider{
		failureRate: 0.5,
	}

	cfg := DefaultEngineConfig()
	cfg.MaxHistory = 10
	engine := NewEngine(metrics, cfg)

	rule := NewRule("Test", Condition{
		Metric:   ConditionTypeFailureRate,
		Operator: OperatorGreaterThan,
		Value:    0.1,
	}, Action{
		Type: ActionTypeSlack,
		Config: ActionConfig{
			WebhookURL: server.URL,
		},
	})
	rule.Cooldown = Duration(0) // No cooldown for testing

	_ = engine.AddRule(rule)

	ctx := context.Background()

	// Fire multiple alerts
	for range 5 {
		engine.EvaluateNow(ctx)
	}

	history := engine.GetAlertHistory(0)
	if len(history) != 5 {
		t.Errorf("expected 5 alerts in history, got %d", len(history))
	}

	// Check that history is in reverse chronological order
	for i := 0; i < len(history)-1; i++ {
		if history[i].FiredAt.Before(history[i+1].FiredAt) {
			t.Error("expected history to be in reverse chronological order")
		}
	}
}

func TestEngine_CooldownPreventsAlert(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	metrics := &mockMetricsProvider{
		failureRate: 0.5,
	}

	engine := NewEngine(metrics, DefaultEngineConfig())

	rule := NewRule("Test", Condition{
		Metric:   ConditionTypeFailureRate,
		Operator: OperatorGreaterThan,
		Value:    0.1,
	}, Action{
		Type: ActionTypeSlack,
		Config: ActionConfig{
			WebhookURL: server.URL,
		},
	})
	rule.Cooldown = Duration(1 * time.Hour) // Long cooldown

	_ = engine.AddRule(rule)

	ctx := context.Background()

	// First evaluation should fire
	engine.EvaluateNow(ctx)

	// Second evaluation should be blocked by cooldown
	engine.EvaluateNow(ctx)

	if callCount != 1 {
		t.Errorf("expected 1 call (cooldown should block second), got %d", callCount)
	}
}

func TestDuration_JSON(t *testing.T) {
	d := Duration(5 * time.Minute)

	// Marshal
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	if string(data) != `"5m0s"` {
		t.Errorf("expected \"5m0s\", got %s", string(data))
	}

	// Unmarshal
	var d2 Duration
	if err := json.Unmarshal([]byte(`"10m"`), &d2); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if d2.Duration() != 10*time.Minute {
		t.Errorf("expected 10m, got %v", d2.Duration())
	}
}

func TestNewRule(t *testing.T) {
	rule := NewRule("Test", Condition{
		Metric:   ConditionTypeFailureRate,
		Operator: OperatorGreaterThan,
		Value:    0.1,
	}, Action{
		Type: ActionTypeSlack,
		Config: ActionConfig{
			WebhookURL: "https://example.com",
		},
	})

	if rule.ID == uuid.Nil {
		t.Error("expected ID to be set")
	}
	if rule.Name != "Test" {
		t.Errorf("expected name 'Test', got %s", rule.Name)
	}
	if !rule.Enabled {
		t.Error("expected rule to be enabled by default")
	}
	if rule.Cooldown.Duration() != 15*time.Minute {
		t.Errorf("expected default cooldown of 15m, got %v", rule.Cooldown.Duration())
	}
}
