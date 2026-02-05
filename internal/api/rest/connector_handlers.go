package rest

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/stiffinWanjohi/relay/internal/auth"
	"github.com/stiffinWanjohi/relay/internal/connector"
)

// ConnectorRequest represents the request body for creating/updating a connector.
type ConnectorRequest struct {
	Name     string                    `json:"name,omitempty"`
	Type     string                    `json:"type"`
	Config   *ConnectorConfigRequest   `json:"config"`
	Template *ConnectorTemplateRequest `json:"template,omitempty"`
}

// ConnectorConfigRequest represents the connector configuration.
type ConnectorConfigRequest struct {
	WebhookURL   string   `json:"webhookUrl,omitempty"`
	Channel      string   `json:"channel,omitempty"`
	Username     string   `json:"username,omitempty"`
	IconEmoji    string   `json:"iconEmoji,omitempty"`
	IconURL      string   `json:"iconUrl,omitempty"`
	SMTPHost     string   `json:"smtpHost,omitempty"`
	SMTPPort     int      `json:"smtpPort,omitempty"`
	SMTPUsername string   `json:"smtpUsername,omitempty"`
	SMTPPassword string   `json:"smtpPassword,omitempty"`
	FromEmail    string   `json:"fromEmail,omitempty"`
	ToEmails     []string `json:"toEmails,omitempty"`
}

// ConnectorTemplateRequest represents the connector template.
type ConnectorTemplateRequest struct {
	Text    string `json:"text,omitempty"`
	Title   string `json:"title,omitempty"`
	Body    string `json:"body,omitempty"`
	Color   string `json:"color,omitempty"`
	Subject string `json:"subject,omitempty"`
}

// ListConnectors handles GET /api/v1/connectors
func (h *Handler) ListConnectors(w http.ResponseWriter, r *http.Request) {
	if h.connectorRegistry == nil {
		respondError(w, http.StatusServiceUnavailable, "Connectors not configured", "CONNECTORS_DISABLED")
		return
	}

	clientID, err := auth.RequireClientID(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED")
		return
	}

	// If we have a store, list from database
	if h.connectorStore != nil {
		connectors, err := h.connectorStore.List(r.Context(), clientID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to list connectors", "INTERNAL_ERROR")
			return
		}

		response := make([]map[string]any, 0, len(connectors))
		for _, sc := range connectors {
			response = append(response, storedConnectorToResponse(sc))
		}

		respondJSON(w, http.StatusOK, map[string]any{
			"connectors": response,
			"total":      len(response),
		})
		return
	}

	// Fall back to in-memory registry
	names := h.connectorRegistry.List()
	response := make([]map[string]any, 0, len(names))
	for _, name := range names {
		c, ok := h.connectorRegistry.Get(name)
		if ok {
			response = append(response, connectorToResponse(name, c))
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"connectors": response,
		"total":      len(response),
	})
}

// GetConnector handles GET /api/v1/connectors/{name}
func (h *Handler) GetConnector(w http.ResponseWriter, r *http.Request) {
	if h.connectorRegistry == nil {
		respondError(w, http.StatusServiceUnavailable, "Connectors not configured", "CONNECTORS_DISABLED")
		return
	}

	clientID, err := auth.RequireClientID(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED")
		return
	}

	name := chi.URLParam(r, "name")

	// If we have a store, get from database
	if h.connectorStore != nil {
		sc, err := h.connectorStore.GetByName(r.Context(), clientID, name)
		if err != nil {
			if err == connector.ErrConnectorNotFound {
				respondError(w, http.StatusNotFound, "Connector not found", "NOT_FOUND")
				return
			}
			respondError(w, http.StatusInternalServerError, "Failed to get connector", "INTERNAL_ERROR")
			return
		}
		respondJSON(w, http.StatusOK, storedConnectorToResponse(sc))
		return
	}

	// Fall back to in-memory registry
	c, ok := h.connectorRegistry.Get(name)
	if !ok {
		respondError(w, http.StatusNotFound, "Connector not found", "NOT_FOUND")
		return
	}

	respondJSON(w, http.StatusOK, connectorToResponse(name, c))
}

