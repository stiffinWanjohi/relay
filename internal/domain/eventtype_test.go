package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewEventType(t *testing.T) {
	clientID := "client-123"
	name := "order.created"
	description := "Fired when a new order is created"

	et := NewEventType(clientID, name, description)

	if et.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if et.ClientID != clientID {
		t.Errorf("expected clientID %q, got %q", clientID, et.ClientID)
	}
	if et.Name != name {
		t.Errorf("expected name %q, got %q", name, et.Name)
	}
	if et.Description != description {
		t.Errorf("expected description %q, got %q", description, et.Description)
	}
	if et.Schema != nil {
		t.Error("expected nil schema")
	}
	if et.SchemaVersion != "" {
		t.Errorf("expected empty schema version, got %q", et.SchemaVersion)
	}
	if et.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if et.UpdatedAt.IsZero() {
		t.Error("expected non-zero UpdatedAt")
	}
}

func TestEventType_WithSchema(t *testing.T) {
	et := NewEventType("client-123", "order.created", "Order created event")
	originalCreatedAt := et.CreatedAt

	time.Sleep(1 * time.Millisecond) // Ensure time difference

	schema := []byte(`{"type": "object", "properties": {"order_id": {"type": "string"}}}`)
	version := "1.0"

	updated := et.WithSchema(schema, version)

	// Original should be unchanged (immutability)
	if et.Schema != nil {
		t.Error("original event type should not have schema")
	}

	// Updated should have schema
	if string(updated.Schema) != string(schema) {
		t.Errorf("expected schema %q, got %q", string(schema), string(updated.Schema))
	}
	if updated.SchemaVersion != version {
		t.Errorf("expected schema version %q, got %q", version, updated.SchemaVersion)
	}

	// ID and ClientID should be preserved
	if updated.ID != et.ID {
		t.Error("ID should be preserved")
	}
	if updated.ClientID != et.ClientID {
		t.Error("ClientID should be preserved")
	}

	// CreatedAt should be preserved, UpdatedAt should change
	if !updated.CreatedAt.Equal(originalCreatedAt) {
		t.Error("CreatedAt should be preserved")
	}
	if !updated.UpdatedAt.After(originalCreatedAt) {
		t.Error("UpdatedAt should be after original")
	}
}

func TestEventType_WithDescription(t *testing.T) {
	et := NewEventType("client-123", "order.created", "Original description")

	newDescription := "Updated description"
	updated := et.WithDescription(newDescription)

	// Original should be unchanged (immutability)
	if et.Description != "Original description" {
		t.Error("original event type description should not change")
	}

	// Updated should have new description
	if updated.Description != newDescription {
		t.Errorf("expected description %q, got %q", newDescription, updated.Description)
	}

	// ID should be preserved
	if updated.ID != et.ID {
		t.Error("ID should be preserved")
	}
}

func TestEventType_HasSchema(t *testing.T) {
	et := NewEventType("client-123", "order.created", "")

	if et.HasSchema() {
		t.Error("new event type should not have schema")
	}

	schema := []byte(`{"type": "object"}`)
	withSchema := et.WithSchema(schema, "1.0")

	if !withSchema.HasSchema() {
		t.Error("event type with schema should return HasSchema=true")
	}
}

func TestEventType_ClearSchema(t *testing.T) {
	et := NewEventType("client-123", "order.created", "")
	et = et.WithSchema([]byte(`{"type": "object"}`), "1.0")

	if !et.HasSchema() {
		t.Error("event type should have schema before clearing")
	}

	cleared := et.ClearSchema()

	// Original should still have schema (immutability)
	if !et.HasSchema() {
		t.Error("original event type should still have schema")
	}

	// Cleared should not have schema
	if cleared.HasSchema() {
		t.Error("cleared event type should not have schema")
	}
	if cleared.SchemaVersion != "" {
		t.Error("cleared event type should not have schema version")
	}

	// ID should be preserved
	if cleared.ID != et.ID {
		t.Error("ID should be preserved")
	}
}
