package domain

import (
	"time"

	"github.com/google/uuid"
)

// EventType represents a registered event type with optional JSONSchema validation.
// Event types define the structure and documentation for events that can be sent
// through the webhook system.
type EventType struct {
	ID            uuid.UUID
	ClientID      string
	Name          string // Event type name, e.g., "order.created"
	Description   string
	Schema        []byte // JSONSchema for payload validation (optional)
	SchemaVersion string // Schema version, e.g., "1.0", "1.1"
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewEventType creates a new event type with the given name and description.
func NewEventType(clientID, name, description string) EventType {
	now := time.Now().UTC()
	return EventType{
		ID:          uuid.New(),
		ClientID:    clientID,
		Name:        name,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// WithSchema returns a new EventType with the given schema and version.
func (et EventType) WithSchema(schema []byte, version string) EventType {
	return EventType{
		ID:            et.ID,
		ClientID:      et.ClientID,
		Name:          et.Name,
		Description:   et.Description,
		Schema:        schema,
		SchemaVersion: version,
		CreatedAt:     et.CreatedAt,
		UpdatedAt:     time.Now().UTC(),
	}
}

// WithDescription returns a new EventType with the given description.
func (et EventType) WithDescription(description string) EventType {
	return EventType{
		ID:            et.ID,
		ClientID:      et.ClientID,
		Name:          et.Name,
		Description:   description,
		Schema:        et.Schema,
		SchemaVersion: et.SchemaVersion,
		CreatedAt:     et.CreatedAt,
		UpdatedAt:     time.Now().UTC(),
	}
}

// HasSchema returns true if the event type has a JSONSchema defined.
func (et EventType) HasSchema() bool {
	return len(et.Schema) > 0
}

// ClearSchema returns a new EventType with the schema removed.
func (et EventType) ClearSchema() EventType {
	return EventType{
		ID:            et.ID,
		ClientID:      et.ClientID,
		Name:          et.Name,
		Description:   et.Description,
		Schema:        nil,
		SchemaVersion: "",
		CreatedAt:     et.CreatedAt,
		UpdatedAt:     time.Now().UTC(),
	}
}
