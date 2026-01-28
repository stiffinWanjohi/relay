package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// CreateEventRequest is the request to create a new event.
type CreateEventRequest struct {
	Destination string            `json:"destination,omitempty"`
	EventType   string            `json:"eventType,omitempty"`
	Payload     any               `json:"payload"`
	Headers     map[string]string `json:"headers,omitempty"`
	MaxAttempts *int              `json:"maxAttempts,omitempty"`
}

// CreateEvent creates a new webhook event.
func (c *Client) CreateEvent(ctx context.Context, idempotencyKey string, req CreateEventRequest) (*Event, error) {
	var event Event
	headers := map[string]string{
		"X-Idempotency-Key": idempotencyKey,
	}
	err := c.do(ctx, "POST", "/api/v1/events", req, &event, headers)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// GetEvent retrieves an event by ID with its delivery attempts.
func (c *Client) GetEvent(ctx context.Context, id string) (*EventWithAttempts, error) {
	var event EventWithAttempts
	err := c.do(ctx, "GET", "/api/v1/events/"+id, nil, &event, nil)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// ListEventsOptions configures the ListEvents request.
type ListEventsOptions struct {
	Status *EventStatus
	Limit  int
	Cursor string
}

// ListEvents returns a paginated list of events.
func (c *Client) ListEvents(ctx context.Context, opts ListEventsOptions) (*EventList, error) {
	params := url.Values{}
	if opts.Status != nil {
		params.Set("status", string(*opts.Status))
	}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		params.Set("cursor", opts.Cursor)
	}

	path := "/api/v1/events"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var list EventList
	err := c.do(ctx, "GET", path, nil, &list, nil)
	if err != nil {
		return nil, err
	}
	return &list, nil
}

// ReplayEvent replays a failed or dead event.
func (c *Client) ReplayEvent(ctx context.Context, id string) (*Event, error) {
	var event Event
	err := c.do(ctx, "POST", "/api/v1/events/"+id+"/replay", nil, &event, nil)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// BatchRetryByIDs retries multiple events by their IDs.
func (c *Client) BatchRetryByIDs(ctx context.Context, ids []string) (*BatchRetryResult, error) {
	req := map[string]any{
		"event_ids": ids,
	}
	var result BatchRetryResult
	err := c.do(ctx, "POST", "/api/v1/events/batch/retry", req, &result, nil)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// BatchRetryByStatus retries all events with the given status.
func (c *Client) BatchRetryByStatus(ctx context.Context, status EventStatus, limit int) (*BatchRetryResult, error) {
	req := map[string]any{
		"status": string(status),
	}
	if limit > 0 {
		req["limit"] = limit
	}
	var result BatchRetryResult
	err := c.do(ctx, "POST", "/api/v1/events/batch/retry", req, &result, nil)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// BatchRetryByEndpoint retries all failed/dead events for an endpoint.
func (c *Client) BatchRetryByEndpoint(ctx context.Context, endpointID string, status EventStatus, limit int) (*BatchRetryResult, error) {
	req := map[string]any{
		"endpoint_id": endpointID,
		"status":      string(status),
	}
	if limit > 0 {
		req["limit"] = limit
	}
	var result BatchRetryResult
	err := c.do(ctx, "POST", "/api/v1/events/batch/retry", req, &result, nil)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// SendEvent sends an event to all endpoints subscribed to the event type.
// This is a convenience method that uses the GraphQL API.
func (c *Client) SendEvent(ctx context.Context, idempotencyKey, eventType string, payload any, headers map[string]string) ([]Event, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	query := `
		mutation SendEvent($input: SendEventInput!, $idempotencyKey: String!) {
			sendEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
				idempotencyKey
				destination
				eventType
				status
				attempts
				maxAttempts
				createdAt
			}
		}
	`

	variables := map[string]any{
		"idempotencyKey": idempotencyKey,
		"input": map[string]any{
			"eventType": eventType,
			"payload":   json.RawMessage(payloadJSON),
			"headers":   headers,
		},
	}

	var result struct {
		Data struct {
			SendEvent []Event `json:"sendEvent"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	err = c.do(ctx, "POST", "/graphql", map[string]any{
		"query":     query,
		"variables": variables,
	}, &result, nil)
	if err != nil {
		return nil, err
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	return result.Data.SendEvent, nil
}
