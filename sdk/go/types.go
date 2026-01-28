package relay

import (
	"encoding/json"
	"time"
)

// EventStatus represents the status of an event.
type EventStatus string

const (
	EventStatusQueued     EventStatus = "queued"
	EventStatusDelivering EventStatus = "delivering"
	EventStatusDelivered  EventStatus = "delivered"
	EventStatusFailed     EventStatus = "failed"
	EventStatusDead       EventStatus = "dead"
)

// EndpointStatus represents the status of an endpoint.
type EndpointStatus string

const (
	EndpointStatusActive   EndpointStatus = "active"
	EndpointStatusPaused   EndpointStatus = "paused"
	EndpointStatusDisabled EndpointStatus = "disabled"
)

// Event represents a webhook event.
type Event struct {
	ID             string            `json:"id"`
	IdempotencyKey string            `json:"idempotencyKey"`
	Destination    string            `json:"destination"`
	EventType      string            `json:"eventType,omitempty"`
	Payload        json.RawMessage   `json:"payload,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Status         EventStatus       `json:"status"`
	Attempts       int               `json:"attempts"`
	MaxAttempts    int               `json:"maxAttempts"`
	CreatedAt      time.Time         `json:"createdAt"`
	DeliveredAt    *time.Time        `json:"deliveredAt,omitempty"`
	NextAttemptAt  *time.Time        `json:"nextAttemptAt,omitempty"`
}

// DeliveryAttempt represents a single delivery attempt.
type DeliveryAttempt struct {
	ID            string    `json:"id"`
	AttemptNumber int       `json:"attemptNumber"`
	StatusCode    *int      `json:"statusCode,omitempty"`
	ResponseBody  string    `json:"responseBody,omitempty"`
	Error         string    `json:"error,omitempty"`
	DurationMs    int       `json:"durationMs"`
	AttemptedAt   time.Time `json:"attemptedAt"`
}

// EventWithAttempts includes an event with its delivery history.
type EventWithAttempts struct {
	Event
	DeliveryAttempts []DeliveryAttempt `json:"deliveryAttempts"`
}

// Endpoint represents a webhook endpoint.
type Endpoint struct {
	ID               string            `json:"id"`
	URL              string            `json:"url"`
	Description      string            `json:"description,omitempty"`
	EventTypes       []string          `json:"eventTypes"`
	Status           EndpointStatus    `json:"status"`
	MaxRetries       int               `json:"maxRetries"`
	RetryBackoffMs   int               `json:"retryBackoffMs"`
	RetryBackoffMax  int               `json:"retryBackoffMax"`
	RetryBackoffMult float64           `json:"retryBackoffMult"`
	TimeoutMs        int               `json:"timeoutMs"`
	RateLimitPerSec  int               `json:"rateLimitPerSec"`
	CircuitThreshold int               `json:"circuitThreshold"`
	CircuitResetMs   int               `json:"circuitResetMs"`
	CustomHeaders    map[string]string `json:"customHeaders,omitempty"`
	HasCustomSecret  bool              `json:"hasCustomSecret"`
	SecretRotatedAt  *time.Time        `json:"secretRotatedAt,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}

// EndpointStats holds statistics for an endpoint.
type EndpointStats struct {
	TotalEvents  int     `json:"totalEvents"`
	Delivered    int     `json:"delivered"`
	Failed       int     `json:"failed"`
	Pending      int     `json:"pending"`
	SuccessRate  float64 `json:"successRate"`
	AvgLatencyMs float64 `json:"avgLatencyMs"`
}

// EndpointWithStats includes an endpoint with its statistics.
type EndpointWithStats struct {
	Endpoint
	Stats EndpointStats `json:"stats"`
}

// QueueStats holds queue statistics.
type QueueStats struct {
	Queued     int `json:"queued"`
	Delivering int `json:"delivering"`
	Delivered  int `json:"delivered"`
	Failed     int `json:"failed"`
	Dead       int `json:"dead"`
	Pending    int `json:"pending"`
	Processing int `json:"processing"`
	Delayed    int `json:"delayed"`
}

// Pagination holds pagination information.
type Pagination struct {
	HasMore bool   `json:"hasMore"`
	Cursor  string `json:"cursor,omitempty"`
	Total   int    `json:"total"`
}

// EventList is a paginated list of events.
type EventList struct {
	Data       []Event    `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// EndpointList is a paginated list of endpoints.
type EndpointList struct {
	Data       []Endpoint `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// BatchRetryResult is the result of a batch retry operation.
type BatchRetryResult struct {
	Succeeded      []Event           `json:"succeeded"`
	Failed         []BatchRetryError `json:"failed"`
	TotalRequested int               `json:"totalRequested"`
	TotalSucceeded int               `json:"totalSucceeded"`
}

// BatchRetryError represents an event that failed to retry.
type BatchRetryError struct {
	EventID string `json:"eventId"`
	Error   string `json:"error"`
}
