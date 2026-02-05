package rest

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/alerting"
)

// AlertRuleRequest represents the request body for creating/updating an alert rule.
type AlertRuleRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Enabled     *bool                  `json:"enabled,omitempty"`
	Condition   *AlertConditionRequest `json:"condition"`
	Action      *AlertActionRequest    `json:"action"`
	Cooldown    string                 `json:"cooldown,omitempty"`
}

// AlertConditionRequest represents the condition for an alert rule.
type AlertConditionRequest struct {
	Metric   string  `json:"metric"`
	Operator string  `json:"operator"`
	Value    float64 `json:"value"`
	Window   string  `json:"window"`
}

// AlertActionRequest represents the action for an alert rule.
type AlertActionRequest struct {
	Type       string            `json:"type"`
	WebhookURL string            `json:"webhookUrl,omitempty"`
	Channel    string            `json:"channel,omitempty"`
	To         []string          `json:"to,omitempty"`
	Subject    string            `json:"subject,omitempty"`
	RoutingKey string            `json:"routingKey,omitempty"`
	Severity   string            `json:"severity,omitempty"`
	Message    string            `json:"message,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}

// ListAlertRules handles GET /api/v1/alerting/rules
func (h *Handler) ListAlertRules(w http.ResponseWriter, r *http.Request) {
	if h.alertEngine == nil {
		respondError(w, http.StatusServiceUnavailable, "Alerting not configured", "ALERTING_DISABLED")
		return
	}

	rules := h.alertEngine.ListRules()
	response := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		response = append(response, alertRuleToResponse(rule))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"rules": response,
		"total": len(response),
	})
}

// GetAlertRule handles GET /api/v1/alerting/rules/{ruleId}
func (h *Handler) GetAlertRule(w http.ResponseWriter, r *http.Request) {
	if h.alertEngine == nil {
		respondError(w, http.StatusServiceUnavailable, "Alerting not configured", "ALERTING_DISABLED")
		return
	}

	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid rule ID", "BAD_REQUEST")
		return
	}

	rule, err := h.alertEngine.GetRule(ruleID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Alert rule not found", "NOT_FOUND")
		return
	}

	respondJSON(w, http.StatusOK, alertRuleToResponse(rule))
}

// CreateAlertRule handles POST /api/v1/alerting/rules
func (h *Handler) CreateAlertRule(w http.ResponseWriter, r *http.Request) {
	if h.alertEngine == nil {
		respondError(w, http.StatusServiceUnavailable, "Alerting not configured", "ALERTING_DISABLED")
		return
	}

	var req AlertRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "Name is required", "VALIDATION_ERROR")
		return
	}
	if req.Condition == nil {
		respondError(w, http.StatusBadRequest, "Condition is required", "VALIDATION_ERROR")
		return
	}
	if req.Action == nil {
		respondError(w, http.StatusBadRequest, "Action is required", "VALIDATION_ERROR")
		return
	}

	condition := alerting.Condition{
		Metric:   parseConditionType(req.Condition.Metric),
		Operator: parseOperator(req.Condition.Operator),
		Value:    req.Condition.Value,
		Window:   parseDuration(req.Condition.Window, 5*time.Minute),
	}

	action := alerting.Action{
		Type: parseActionType(req.Action.Type),
		Config: alerting.ActionConfig{
			WebhookURL: req.Action.WebhookURL,
			URL:        req.Action.WebhookURL,
			Channel:    req.Action.Channel,
			To:         req.Action.To,
			Subject:    req.Action.Subject,
			RoutingKey: req.Action.RoutingKey,
			Severity:   req.Action.Severity,
			Headers:    req.Action.Headers,
		},
		Message: req.Action.Message,
	}

	rule := alerting.NewRule(req.Name, condition, action)
	rule.Description = req.Description

	if req.Cooldown != "" {
		rule.Cooldown = parseDuration(req.Cooldown, 15*time.Minute)
	}

	if err := h.alertEngine.AddRule(rule); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	respondJSON(w, http.StatusCreated, alertRuleToResponse(rule))
}

// UpdateAlertRule handles PUT /api/v1/alerting/rules/{ruleId}
func (h *Handler) UpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	if h.alertEngine == nil {
		respondError(w, http.StatusServiceUnavailable, "Alerting not configured", "ALERTING_DISABLED")
		return
	}

	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid rule ID", "BAD_REQUEST")
		return
	}

	rule, err := h.alertEngine.GetRule(ruleID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Alert rule not found", "NOT_FOUND")
		return
	}

	var req AlertRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	if req.Name != "" {
		rule.Name = req.Name
	}
	if req.Description != "" {
		rule.Description = req.Description
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if req.Condition != nil {
		rule.Condition = alerting.Condition{
			Metric:   parseConditionType(req.Condition.Metric),
			Operator: parseOperator(req.Condition.Operator),
			Value:    req.Condition.Value,
			Window:   parseDuration(req.Condition.Window, 5*time.Minute),
		}
	}
	if req.Action != nil {
		rule.Action = alerting.Action{
			Type: parseActionType(req.Action.Type),
			Config: alerting.ActionConfig{
				WebhookURL: req.Action.WebhookURL,
				URL:        req.Action.WebhookURL,
				Channel:    req.Action.Channel,
				To:         req.Action.To,
				Subject:    req.Action.Subject,
				RoutingKey: req.Action.RoutingKey,
				Severity:   req.Action.Severity,
				Headers:    req.Action.Headers,
			},
			Message: req.Action.Message,
		}
	}
	if req.Cooldown != "" {
		rule.Cooldown = parseDuration(req.Cooldown, 15*time.Minute)
	}

	if err := h.alertEngine.UpdateRule(rule); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	respondJSON(w, http.StatusOK, alertRuleToResponse(rule))
}

// DeleteAlertRule handles DELETE /api/v1/alerting/rules/{ruleId}
func (h *Handler) DeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	if h.alertEngine == nil {
		respondError(w, http.StatusServiceUnavailable, "Alerting not configured", "ALERTING_DISABLED")
		return
	}

	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid rule ID", "BAD_REQUEST")
		return
	}

	if err := h.alertEngine.RemoveRule(ruleID); err != nil {
		respondError(w, http.StatusNotFound, "Alert rule not found", "NOT_FOUND")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// EnableAlertRule handles POST /api/v1/alerting/rules/{ruleId}/enable
func (h *Handler) EnableAlertRule(w http.ResponseWriter, r *http.Request) {
	if h.alertEngine == nil {
		respondError(w, http.StatusServiceUnavailable, "Alerting not configured", "ALERTING_DISABLED")
		return
	}

	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid rule ID", "BAD_REQUEST")
		return
	}

	if err := h.alertEngine.EnableRule(ruleID); err != nil {
		respondError(w, http.StatusNotFound, "Alert rule not found", "NOT_FOUND")
		return
	}

	rule, _ := h.alertEngine.GetRule(ruleID)
	respondJSON(w, http.StatusOK, alertRuleToResponse(rule))
}

// DisableAlertRule handles POST /api/v1/alerting/rules/{ruleId}/disable
func (h *Handler) DisableAlertRule(w http.ResponseWriter, r *http.Request) {
	if h.alertEngine == nil {
		respondError(w, http.StatusServiceUnavailable, "Alerting not configured", "ALERTING_DISABLED")
		return
	}

	ruleID, err := uuid.Parse(chi.URLParam(r, "ruleId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid rule ID", "BAD_REQUEST")
		return
	}

	if err := h.alertEngine.DisableRule(ruleID); err != nil {
		respondError(w, http.StatusNotFound, "Alert rule not found", "NOT_FOUND")
		return
	}

	rule, _ := h.alertEngine.GetRule(ruleID)
	respondJSON(w, http.StatusOK, alertRuleToResponse(rule))
}

// EvaluateAlertRules handles POST /api/v1/alerting/evaluate
func (h *Handler) EvaluateAlertRules(w http.ResponseWriter, r *http.Request) {
	if h.alertEngine == nil {
		respondError(w, http.StatusServiceUnavailable, "Alerting not configured", "ALERTING_DISABLED")
		return
	}

	h.alertEngine.EvaluateNow(r.Context())
	respondJSON(w, http.StatusOK, map[string]any{"evaluated": true})
}

// GetAlertHistory handles GET /api/v1/alerting/history
func (h *Handler) GetAlertHistory(w http.ResponseWriter, r *http.Request) {
	if h.alertEngine == nil {
		respondError(w, http.StatusServiceUnavailable, "Alerting not configured", "ALERTING_DISABLED")
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := parseInt(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	alerts := h.alertEngine.GetAlertHistory(limit)
	response := make([]map[string]any, 0, len(alerts))
	for _, alert := range alerts {
		response = append(response, map[string]any{
			"id":        alert.ID.String(),
			"ruleId":    alert.RuleID.String(),
			"ruleName":  alert.RuleName,
			"metric":    alert.Metric,
			"value":     alert.Value,
			"threshold": alert.Threshold,
			"message":   alert.Message,
			"firedAt":   alert.FiredAt,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"alerts": response,
		"total":  len(response),
	})
}

// Helper functions

func alertRuleToResponse(rule *alerting.Rule) map[string]any {
	resp := map[string]any{
		"id":      rule.ID.String(),
		"name":    rule.Name,
		"enabled": rule.Enabled,
		"condition": map[string]any{
			"metric":   string(rule.Condition.Metric),
			"operator": string(rule.Condition.Operator),
			"value":    rule.Condition.Value,
			"window":   rule.Condition.Window.Duration().String(),
		},
		"action": map[string]any{
			"type":       string(rule.Action.Type),
			"webhookUrl": rule.Action.Config.WebhookURL,
			"channel":    rule.Action.Config.Channel,
			"to":         rule.Action.Config.To,
			"subject":    rule.Action.Config.Subject,
			"routingKey": rule.Action.Config.RoutingKey,
			"severity":   rule.Action.Config.Severity,
			"message":    rule.Action.Message,
		},
		"cooldown":  rule.Cooldown.Duration().String(),
		"createdAt": rule.CreatedAt,
		"updatedAt": rule.UpdatedAt,
	}

	if rule.Description != "" {
		resp["description"] = rule.Description
	}

	if !rule.LastFiredAt().IsZero() {
		resp["lastFiredAt"] = rule.LastFiredAt()
	}

	return resp
}

func parseConditionType(s string) alerting.ConditionType {
	switch s {
	case "failure_rate", "FAILURE_RATE":
		return alerting.ConditionTypeFailureRate
	case "success_rate", "SUCCESS_RATE":
		return alerting.ConditionTypeSuccessRate
	case "latency", "LATENCY":
		return alerting.ConditionTypeLatency
	case "queue_depth", "QUEUE_DEPTH":
		return alerting.ConditionTypeQueueDepth
	case "error_count", "ERROR_COUNT":
		return alerting.ConditionTypeErrorCount
	case "delivery_count", "DELIVERY_COUNT":
		return alerting.ConditionTypeDeliveryCount
	default:
		return alerting.ConditionTypeFailureRate
	}
}

func parseOperator(s string) alerting.Operator {
	switch s {
	case "gt", "GT", ">":
		return alerting.OperatorGreaterThan
	case "gte", "GTE", ">=":
		return alerting.OperatorGreaterThanOrEqual
	case "lt", "LT", "<":
		return alerting.OperatorLessThan
	case "lte", "LTE", "<=":
		return alerting.OperatorLessThanOrEqual
	case "eq", "EQ", "==":
		return alerting.OperatorEqual
	case "ne", "NE", "!=":
		return alerting.OperatorNotEqual
	default:
		return alerting.OperatorGreaterThan
	}
}

func parseActionType(s string) alerting.ActionType {
	switch s {
	case "slack", "SLACK":
		return alerting.ActionTypeSlack
	case "email", "EMAIL":
		return alerting.ActionTypeEmail
	case "webhook", "WEBHOOK":
		return alerting.ActionTypeWebhook
	case "pagerduty", "PAGERDUTY":
		return alerting.ActionTypePagerDuty
	default:
		return alerting.ActionTypeSlack
	}
}

func parseDuration(s string, def time.Duration) alerting.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return alerting.Duration(def)
	}
	return alerting.Duration(d)
}

func parseInt(s string) (int, error) {
	i, err := json.Number(s).Int64()
	return int(i), err
}
