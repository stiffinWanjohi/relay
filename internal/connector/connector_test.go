package connector

import (
	"encoding/json"
	"testing"
)

func TestConnector_Validate_Slack(t *testing.T) {
	tests := []struct {
		name    string
		conn    *Connector
		wantErr bool
	}{
		{
			name: "valid slack connector",
			conn: &Connector{
				Type:   ConnectorTypeSlack,
				Config: Config{WebhookURL: "https://hooks.slack.com/services/xxx"},
			},
			wantErr: false,
		},
		{
			name: "slack without webhook URL",
			conn: &Connector{
				Type:   ConnectorTypeSlack,
				Config: Config{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.conn.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConnector_Validate_Discord(t *testing.T) {
	tests := []struct {
		name    string
		conn    *Connector
		wantErr bool
	}{
		{
			name: "valid discord connector",
			conn: &Connector{
				Type:   ConnectorTypeDiscord,
				Config: Config{WebhookURL: "https://discord.com/api/webhooks/xxx"},
			},
			wantErr: false,
		},
		{
			name: "discord without webhook URL",
			conn: &Connector{
				Type:   ConnectorTypeDiscord,
				Config: Config{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.conn.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConnector_Validate_Teams(t *testing.T) {
	conn := &Connector{
		Type:   ConnectorTypeTeams,
		Config: Config{WebhookURL: "https://outlook.office.com/webhook/xxx"},
	}
	if err := conn.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestConnector_Validate_Email(t *testing.T) {
	tests := []struct {
		name    string
		conn    *Connector
		wantErr bool
	}{
		{
			name: "valid email connector",
			conn: &Connector{
				Type: ConnectorTypeEmail,
				Config: Config{
					SMTPHost:  "smtp.example.com",
					SMTPPort:  587,
					FromEmail: "noreply@example.com",
					ToEmails:  []string{"user@example.com"},
				},
			},
			wantErr: false,
		},
		{
			name: "email without smtp host",
			conn: &Connector{
				Type: ConnectorTypeEmail,
				Config: Config{
					FromEmail: "noreply@example.com",
					ToEmails:  []string{"user@example.com"},
				},
			},
			wantErr: true,
		},
		{
			name: "email without from",
			conn: &Connector{
				Type: ConnectorTypeEmail,
				Config: Config{
					SMTPHost: "smtp.example.com",
					ToEmails: []string{"user@example.com"},
				},
			},
			wantErr: true,
		},
		{
			name: "email without to",
			conn: &Connector{
				Type: ConnectorTypeEmail,
				Config: Config{
					SMTPHost:  "smtp.example.com",
					FromEmail: "noreply@example.com",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.conn.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConnector_Validate_UnknownType(t *testing.T) {
	conn := &Connector{
		Type: "unknown",
	}
	if err := conn.Validate(); err == nil {
		t.Error("expected error for unknown connector type")
	}
}

func TestConnector_TransformSlack(t *testing.T) {
	conn := NewSlackConnector("https://hooks.slack.com/xxx",
		WithChannel("#alerts"),
		WithUsername("Relay Bot"),
		WithTemplate(Template{
			Text:  "Event: {{.event_type}} - {{.message}}",
			Color: "danger",
		}),
	)

	payload := map[string]any{
		"message":  "Order created",
		"order_id": 123,
	}

	result, err := conn.Transform("order.created", payload)
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	if result["channel"] != "#alerts" {
		t.Errorf("expected channel #alerts, got %v", result["channel"])
	}
	if result["username"] != "Relay Bot" {
		t.Errorf("expected username Relay Bot, got %v", result["username"])
	}

	text, ok := result["text"].(string)
	if !ok || text != "Event: order.created - Order created" {
		t.Errorf("unexpected text: %v", result["text"])
	}
}

func TestConnector_TransformDiscord(t *testing.T) {
	conn := NewDiscordConnector("https://discord.com/api/webhooks/xxx",
		WithUsername("Relay"),
		WithTemplate(Template{
			Text:  "{{.event_type}}: {{.message}}",
			Title: "New Event",
			Color: "blue",
		}),
	)

	payload := map[string]any{
		"message": "Hello Discord",
	}

	result, err := conn.Transform("test.event", payload)
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	if result["content"] != "test.event: Hello Discord" {
		t.Errorf("unexpected content: %v", result["content"])
	}
	if result["username"] != "Relay" {
		t.Errorf("expected username Relay, got %v", result["username"])
	}

	embeds, ok := result["embeds"].([]map[string]any)
	if !ok || len(embeds) != 1 {
		t.Errorf("expected 1 embed, got %v", result["embeds"])
	}
}

func TestConnector_TransformTeams(t *testing.T) {
	conn := NewTeamsConnector("https://outlook.office.com/webhook/xxx",
		WithTemplate(Template{
			Text:  "{{.message}}",
			Title: "{{.event_type}}",
			Color: "0076D7",
		}),
	)

	payload := map[string]any{
		"message": "Hello Teams",
	}

	result, err := conn.Transform("test.event", payload)
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	if result["@type"] != "MessageCard" {
		t.Errorf("expected @type MessageCard, got %v", result["@type"])
	}
	if result["summary"] != "test.event" {
		t.Errorf("expected summary test.event, got %v", result["summary"])
	}
}

func TestConnector_TransformEmail(t *testing.T) {
	conn := NewEmailConnector("smtp.example.com", 587, "from@example.com", []string{"to@example.com"},
		WithTemplate(Template{
			Subject: "[Alert] {{.event_type}}",
			Body:    "Event: {{.event_type}}\nMessage: {{.message}}",
		}),
	)

	payload := map[string]any{
		"message": "Something happened",
	}

	result, err := conn.Transform("alert.triggered", payload)
	if err != nil {
		t.Fatalf("Transform() error = %v", err)
	}

	if result["_connector_type"] != "email" {
		t.Errorf("expected _connector_type email, got %v", result["_connector_type"])
	}
	if result["subject"] != "[Alert] alert.triggered" {
		t.Errorf("unexpected subject: %v", result["subject"])
	}
	if result["smtp_host"] != "smtp.example.com" {
		t.Errorf("expected smtp_host smtp.example.com, got %v", result["smtp_host"])
	}
}

func TestParseConnector(t *testing.T) {
	jsonData := `{
		"type": "slack",
		"config": {
			"webhook_url": "https://hooks.slack.com/xxx",
			"channel": "#general"
		},
		"template": {
			"text": "Hello {{.name}}"
		}
	}`

	conn, err := ParseConnector([]byte(jsonData))
	if err != nil {
		t.Fatalf("ParseConnector() error = %v", err)
	}

	if conn.Type != ConnectorTypeSlack {
		t.Errorf("expected type slack, got %s", conn.Type)
	}
	if conn.Config.WebhookURL != "https://hooks.slack.com/xxx" {
		t.Errorf("expected webhook URL, got %s", conn.Config.WebhookURL)
	}
	if conn.Config.Channel != "#general" {
		t.Errorf("expected channel #general, got %s", conn.Config.Channel)
	}
}

func TestParseConnector_Invalid(t *testing.T) {
	// Invalid JSON
	_, err := ParseConnector([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}

	// Missing required field
	_, err = ParseConnector([]byte(`{"type": "slack", "config": {}}`))
	if err == nil {
		t.Error("expected error for missing webhook URL")
	}
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()

	conn := NewSlackConnector("https://hooks.slack.com/xxx")
	if err := reg.Register("my-slack", conn); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	retrieved, ok := reg.Get("my-slack")
	if !ok {
		t.Fatal("expected connector to be found")
	}
	if retrieved.Type != ConnectorTypeSlack {
		t.Errorf("expected type slack, got %s", retrieved.Type)
	}

	names := reg.List()
	if len(names) != 1 || names[0] != "my-slack" {
		t.Errorf("expected [my-slack], got %v", names)
	}

	reg.Delete("my-slack")
	_, ok = reg.Get("my-slack")
	if ok {
		t.Error("expected connector to be deleted")
	}
}

func TestParseColorToInt(t *testing.T) {
	tests := []struct {
		color    string
		expected int
	}{
		{"red", 0xFF0000},
		{"green", 0x00FF00},
		{"blue", 0x0000FF},
		{"danger", 0xFF0000},
		{"success", 0x00FF00},
		{"#FF5733", 0xFF5733},
		{"FF5733", 0xFF5733},
	}

	for _, tt := range tests {
		t.Run(tt.color, func(t *testing.T) {
			result := parseColorToInt(tt.color)
			if result != tt.expected {
				t.Errorf("parseColorToInt(%s) = %d, expected %d", tt.color, result, tt.expected)
			}
		})
	}
}

func TestConnector_JSON_Roundtrip(t *testing.T) {
	original := NewSlackConnector("https://hooks.slack.com/xxx",
		WithChannel("#alerts"),
		WithUsername("Bot"),
		WithIconEmoji(":robot_face:"),
		WithTemplate(Template{
			Text:  "{{.message}}",
			Title: "Alert",
			Color: "danger",
		}),
	)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var restored Connector
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if restored.Type != original.Type {
		t.Errorf("type mismatch: %s != %s", restored.Type, original.Type)
	}
	if restored.Config.WebhookURL != original.Config.WebhookURL {
		t.Errorf("webhook URL mismatch")
	}
	if restored.Config.Channel != original.Config.Channel {
		t.Errorf("channel mismatch")
	}
	if restored.Template.Text != original.Template.Text {
		t.Errorf("template text mismatch")
	}
}
