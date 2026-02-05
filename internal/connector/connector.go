package connector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"text/template"

	"github.com/stiffinWanjohi/relay/internal/logging"
)

var log = logging.Component("connector")

// ConnectorType represents the type of connector.
type ConnectorType string

const (
	ConnectorTypeSlack   ConnectorType = "slack"
	ConnectorTypeDiscord ConnectorType = "discord"
	ConnectorTypeTeams   ConnectorType = "teams"
	ConnectorTypeEmail   ConnectorType = "email"
	ConnectorTypeWebhook ConnectorType = "webhook"
)

// Errors
var (
	ErrUnknownConnectorType = errors.New("unknown connector type")
	ErrInvalidConfig        = errors.New("invalid connector configuration")
	ErrTemplateExecution    = errors.New("template execution failed")
)

// Config represents connector-specific configuration.
type Config struct {
	// Slack
	WebhookURL string `json:"webhook_url,omitempty"`
	Channel    string `json:"channel,omitempty"`
	Username   string `json:"username,omitempty"`
	IconEmoji  string `json:"icon_emoji,omitempty"`
	IconURL    string `json:"icon_url,omitempty"`

	// Discord
	// Uses WebhookURL

	// Teams
	// Uses WebhookURL

	// Email
	SMTPHost     string   `json:"smtp_host,omitempty"`
	SMTPPort     int      `json:"smtp_port,omitempty"`
	SMTPUsername string   `json:"smtp_username,omitempty"`
	SMTPPassword string   `json:"smtp_password,omitempty"`
	FromEmail    string   `json:"from_email,omitempty"`
	ToEmails     []string `json:"to_emails,omitempty"`
	Subject      string   `json:"subject,omitempty"`
}

// Template represents a message template for a connector.
type Template struct {
	Text    string `json:"text,omitempty"`
	Title   string `json:"title,omitempty"`
	Body    string `json:"body,omitempty"`
	Color   string `json:"color,omitempty"`
	Subject string `json:"subject,omitempty"` // For email
}

// Connector represents a pre-built integration.
type Connector struct {
	Type     ConnectorType `json:"type"`
	Config   Config        `json:"config"`
	Template Template      `json:"template"`
}

// Validate validates the connector configuration.
func (c *Connector) Validate() error {
	switch c.Type {
	case ConnectorTypeSlack:
		if c.Config.WebhookURL == "" {
			return fmt.Errorf("%w: slack requires webhook_url", ErrInvalidConfig)
		}
	case ConnectorTypeDiscord:
		if c.Config.WebhookURL == "" {
			return fmt.Errorf("%w: discord requires webhook_url", ErrInvalidConfig)
		}
	case ConnectorTypeTeams:
		if c.Config.WebhookURL == "" {
			return fmt.Errorf("%w: teams requires webhook_url", ErrInvalidConfig)
		}
	case ConnectorTypeEmail:
		if c.Config.SMTPHost == "" {
			return fmt.Errorf("%w: email requires smtp_host", ErrInvalidConfig)
		}
		if c.Config.FromEmail == "" {
			return fmt.Errorf("%w: email requires from_email", ErrInvalidConfig)
		}
		if len(c.Config.ToEmails) == 0 {
			return fmt.Errorf("%w: email requires at least one to_email", ErrInvalidConfig)
		}
	case ConnectorTypeWebhook:
		if c.Config.WebhookURL == "" {
			return fmt.Errorf("%w: webhook requires webhook_url", ErrInvalidConfig)
		}
	default:
		return fmt.Errorf("%w: %s", ErrUnknownConnectorType, c.Type)
	}
	return nil
}

// GetURL returns the destination URL for the connector.
func (c *Connector) GetURL() string {
	return c.Config.WebhookURL
}

// Transform transforms the event payload for the connector type.
func (c *Connector) Transform(eventType string, payload map[string]any) (map[string]any, error) {
	// Add event metadata to template context
	ctx := map[string]any{
		"event_type": eventType,
		"payload":    payload,
	}

	// Merge payload fields into top level for easier template access
	for k, v := range payload {
		ctx[k] = v
	}

	switch c.Type {
	case ConnectorTypeSlack:
		return c.transformSlack(ctx)
	case ConnectorTypeDiscord:
		return c.transformDiscord(ctx)
	case ConnectorTypeTeams:
		return c.transformTeams(ctx)
	case ConnectorTypeEmail:
		return c.transformEmail(ctx)
	case ConnectorTypeWebhook:
		return payload, nil // Pass through as-is
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownConnectorType, c.Type)
	}
}

