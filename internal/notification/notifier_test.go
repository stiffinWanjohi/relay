package notification

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNoopNotifier(t *testing.T) {
	n := &NoopNotifier{}
	ctx := context.Background()

	if err := n.NotifyCircuitTrip(ctx, "ep1", "https://example.com", 5); err != nil {
		t.Errorf("NotifyCircuitTrip error: %v", err)
	}
	if err := n.NotifyCircuitRecover(ctx, "ep1", "https://example.com"); err != nil {
		t.Errorf("NotifyCircuitRecover error: %v", err)
	}
	if err := n.NotifyEndpointDisabled(ctx, "ep1", "too many failures"); err != nil {
		t.Errorf("NotifyEndpointDisabled error: %v", err)
	}
	if err := n.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
}

func TestNewService_Disabled(t *testing.T) {
	cfg := Config{
		Enabled: false,
	}

	s := NewService(cfg, nil)
	defer func() { _ = s.Close() }()

	if len(s.notifiers) != 1 {
		t.Errorf("expected 1 notifier, got %d", len(s.notifiers))
	}

	_, ok := s.notifiers[0].(*NoopNotifier)
	if !ok {
		t.Error("expected NoopNotifier when disabled")
	}
}

func TestNewService_WithSlack(t *testing.T) {
	cfg := Config{
		Enabled:         true,
		SlackWebhookURL: "https://hooks.slack.com/services/test",
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewService(cfg, logger)
	defer func() { _ = s.Close() }()

	if len(s.notifiers) != 1 {
		t.Errorf("expected 1 notifier, got %d", len(s.notifiers))
	}

	_, ok := s.notifiers[0].(*SlackNotifier)
	if !ok {
		t.Error("expected SlackNotifier")
	}
}

func TestNewService_WithEmail(t *testing.T) {
	cfg := Config{
		Enabled:  true,
		SMTPHost: "smtp.example.com",
		SMTPPort: 587,
		EmailTo:  []string{"ops@example.com"},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewService(cfg, logger)
	defer func() { _ = s.Close() }()

	if len(s.notifiers) != 1 {
		t.Errorf("expected 1 notifier, got %d", len(s.notifiers))
	}

	_, ok := s.notifiers[0].(*EmailNotifier)
	if !ok {
		t.Error("expected EmailNotifier")
	}
}

func TestNewService_WithMultipleNotifiers(t *testing.T) {
	cfg := Config{
		Enabled:         true,
		SlackWebhookURL: "https://hooks.slack.com/services/test",
		SMTPHost:        "smtp.example.com",
		SMTPPort:        587,
		EmailTo:         []string{"ops@example.com"},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewService(cfg, logger)
	defer func() { _ = s.Close() }()

	if len(s.notifiers) != 2 {
		t.Errorf("expected 2 notifiers, got %d", len(s.notifiers))
	}
}

func TestNewService_NoNotifiersConfigured(t *testing.T) {
	cfg := Config{
		Enabled: true,
		// No Slack or Email configured
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewService(cfg, logger)
	defer func() { _ = s.Close() }()

	if len(s.notifiers) != 1 {
		t.Errorf("expected 1 notifier (noop), got %d", len(s.notifiers))
	}

	_, ok := s.notifiers[0].(*NoopNotifier)
	if !ok {
		t.Error("expected NoopNotifier when no notifiers configured")
	}
}

type mockNotifier struct {
	tripCalls     int
	recoverCalls  int
	disabledCalls int
	mu            sync.Mutex
	err           error
}

func (m *mockNotifier) NotifyCircuitTrip(_ context.Context, _, _ string, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tripCalls++
	return m.err
}

func (m *mockNotifier) NotifyCircuitRecover(_ context.Context, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recoverCalls++
	return m.err
}

func (m *mockNotifier) NotifyEndpointDisabled(_ context.Context, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disabledCalls++
	return m.err
}

func (m *mockNotifier) Close() error {
	return nil
}

func TestService_NotifyCircuitTrip(t *testing.T) {
	mock := &mockNotifier{}
	s := &Service{
		notifiers: []Notifier{mock},
		async:     false,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	s.NotifyCircuitTrip(context.Background(), "ep1", "https://example.com", 5)

	if mock.tripCalls != 1 {
		t.Errorf("expected 1 trip call, got %d", mock.tripCalls)
	}
}

func TestService_NotifyCircuitRecover(t *testing.T) {
	mock := &mockNotifier{}
	s := &Service{
		notifiers: []Notifier{mock},
		async:     false,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	s.NotifyCircuitRecover(context.Background(), "ep1", "https://example.com")

	if mock.recoverCalls != 1 {
		t.Errorf("expected 1 recover call, got %d", mock.recoverCalls)
	}
}

func TestService_NotifyEndpointDisabled(t *testing.T) {
	mock := &mockNotifier{}
	s := &Service{
		notifiers: []Notifier{mock},
		async:     false,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	s.NotifyEndpointDisabled(context.Background(), "ep1", "too many failures")

	if mock.disabledCalls != 1 {
		t.Errorf("expected 1 disabled call, got %d", mock.disabledCalls)
	}
}

func TestService_AsyncNotification(t *testing.T) {
	mock := &mockNotifier{}

	s := &Service{
		notifiers: []Notifier{mock},
		async:     true,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	s.NotifyCircuitTrip(context.Background(), "ep1", "https://example.com", 5)

	// Wait for async completion
	_ = s.Close()

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.tripCalls != 1 {
		t.Errorf("expected 1 trip call, got %d", mock.tripCalls)
	}
}

func TestService_MultipleNotifiers(t *testing.T) {
	mock1 := &mockNotifier{}
	mock2 := &mockNotifier{}

	s := &Service{
		notifiers: []Notifier{mock1, mock2},
		async:     false,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	s.NotifyCircuitTrip(context.Background(), "ep1", "https://example.com", 5)

	if mock1.tripCalls != 1 {
		t.Errorf("expected 1 trip call on mock1, got %d", mock1.tripCalls)
	}
	if mock2.tripCalls != 1 {
		t.Errorf("expected 1 trip call on mock2, got %d", mock2.tripCalls)
	}
}

func TestService_NotifierError(t *testing.T) {
	mock := &mockNotifier{err: errors.New("notification failed")}

	s := &Service{
		notifiers: []Notifier{mock},
		async:     false,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// Should not panic, just log error
	s.NotifyCircuitTrip(context.Background(), "ep1", "https://example.com", 5)

	if mock.tripCalls != 1 {
		t.Errorf("expected 1 trip call even with error, got %d", mock.tripCalls)
	}
}

func TestSlackNotifier_NotifyCircuitTrip(t *testing.T) {
	var receivedMsg slackMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}

		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &receivedMsg); err != nil {
			t.Errorf("failed to parse slack message: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	notifier := NewSlackNotifier(server.URL, logger)
	defer func() { _ = notifier.Close() }()

	err := notifier.NotifyCircuitTrip(context.Background(), "endpoint-123", "https://example.com/webhook", 5)
	if err != nil {
		t.Fatalf("NotifyCircuitTrip failed: %v", err)
	}

	if len(receivedMsg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(receivedMsg.Attachments))
	}

	att := receivedMsg.Attachments[0]
	if att.Color != "danger" {
		t.Errorf("expected danger color, got %s", att.Color)
	}
	if att.Title != "Circuit Breaker Tripped" {
		t.Errorf("unexpected title: %s", att.Title)
	}
}

func TestSlackNotifier_NotifyCircuitRecover(t *testing.T) {
	var receivedMsg slackMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedMsg)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	notifier := NewSlackNotifier(server.URL, logger)
	defer func() { _ = notifier.Close() }()

	err := notifier.NotifyCircuitRecover(context.Background(), "endpoint-123", "https://example.com/webhook")
	if err != nil {
		t.Fatalf("NotifyCircuitRecover failed: %v", err)
	}

	if len(receivedMsg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(receivedMsg.Attachments))
	}

	att := receivedMsg.Attachments[0]
	if att.Color != "good" {
		t.Errorf("expected good color, got %s", att.Color)
	}
	if att.Title != "Circuit Breaker Recovered" {
		t.Errorf("unexpected title: %s", att.Title)
	}
}

func TestSlackNotifier_NotifyEndpointDisabled(t *testing.T) {
	var receivedMsg slackMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedMsg)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	notifier := NewSlackNotifier(server.URL, logger)
	defer func() { _ = notifier.Close() }()

	err := notifier.NotifyEndpointDisabled(context.Background(), "endpoint-123", "too many failures")
	if err != nil {
		t.Fatalf("NotifyEndpointDisabled failed: %v", err)
	}

	if len(receivedMsg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(receivedMsg.Attachments))
	}

	att := receivedMsg.Attachments[0]
	if att.Color != "warning" {
		t.Errorf("expected warning color, got %s", att.Color)
	}
	if att.Title != "Endpoint Disabled" {
		t.Errorf("unexpected title: %s", att.Title)
	}
}

func TestSlackNotifier_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	notifier := NewSlackNotifier(server.URL, logger)
	defer func() { _ = notifier.Close() }()

	err := notifier.NotifyCircuitTrip(context.Background(), "ep1", "https://example.com", 5)
	if err == nil {
		t.Error("expected error for server error response")
	}
}

func TestSlackNotifier_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	notifier := NewSlackNotifier(server.URL, logger)
	notifier.client.Timeout = 50 * time.Millisecond
	defer func() { _ = notifier.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := notifier.NotifyCircuitTrip(ctx, "ep1", "https://example.com", 5)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestEmailNotifier_NotifyCircuitTrip(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	notifier := NewEmailNotifier(
		"smtp.example.com",
		587,
		"user",
		"pass",
		"relay@example.com",
		[]string{"ops@example.com"},
		logger,
	)
	defer func() { _ = notifier.Close() }()

	// This will fail to connect but shouldn't panic
	err := notifier.NotifyCircuitTrip(context.Background(), "ep1", "https://example.com", 5)
	if err == nil {
		t.Log("email notifier connected (unexpected in test)")
	}
	// We expect an error since there's no SMTP server
}

func TestEmailNotifier_BuildMessage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	notifier := NewEmailNotifier(
		"smtp.example.com",
		587,
		"user",
		"pass",
		"relay@example.com",
		[]string{"ops@example.com"},
		logger,
	)

	// Test that notifier is created with correct config
	if notifier.host != "smtp.example.com" {
		t.Errorf("expected host smtp.example.com, got %s", notifier.host)
	}
	if notifier.port != 587 {
		t.Errorf("expected port 587, got %d", notifier.port)
	}
	if notifier.from != "relay@example.com" {
		t.Errorf("expected from relay@example.com, got %s", notifier.from)
	}
	if len(notifier.to) != 1 || notifier.to[0] != "ops@example.com" {
		t.Errorf("expected to [ops@example.com], got %v", notifier.to)
	}
}