// CreateConnector handles POST /api/v1/connectors
func (h *Handler) CreateConnector(w http.ResponseWriter, r *http.Request) {
	if h.connectorRegistry == nil {
		respondError(w, http.StatusServiceUnavailable, "Connectors not configured", "CONNECTORS_DISABLED")
		return
	}

	clientID, err := auth.RequireClientID(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED")
		return
	}

	var req ConnectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "Name is required", "VALIDATION_ERROR")
		return
	}
	if req.Type == "" {
		respondError(w, http.StatusBadRequest, "Type is required", "VALIDATION_ERROR")
		return
	}
	if req.Config == nil {
		respondError(w, http.StatusBadRequest, "Config is required", "VALIDATION_ERROR")
		return
	}

	c := &connector.Connector{
		Type:     parseConnectorType(req.Type),
		Config:   configRequestToConnector(req.Config),
		Template: templateRequestToConnector(req.Template),
	}

	// Validate connector
	if err := c.Validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	// If we have a store, persist to database
	if h.connectorStore != nil {
		sc, err := h.connectorStore.Create(r.Context(), clientID, req.Name, c)
		if err != nil {
			if err == connector.ErrDuplicateConnector {
				respondError(w, http.StatusConflict, "Connector with this name already exists", "DUPLICATE")
				return
			}
			respondError(w, http.StatusInternalServerError, "Failed to create connector", "INTERNAL_ERROR")
			return
		}

		// Also register in memory for immediate use
		_ = h.connectorRegistry.Register(req.Name, c)

		respondJSON(w, http.StatusCreated, storedConnectorToResponse(sc))
		return
	}

	// Fall back to in-memory only
	if err := h.connectorRegistry.Register(req.Name, c); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	respondJSON(w, http.StatusCreated, connectorToResponse(req.Name, c))
}

// UpdateConnector handles PUT /api/v1/connectors/{name}
func (h *Handler) UpdateConnector(w http.ResponseWriter, r *http.Request) {
	if h.connectorRegistry == nil {
		respondError(w, http.StatusServiceUnavailable, "Connectors not configured", "CONNECTORS_DISABLED")
		return
	}

	clientID, err := auth.RequireClientID(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED")
		return
	}

	name := chi.URLParam(r, "name")

	var req ConnectorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	// If we have a store, update in database
	if h.connectorStore != nil {
		sc, err := h.connectorStore.GetByName(r.Context(), clientID, name)
		if err != nil {
			if err == connector.ErrConnectorNotFound {
				respondError(w, http.StatusNotFound, "Connector not found", "NOT_FOUND")
				return
			}
			respondError(w, http.StatusInternalServerError, "Failed to get connector", "INTERNAL_ERROR")
			return
		}

		// Apply updates
		c := sc.ToConnector()
		if req.Config != nil {
			c.Config = configRequestToConnector(req.Config)
		}
		if req.Template != nil {
			c.Template = templateRequestToConnector(req.Template)
		}

		// Validate
		if err := c.Validate(); err != nil {
			respondError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
			return
		}

		updated, err := h.connectorStore.Update(r.Context(), sc.ID, c, sc.Enabled)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to update connector", "INTERNAL_ERROR")
			return
		}

		// Update in-memory registry
		h.connectorRegistry.Delete(name)
		_ = h.connectorRegistry.Register(name, c)

		respondJSON(w, http.StatusOK, storedConnectorToResponse(updated))
		return
	}

	// Fall back to in-memory only
	existing, ok := h.connectorRegistry.Get(name)
	if !ok {
		respondError(w, http.StatusNotFound, "Connector not found", "NOT_FOUND")
		return
	}

	if req.Config != nil {
		existing.Config = configRequestToConnector(req.Config)
	}
	if req.Template != nil {
		existing.Template = templateRequestToConnector(req.Template)
	}

	// Re-register to validate
	h.connectorRegistry.Delete(name)
	if err := h.connectorRegistry.Register(name, existing); err != nil {
		respondError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	respondJSON(w, http.StatusOK, connectorToResponse(name, existing))
}