func (c *Connector) transformSlack(ctx map[string]any) (map[string]any, error) {
	text, err := c.executeTemplate(c.Template.Text, ctx)
	if err != nil {
		return nil, err
	}

	msg := map[string]any{
		"text": text,
	}

	if c.Config.Channel != "" {
		msg["channel"] = c.Config.Channel
	}
	if c.Config.Username != "" {
		msg["username"] = c.Config.Username
	}
	if c.Config.IconEmoji != "" {
		msg["icon_emoji"] = c.Config.IconEmoji
	}
	if c.Config.IconURL != "" {
		msg["icon_url"] = c.Config.IconURL
	}

	// Add attachment if title or color is set
	if c.Template.Title != "" || c.Template.Color != "" {
		title, _ := c.executeTemplate(c.Template.Title, ctx)
		body, _ := c.executeTemplate(c.Template.Body, ctx)

		attachment := map[string]any{}
		if title != "" {
			attachment["title"] = title
		}
		if body != "" {
			attachment["text"] = body
		}
		if c.Template.Color != "" {
			attachment["color"] = c.Template.Color
		}
		msg["attachments"] = []map[string]any{attachment}
	}

	return msg, nil
}

func (c *Connector) transformDiscord(ctx map[string]any) (map[string]any, error) {
	text, err := c.executeTemplate(c.Template.Text, ctx)
	if err != nil {
		return nil, err
	}

	msg := map[string]any{
		"content": text,
	}

	if c.Config.Username != "" {
		msg["username"] = c.Config.Username
	}

	// Add embed if title or color is set
	if c.Template.Title != "" || c.Template.Color != "" {
		title, _ := c.executeTemplate(c.Template.Title, ctx)
		body, _ := c.executeTemplate(c.Template.Body, ctx)

		embed := map[string]any{}
		if title != "" {
			embed["title"] = title
		}
		if body != "" {
			embed["description"] = body
		}
		if c.Template.Color != "" {
			// Discord expects color as integer
			embed["color"] = parseColorToInt(c.Template.Color)
		}
		msg["embeds"] = []map[string]any{embed}
	}

	return msg, nil
}

func (c *Connector) transformTeams(ctx map[string]any) (map[string]any, error) {
	text, err := c.executeTemplate(c.Template.Text, ctx)
	if err != nil {
		return nil, err
	}

	title, _ := c.executeTemplate(c.Template.Title, ctx)

	// Microsoft Teams message card format
	msg := map[string]any{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"themeColor": c.Template.Color,
		"summary":    title,
		"sections": []map[string]any{
			{
				"activityTitle": title,
				"text":          text,
				"markdown":      true,
			},
		},
	}

	return msg, nil
}

func (c *Connector) transformEmail(ctx map[string]any) (map[string]any, error) {
	subject, err := c.executeTemplate(c.Template.Subject, ctx)
	if err != nil {
		subject = "Webhook Event"
	}

	body, err := c.executeTemplate(c.Template.Body, ctx)
	if err != nil {
		// Fallback to JSON encoding the payload
		payloadBytes, _ := json.MarshalIndent(ctx["payload"], "", "  ")
		body = string(payloadBytes)
	}

	// Return email-specific payload that the email sender will interpret
	return map[string]any{
		"_connector_type": "email",
		"smtp_host":       c.Config.SMTPHost,
		"smtp_port":       c.Config.SMTPPort,
		"smtp_username":   c.Config.SMTPUsername,
		"smtp_password":   c.Config.SMTPPassword,
		"from":            c.Config.FromEmail,
		"to":              c.Config.ToEmails,
		"subject":         subject,
		"body":            body,
	}, nil
}

func (c *Connector) executeTemplate(tmplStr string, ctx map[string]any) (string, error) {
	if tmplStr == "" {
		return "", nil
	}

	tmpl, err := template.New("msg").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTemplateExecution, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("%w: %v", ErrTemplateExecution, err)
	}

	return buf.String(), nil
}

// parseColorToInt converts hex color to integer for Discord
func parseColorToInt(color string) int {
	// Common color names
	colors := map[string]int{
		"red":     0xFF0000,
		"green":   0x00FF00,
		"blue":    0x0000FF,
		"yellow":  0xFFFF00,
		"orange":  0xFFA500,
		"purple":  0x800080,
		"danger":  0xFF0000,
		"warning": 0xFFA500,
		"success": 0x00FF00,
		"info":    0x0000FF,
		"good":    0x00FF00,
	}

	if val, ok := colors[color]; ok {
		return val
	}

	// Try parsing as hex
	var result int
	if len(color) > 0 && color[0] == '#' {
		color = color[1:]
	}
	fmt.Sscanf(color, "%x", &result)
	return result
}
