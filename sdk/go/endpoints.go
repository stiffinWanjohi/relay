package relay

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// CreateEndpointRequest is the request to create a new endpoint.
type CreateEndpointRequest struct {
	URL              string            `json:"url"`
	Description      string            `json:"description,omitempty"`
	EventTypes       []string          `json:"eventTypes"`
	MaxRetries       *int              `json:"maxRetries,omitempty"`
	RetryBackoffMs   *int              `json:"retryBackoffMs,omitempty"`
	RetryBackoffMax  *int              `json:"retryBackoffMax,omitempty"`
	RetryBackoffMult *float64          `json:"retryBackoffMult,omitempty"`
	TimeoutMs        *int              `json:"timeoutMs,omitempty"`
	RateLimitPerSec  *int              `json:"rateLimitPerSec,omitempty"`
	CircuitThreshold *int              `json:"circuitThreshold,omitempty"`
	CircuitResetMs   *int              `json:"circuitResetMs,omitempty"`
	CustomHeaders    map[string]string `json:"customHeaders,omitempty"`
}

// UpdateEndpointRequest is the request to update an endpoint.
type UpdateEndpointRequest struct {
	URL              *string           `json:"url,omitempty"`
	Description      *string           `json:"description,omitempty"`
	EventTypes       []string          `json:"eventTypes,omitempty"`
	Status           *EndpointStatus   `json:"status,omitempty"`
	MaxRetries       *int              `json:"maxRetries,omitempty"`
	RetryBackoffMs   *int              `json:"retryBackoffMs,omitempty"`
	RetryBackoffMax  *int              `json:"retryBackoffMax,omitempty"`
	RetryBackoffMult *float64          `json:"retryBackoffMult,omitempty"`
	TimeoutMs        *int              `json:"timeoutMs,omitempty"`
	RateLimitPerSec  *int              `json:"rateLimitPerSec,omitempty"`
	CircuitThreshold *int              `json:"circuitThreshold,omitempty"`
	CircuitResetMs   *int              `json:"circuitResetMs,omitempty"`
	CustomHeaders    map[string]string `json:"customHeaders,omitempty"`
}

// CreateEndpoint creates a new webhook endpoint.
func (c *Client) CreateEndpoint(ctx context.Context, req CreateEndpointRequest) (*Endpoint, error) {
	var endpoint Endpoint
	err := c.do(ctx, "POST", "/api/v1/endpoints", req, &endpoint, nil)
	if err != nil {
		return nil, err
	}
	return &endpoint, nil
}

// GetEndpoint retrieves an endpoint by ID.
func (c *Client) GetEndpoint(ctx context.Context, id string) (*EndpointWithStats, error) {
	var endpoint EndpointWithStats
	err := c.do(ctx, "GET", "/api/v1/endpoints/"+id, nil, &endpoint, nil)
	if err != nil {
		return nil, err
	}
	return &endpoint, nil
}

// ListEndpointsOptions configures the ListEndpoints request.
type ListEndpointsOptions struct {
	Status *EndpointStatus
	Limit  int
	Cursor string
}

// ListEndpoints returns a paginated list of endpoints.
func (c *Client) ListEndpoints(ctx context.Context, opts ListEndpointsOptions) (*EndpointList, error) {
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

	path := "/api/v1/endpoints"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var list EndpointList
	err := c.do(ctx, "GET", path, nil, &list, nil)
	if err != nil {
		return nil, err
	}
	return &list, nil
}

// UpdateEndpoint updates an existing endpoint.
func (c *Client) UpdateEndpoint(ctx context.Context, id string, req UpdateEndpointRequest) (*Endpoint, error) {
	var endpoint Endpoint
	err := c.do(ctx, "PATCH", "/api/v1/endpoints/"+id, req, &endpoint, nil)
	if err != nil {
		return nil, err
	}
	return &endpoint, nil
}

// DeleteEndpoint deletes an endpoint.
func (c *Client) DeleteEndpoint(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/api/v1/endpoints/"+id, nil, nil, nil)
}

// PauseEndpoint pauses an endpoint (stops receiving events).
func (c *Client) PauseEndpoint(ctx context.Context, id string) (*Endpoint, error) {
	status := EndpointStatusPaused
	return c.UpdateEndpoint(ctx, id, UpdateEndpointRequest{Status: &status})
}

// ResumeEndpoint resumes a paused endpoint.
func (c *Client) ResumeEndpoint(ctx context.Context, id string) (*Endpoint, error) {
	status := EndpointStatusActive
	return c.UpdateEndpoint(ctx, id, UpdateEndpointRequest{Status: &status})
}

// RotateEndpointSecret generates and sets a new signing secret for an endpoint.
// Returns the new secret (only shown once).
func (c *Client) RotateEndpointSecret(ctx context.Context, id string) (string, *Endpoint, error) {
	query := `
		mutation RotateEndpointSecret($id: ID!) {
			rotateEndpointSecret(id: $id) {
				newSecret
				endpoint {
					id
					url
					hasCustomSecret
					secretRotatedAt
				}
			}
		}
	`

	variables := map[string]any{
		"id": id,
	}

	var result struct {
		Data struct {
			RotateEndpointSecret struct {
				NewSecret string   `json:"newSecret"`
				Endpoint  Endpoint `json:"endpoint"`
			} `json:"rotateEndpointSecret"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	err := c.do(ctx, "POST", "/graphql", map[string]any{
		"query":     query,
		"variables": variables,
	}, &result, nil)
	if err != nil {
		return "", nil, err
	}

	if len(result.Errors) > 0 {
		return "", nil, fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	return result.Data.RotateEndpointSecret.NewSecret, &result.Data.RotateEndpointSecret.Endpoint, nil
}

// ClearPreviousSecret removes the previous secret after rotation grace period.
func (c *Client) ClearPreviousSecret(ctx context.Context, id string) (*Endpoint, error) {
	query := `
		mutation ClearPreviousSecret($id: ID!) {
			clearPreviousSecret(id: $id) {
				id
				url
				hasCustomSecret
				secretRotatedAt
			}
		}
	`

	variables := map[string]any{
		"id": id,
	}

	var result struct {
		Data struct {
			ClearPreviousSecret Endpoint `json:"clearPreviousSecret"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	err := c.do(ctx, "POST", "/graphql", map[string]any{
		"query":     query,
		"variables": variables,
	}, &result, nil)
	if err != nil {
		return nil, err
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", result.Errors[0].Message)
	}

	return &result.Data.ClearPreviousSecret, nil
}

// GetStats retrieves queue statistics.
func (c *Client) GetStats(ctx context.Context) (*QueueStats, error) {
	var stats QueueStats
	err := c.do(ctx, "GET", "/api/v1/stats", nil, &stats, nil)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}