// DeleteConnector handles DELETE /api/v1/connectors/{name}
func (h *Handler) DeleteConnector(w http.ResponseWriter, r *http.Request) {
	if h.connectorRegistry == nil {
		respondError(w, http.StatusServiceUnavailable, "Connectors not configured", "CONNECTORS_DISABLED")
		return
	}

	clientID, err := auth.RequireClientID(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED")
		return
	}

	name := chi.URLParam(r, "name")

	// If we have a store, delete from database
	if h.connectorStore != nil {
		if err := h.connectorStore.DeleteByName(r.Context(), clientID, name); err != nil {
			if err == connector.ErrConnectorNotFound {
				respondError(w, http.StatusNotFound, "Connector not found", "NOT_FOUND")
				return
			}
			respondError(w, http.StatusInternalServerError, "Failed to delete connector", "INTERNAL_ERROR")
			return
		}

		// Also delete from in-memory registry
		h.connectorRegistry.Delete(name)

		respondJSON(w, http.StatusOK, map[string]any{"deleted": true})
		return
	}

	// Fall back to in-memory only
	_, ok := h.connectorRegistry.Get(name)
	if !ok {
		respondError(w, http.StatusNotFound, "Connector not found", "NOT_FOUND")
		return
	}

	h.connectorRegistry.Delete(name)
	respondJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// Helper functions

func connectorToResponse(name string, c *connector.Connector) map[string]any {
	return map[string]any{
		"name": name,
		"type": string(c.Type),
		"config": map[string]any{
			"webhookUrl": c.Config.WebhookURL,
			"channel":    c.Config.Channel,
			"username":   c.Config.Username,
			"iconEmoji":  c.Config.IconEmoji,
			"iconUrl":    c.Config.IconURL,
			"smtpHost":   c.Config.SMTPHost,
			"smtpPort":   c.Config.SMTPPort,
			"fromEmail":  c.Config.FromEmail,
			"toEmails":   c.Config.ToEmails,
		},
		"template": map[string]any{
			"text":    c.Template.Text,
			"title":   c.Template.Title,
			"body":    c.Template.Body,
			"color":   c.Template.Color,
			"subject": c.Template.Subject,
		},
	}
}

func storedConnectorToResponse(sc *connector.StoredConnector) map[string]any {
	return map[string]any{
		"id":      sc.ID.String(),
		"name":    sc.Name,
		"type":    string(sc.Type),
		"enabled": sc.Enabled,
		"config": map[string]any{
			"webhookUrl": sc.Config.WebhookURL,
			"channel":    sc.Config.Channel,
			"username":   sc.Config.Username,
			"iconEmoji":  sc.Config.IconEmoji,
			"iconUrl":    sc.Config.IconURL,
			"smtpHost":   sc.Config.SMTPHost,
			"smtpPort":   sc.Config.SMTPPort,
			"fromEmail":  sc.Config.FromEmail,
			"toEmails":   sc.Config.ToEmails,
		},
		"template": map[string]any{
			"text":    sc.Template.Text,
			"title":   sc.Template.Title,
			"body":    sc.Template.Body,
			"color":   sc.Template.Color,
			"subject": sc.Template.Subject,
		},
		"createdAt": sc.CreatedAt,
		"updatedAt": sc.UpdatedAt,
	}
}

func parseConnectorType(s string) connector.ConnectorType {
	switch s {
	case "slack", "SLACK":
		return connector.ConnectorTypeSlack
	case "discord", "DISCORD":
		return connector.ConnectorTypeDiscord
	case "teams", "TEAMS":
		return connector.ConnectorTypeTeams
	case "email", "EMAIL":
		return connector.ConnectorTypeEmail
	case "webhook", "WEBHOOK":
		return connector.ConnectorTypeWebhook
	default:
		return connector.ConnectorTypeWebhook
	}
}

func configRequestToConnector(req *ConnectorConfigRequest) connector.Config {
	if req == nil {
		return connector.Config{}
	}
	return connector.Config{
		WebhookURL:   req.WebhookURL,
		Channel:      req.Channel,
		Username:     req.Username,
		IconEmoji:    req.IconEmoji,
		IconURL:      req.IconURL,
		SMTPHost:     req.SMTPHost,
		SMTPPort:     req.SMTPPort,
		SMTPUsername: req.SMTPUsername,
		SMTPPassword: req.SMTPPassword,
		FromEmail:    req.FromEmail,
		ToEmails:     req.ToEmails,
	}
}

func templateRequestToConnector(req *ConnectorTemplateRequest) connector.Template {
	if req == nil {
		return connector.Template{}
	}
	return connector.Template{
		Text:    req.Text,
		Title:   req.Title,
		Body:    req.Body,
		Color:   req.Color,
		Subject: req.Subject,
	}
}
