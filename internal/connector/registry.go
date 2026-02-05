package connector

import (
	"encoding/json"
	"fmt"
)

// Registry manages connector definitions.
type Registry struct {
	connectors map[string]*Connector
}

// NewRegistry creates a new connector registry.
func NewRegistry() *Registry {
	return &Registry{
		connectors: make(map[string]*Connector),
	}
}

// Register adds a connector to the registry.
func (r *Registry) Register(name string, connector *Connector) error {
	if err := connector.Validate(); err != nil {
		return err
	}
	r.connectors[name] = connector
	log.Info("connector registered", "name", name, "type", connector.Type)
	return nil
}

// Get retrieves a connector by name.
func (r *Registry) Get(name string) (*Connector, bool) {
	c, ok := r.connectors[name]
	return c, ok
}

// List returns all registered connector names.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.connectors))
	for name := range r.connectors {
		names = append(names, name)
	}
	return names
}

// Delete removes a connector from the registry.
func (r *Registry) Delete(name string) {
	delete(r.connectors, name)
}

// ParseConnector parses a connector from JSON configuration.
func ParseConnector(connectorJSON []byte) (*Connector, error) {
	var c Connector
	if err := json.Unmarshal(connectorJSON, &c); err != nil {
		return nil, fmt.Errorf("failed to parse connector: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// DefaultTemplates provides default message templates for each connector type.
var DefaultTemplates = map[ConnectorType]Template{
	ConnectorTypeSlack: {
		Text:  "{{.event_type}}: {{if .message}}{{.message}}{{else}}New event received{{end}}",
		Title: "{{.event_type}}",
		Color: "#36a64f",
	},
	ConnectorTypeDiscord: {
		Text:  "{{.event_type}}: {{if .message}}{{.message}}{{else}}New event received{{end}}",
		Title: "{{.event_type}}",
		Color: "info",
	},
	ConnectorTypeTeams: {
		Text:  "{{if .message}}{{.message}}{{else}}New event received{{end}}",
		Title: "{{.event_type}}",
		Color: "0076D7",
	},
	ConnectorTypeEmail: {
		Subject: "[Relay] {{.event_type}}",
		Body:    "Event Type: {{.event_type}}\n\nPayload:\n{{range $k, $v := .payload}}{{$k}}: {{$v}}\n{{end}}",
	},
}

// NewSlackConnector creates a new Slack connector with sensible defaults.
func NewSlackConnector(webhookURL string, opts ...ConnectorOption) *Connector {
	c := &Connector{
		Type: ConnectorTypeSlack,
		Config: Config{
			WebhookURL: webhookURL,
		},
		Template: DefaultTemplates[ConnectorTypeSlack],
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// NewDiscordConnector creates a new Discord connector with sensible defaults.
func NewDiscordConnector(webhookURL string, opts ...ConnectorOption) *Connector {
	c := &Connector{
		Type: ConnectorTypeDiscord,
		Config: Config{
			WebhookURL: webhookURL,
		},
		Template: DefaultTemplates[ConnectorTypeDiscord],
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// NewTeamsConnector creates a new Microsoft Teams connector with sensible defaults.
func NewTeamsConnector(webhookURL string, opts ...ConnectorOption) *Connector {
	c := &Connector{
		Type: ConnectorTypeTeams,
		Config: Config{
			WebhookURL: webhookURL,
		},
		Template: DefaultTemplates[ConnectorTypeTeams],
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// NewEmailConnector creates a new email connector.
func NewEmailConnector(smtpHost string, smtpPort int, from string, to []string, opts ...ConnectorOption) *Connector {
	c := &Connector{
		Type: ConnectorTypeEmail,
		Config: Config{
			SMTPHost:  smtpHost,
			SMTPPort:  smtpPort,
			FromEmail: from,
			ToEmails:  to,
		},
		Template: DefaultTemplates[ConnectorTypeEmail],
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ConnectorOption is a functional option for configuring connectors.
type ConnectorOption func(*Connector)

// WithChannel sets the channel for Slack connectors.
func WithChannel(channel string) ConnectorOption {
	return func(c *Connector) {
		c.Config.Channel = channel
	}
}

// WithUsername sets the username for Slack/Discord connectors.
func WithUsername(username string) ConnectorOption {
	return func(c *Connector) {
		c.Config.Username = username
	}
}

// WithIconEmoji sets the icon emoji for Slack connectors.
func WithIconEmoji(emoji string) ConnectorOption {
	return func(c *Connector) {
		c.Config.IconEmoji = emoji
	}
}

// WithTemplate sets a custom template.
func WithTemplate(tmpl Template) ConnectorOption {
	return func(c *Connector) {
		if tmpl.Text != "" {
			c.Template.Text = tmpl.Text
		}
		if tmpl.Title != "" {
			c.Template.Title = tmpl.Title
		}
		if tmpl.Body != "" {
			c.Template.Body = tmpl.Body
		}
		if tmpl.Color != "" {
			c.Template.Color = tmpl.Color
		}
		if tmpl.Subject != "" {
			c.Template.Subject = tmpl.Subject
		}
	}
}

// WithSMTPAuth sets SMTP authentication credentials.
func WithSMTPAuth(username, password string) ConnectorOption {
	return func(c *Connector) {
		c.Config.SMTPUsername = username
		c.Config.SMTPPassword = password
	}
}
