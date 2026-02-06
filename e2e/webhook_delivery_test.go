package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Webhook.site API for creating and checking webhook requests
const webhookSiteAPI = "https://webhook.site"

// WebhookSiteToken represents a webhook.site token response
type WebhookSiteToken struct {
	UUID string `json:"uuid"`
}

// WebhookSiteRequest represents a received webhook request
type WebhookSiteRequest struct {
	UUID      string          `json:"uuid"`
	Content   string          `json:"content"`
	Headers   json.RawMessage `json:"headers"` // Can be array or object
	Method    string          `json:"method"`
	CreatedAt string          `json:"created_at"`
}

// GetHeaders parses headers from the raw JSON (webhook.site returns map[string][]string)
func (r *WebhookSiteRequest) GetHeaders() map[string]string {
	headers := make(map[string]string)

	// webhook.site returns headers as map[string][]string
	var arrayHeaders map[string][]string
	if err := json.Unmarshal(r.Headers, &arrayHeaders); err == nil {
		for k, v := range arrayHeaders {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		return headers
	}

	// Fallback: try parsing as simple map[string]string
	var objHeaders map[string]string
	if err := json.Unmarshal(r.Headers, &objHeaders); err == nil {
		return objHeaders
	}

	return headers
}

// WebhookSiteRequests represents the list of requests
type WebhookSiteRequests struct {
	Data  []WebhookSiteRequest `json:"data"`
	Total int                  `json:"total"`
}

// createWebhookSiteToken creates a new webhook.site token
func createWebhookSiteToken(t *testing.T) (tokenUUID, webhookURL string) {
	t.Helper()

	resp, err := http.Post(webhookSiteAPI+"/token", "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to create webhook.site token: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Failed to create webhook.site token: status=%d body=%s", resp.StatusCode, string(body))
	}

	var token WebhookSiteToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		t.Fatalf("Failed to decode webhook.site token: %v", err)
	}

	return token.UUID, fmt.Sprintf("%s/%s", webhookSiteAPI, token.UUID)
}

// getWebhookSiteRequests retrieves requests received by webhook.site
func getWebhookSiteRequests(t *testing.T, tokenUUID string) []WebhookSiteRequest {
	t.Helper()

	resp, err := http.Get(fmt.Sprintf("%s/token/%s/requests", webhookSiteAPI, tokenUUID))
	if err != nil {
		t.Fatalf("Failed to get webhook.site requests: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var requests WebhookSiteRequests
	if err := json.NewDecoder(resp.Body).Decode(&requests); err != nil {
		t.Fatalf("Failed to decode webhook.site requests: %v", err)
	}

	return requests.Data
}

// waitForWebhookRequest waits for a webhook request to be received
func waitForWebhookRequest(t *testing.T, tokenUUID string, timeout time.Duration) *WebhookSiteRequest {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		requests := getWebhookSiteRequests(t, tokenUUID)
		if len(requests) > 0 {
			return &requests[0]
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

// Test configuration from environment
func getTestConfig() (dbURL, redisURL, apiURL string) {
	dbURL = os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://relay:relay@localhost:5434/relay?sslmode=disable"
	}
	redisURL = os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6381"
	}
	apiURL = os.Getenv("API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8088"
	}
	return
}

func skipIfNoInfra(t *testing.T) (dbURL, redisURL, apiURL string) {
	dbURL, redisURL, apiURL = getTestConfig()

	// Check API availability
	resp, err := http.Get(apiURL + "/health")
	if err != nil {
		t.Skipf("Skipping E2E test: API not available at %s: %v", apiURL, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("Skipping E2E test: API health check failed with status %d", resp.StatusCode)
	}

	return
}

func skipIfNoWebhookSite(t *testing.T) {
	// Quick check if webhook.site is accessible
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(webhookSiteAPI)
	if err != nil {
		t.Skipf("Skipping E2E test: webhook.site not accessible: %v", err)
	}
	_ = resp.Body.Close()
}

// GraphQL request/response helpers
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

func executeGraphQL(t *testing.T, apiURL, query string, variables map[string]any) json.RawMessage {
	t.Helper()

	reqBody, err := json.Marshal(graphQLRequest{
		Query:     query,
		Variables: variables,
	})
	if err != nil {
		t.Fatalf("Failed to marshal GraphQL request: %v", err)
	}

	resp, err := http.Post(apiURL+"/graphql", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("Failed to execute GraphQL request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		t.Fatalf("Failed to unmarshal GraphQL response: %v\nBody: %s", err, string(body))
	}

	if len(gqlResp.Errors) > 0 {
		t.Fatalf("GraphQL errors: %v", gqlResp.Errors)
	}

	return gqlResp.Data
}

func executeGraphQLWithErrors(t *testing.T, apiURL, query string, variables map[string]any) (json.RawMessage, []string) {
	t.Helper()

	reqBody, _ := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	resp, err := http.Post(apiURL+"/graphql", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("Failed to execute GraphQL request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var gqlResp graphQLResponse
	_ = json.Unmarshal(body, &gqlResp)

	var errors []string
	for _, e := range gqlResp.Errors {
		errors = append(errors, e.Message)
	}
	return gqlResp.Data, errors
}

// TestWebhookDeliveryE2E tests the complete webhook delivery flow
func TestWebhookDeliveryE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)
	skipIfNoWebhookSite(t)

	// Create a webhook.site endpoint
	tokenUUID, webhookURL := createWebhookSiteToken(t)
	t.Logf("Created webhook.site endpoint: %s", webhookURL)

	// Create an event via GraphQL
	idempotencyKey := uuid.New().String()
	createEventQuery := `
		mutation CreateEvent($input: CreateEventInput!, $idempotencyKey: String!) {
			createEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
				status
				destination
				payload
				attempts
				maxAttempts
			}
		}
	`

	testPayload := map[string]any{
		"event_type": "user.created",
		"user_id":    "12345",
		"email":      "test@example.com",
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}

	variables := map[string]any{
		"input": map[string]any{
			"destination": webhookURL,
			"payload":     testPayload,
			"headers": map[string]any{
				"X-Custom-Header": "test-value",
			},
			"maxAttempts": 3,
		},
		"idempotencyKey": idempotencyKey,
	}

	data := executeGraphQL(t, apiURL, createEventQuery, variables)

	var createResp struct {
		CreateEvent struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			Destination string `json:"destination"`
			Attempts    int    `json:"attempts"`
			MaxAttempts int    `json:"maxAttempts"`
		} `json:"createEvent"`
	}
	if err := json.Unmarshal(data, &createResp); err != nil {
		t.Fatalf("Failed to unmarshal createEvent response: %v", err)
	}

	eventID := createResp.CreateEvent.ID
	if eventID == "" {
		t.Fatal("Event ID should not be empty")
	}

	t.Logf("Created event: %s with status: %s", eventID, createResp.CreateEvent.Status)

	// Wait for delivery (with timeout)
	deadline := time.Now().Add(60 * time.Second)
	var finalStatus string

	for time.Now().Before(deadline) {
		getEventQuery := `
			query GetEvent($id: ID!) {
				event(id: $id) {
					id
					status
					attempts
					deliveredAt
					deliveryAttempts {
						id
						statusCode
						durationMs
						attemptNumber
						attemptedAt
					}
				}
			}
		`

		data = executeGraphQL(t, apiURL, getEventQuery, map[string]any{"id": eventID})

		var eventResp struct {
			Event struct {
				ID               string `json:"id"`
				Status           string `json:"status"`
				Attempts         int    `json:"attempts"`
				DeliveredAt      string `json:"deliveredAt"`
				DeliveryAttempts []struct {
					ID            string `json:"id"`
					StatusCode    int    `json:"statusCode"`
					DurationMs    int    `json:"durationMs"`
					AttemptNumber int    `json:"attemptNumber"`
					AttemptedAt   string `json:"attemptedAt"`
				} `json:"deliveryAttempts"`
			} `json:"event"`
		}
		if err := json.Unmarshal(data, &eventResp); err != nil {
			t.Fatalf("Failed to unmarshal event response: %v", err)
		}

		finalStatus = eventResp.Event.Status
		if finalStatus == "DELIVERED" {
			t.Logf("Event delivered successfully after %d attempts", eventResp.Event.Attempts)

			// Verify delivery attempt was recorded
			if len(eventResp.Event.DeliveryAttempts) == 0 {
				t.Error("Expected at least one delivery attempt to be recorded")
			} else {
				// Check the last attempt (the successful one)
				attempt := eventResp.Event.DeliveryAttempts[len(eventResp.Event.DeliveryAttempts)-1]
				if attempt.StatusCode != 200 {
					t.Errorf("Expected status code 200, got %d", attempt.StatusCode)
				}
			}
			break
		}

		time.Sleep(2 * time.Second)
	}

	if finalStatus != "DELIVERED" {
		t.Errorf("Event was not delivered within timeout. Final status: %s", finalStatus)
	}

	// Verify webhook.site received the request
	webhookReq := waitForWebhookRequest(t, tokenUUID, 10*time.Second)
	if webhookReq == nil {
		t.Fatal("Webhook receiver did not receive any requests")
		return
	}

	// Verify payload
	var receivedData map[string]any
	if err := json.Unmarshal([]byte(webhookReq.Content), &receivedData); err != nil {
		t.Errorf("Failed to unmarshal received payload: %v", err)
	}

	if receivedData["user_id"] != "12345" {
		t.Errorf("Expected user_id '12345', got '%v'", receivedData["user_id"])
	}

	// Verify signature header was present
	headers := webhookReq.GetHeaders()
	if headers["x-relay-signature"] == "" && headers["X-Relay-Signature"] == "" {
		t.Error("Expected X-Relay-Signature header to be present")
	}

	t.Logf("Webhook received with headers: %v", headers)
}

// TestIdempotencyE2E tests that duplicate events are deduplicated
func TestIdempotencyE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)
	skipIfNoWebhookSite(t)

	_, webhookURL := createWebhookSiteToken(t)

	idempotencyKey := uuid.New().String()
	createEventQuery := `
		mutation CreateEvent($input: CreateEventInput!, $idempotencyKey: String!) {
			createEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
				status
			}
		}
	`

	variables := map[string]any{
		"input": map[string]any{
			"destination": webhookURL,
			"payload":     map[string]any{"test": "data"},
		},
		"idempotencyKey": idempotencyKey,
	}

	// First request
	data1 := executeGraphQL(t, apiURL, createEventQuery, variables)
	var resp1 struct {
		CreateEvent struct {
			ID string `json:"id"`
		} `json:"createEvent"`
	}
	_ = json.Unmarshal(data1, &resp1)
	firstID := resp1.CreateEvent.ID
	t.Logf("First request returned ID: %s", firstID)

	// Wait for the first request to fully complete (idempotency key gets updated with real ID)
	time.Sleep(1 * time.Second)

	// Second request with same idempotency key - should return same event
	data2 := executeGraphQL(t, apiURL, createEventQuery, variables)
	var resp2 struct {
		CreateEvent struct {
			ID string `json:"id"`
		} `json:"createEvent"`
	}
	_ = json.Unmarshal(data2, &resp2)
	secondID := resp2.CreateEvent.ID
	t.Logf("Second request returned ID: %s", secondID)

	// Should return the same event ID
	if firstID != secondID {
		t.Errorf("Expected same event ID for duplicate requests. First: %s, Second: %s", firstID, secondID)
	}

	t.Logf("Idempotency working: both requests returned event ID %s", firstID)
}

// TestQueueStatsE2E tests the queue statistics endpoint
func TestQueueStatsE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)

	query := `
		query {
			queueStats {
				queued
				delivering
				delivered
				failed
				dead
				pending
				processing
				delayed
			}
		}
	`

	data := executeGraphQL(t, apiURL, query, nil)

	var resp struct {
		QueueStats struct {
			Queued     int `json:"queued"`
			Delivering int `json:"delivering"`
			Delivered  int `json:"delivered"`
			Failed     int `json:"failed"`
			Dead       int `json:"dead"`
			Pending    int `json:"pending"`
			Processing int `json:"processing"`
			Delayed    int `json:"delayed"`
		} `json:"queueStats"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("Failed to unmarshal queue stats: %v", err)
	}

	// Just verify we got valid stats (non-negative)
	if resp.QueueStats.Queued < 0 || resp.QueueStats.Delivered < 0 {
		t.Error("Queue stats should be non-negative")
	}

	t.Logf("Queue stats: queued=%d, delivering=%d, delivered=%d, failed=%d, dead=%d",
		resp.QueueStats.Queued, resp.QueueStats.Delivering, resp.QueueStats.Delivered,
		resp.QueueStats.Failed, resp.QueueStats.Dead)
}

// TestConcurrentDeliveryE2E tests that multiple events can be delivered concurrently
func TestConcurrentDeliveryE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)
	skipIfNoWebhookSite(t)

	// Create webhook endpoints for each event
	numEvents := 5
	eventIDs := make([]string, numEvents)
	var wg sync.WaitGroup

	createEventQuery := `
		mutation CreateEvent($input: CreateEventInput!, $idempotencyKey: String!) {
			createEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
			}
		}
	`

	for i := range numEvents {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			_, webhookURL := createWebhookSiteToken(t)

			variables := map[string]any{
				"input": map[string]any{
					"destination": webhookURL,
					"payload":     map[string]any{"id": fmt.Sprintf("event-%d", idx), "index": idx},
				},
				"idempotencyKey": uuid.New().String(),
			}

			data := executeGraphQL(t, apiURL, createEventQuery, variables)
			var resp struct {
				CreateEvent struct {
					ID string `json:"id"`
				} `json:"createEvent"`
			}
			_ = json.Unmarshal(data, &resp)
			eventIDs[idx] = resp.CreateEvent.ID
		}(i)
	}

	wg.Wait()

	// Wait for all events to be delivered
	deadline := time.Now().Add(90 * time.Second)
	deliveredCount := 0

	for time.Now().Before(deadline) {
		deliveredCount = 0
		for _, eventID := range eventIDs {
			if eventID == "" {
				continue
			}

			getEventQuery := `query GetEvent($id: ID!) { event(id: $id) { status } }`
			data := executeGraphQL(t, apiURL, getEventQuery, map[string]any{"id": eventID})

			var resp struct {
				Event struct {
					Status string `json:"status"`
				} `json:"event"`
			}
			_ = json.Unmarshal(data, &resp)

			if resp.Event.Status == "DELIVERED" {
				deliveredCount++
			}
		}

		if deliveredCount == numEvents {
			break
		}

		time.Sleep(3 * time.Second)
	}

	t.Logf("Delivered %d/%d events", deliveredCount, numEvents)

	if deliveredCount != numEvents {
		t.Errorf("Expected all %d events to be delivered, but only %d were delivered", numEvents, deliveredCount)
	}
}

// TestWebhookSignatureE2E tests that webhook signatures are sent
func TestWebhookSignatureE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)
	skipIfNoWebhookSite(t)

	tokenUUID, webhookURL := createWebhookSiteToken(t)

	idempotencyKey := uuid.New().String()
	createEventQuery := `
		mutation CreateEvent($input: CreateEventInput!, $idempotencyKey: String!) {
			createEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
			}
		}
	`

	variables := map[string]any{
		"input": map[string]any{
			"destination": webhookURL,
			"payload":     map[string]any{"test": "signature"},
		},
		"idempotencyKey": idempotencyKey,
	}

	data := executeGraphQL(t, apiURL, createEventQuery, variables)
	var createResp struct {
		CreateEvent struct {
			ID string `json:"id"`
		} `json:"createEvent"`
	}
	_ = json.Unmarshal(data, &createResp)
	eventID := createResp.CreateEvent.ID

	// Wait for delivery
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		getEventQuery := `query GetEvent($id: ID!) { event(id: $id) { status } }`
		data = executeGraphQL(t, apiURL, getEventQuery, map[string]any{"id": eventID})

		var resp struct {
			Event struct {
				Status string `json:"status"`
			} `json:"event"`
		}
		_ = json.Unmarshal(data, &resp)

		if resp.Event.Status == "DELIVERED" {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// Check webhook.site for received request
	webhookReq := waitForWebhookRequest(t, tokenUUID, 10*time.Second)
	if webhookReq == nil {
		t.Fatal("Webhook receiver did not receive the request")
	}

	// Verify signature headers are present (case-insensitive check)
	headers := webhookReq.GetHeaders()
	hasSignature := false
	hasTimestamp := false
	for k := range headers {
		if k == "x-relay-signature" || k == "X-Relay-Signature" {
			hasSignature = true
		}
		if k == "x-relay-timestamp" || k == "X-Relay-Timestamp" {
			hasTimestamp = true
		}
	}

	if !hasSignature {
		t.Error("X-Relay-Signature header should be present")
	}
	if !hasTimestamp {
		t.Error("X-Relay-Timestamp header should be present")
	}

	t.Logf("Signature verification: signature=%v, timestamp=%v", hasSignature, hasTimestamp)
}

// TestSSRFProtectionE2E tests that internal URLs are blocked
func TestSSRFProtectionE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)

	createEventQuery := `
		mutation CreateEvent($input: CreateEventInput!, $idempotencyKey: String!) {
			createEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
			}
		}
	`

	internalURLs := []string{
		"http://localhost:8080/webhook",
		"http://127.0.0.1:8080/webhook",
		"http://192.168.1.1/webhook",
		"http://10.0.0.1/webhook",
		"http://169.254.169.254/latest/meta-data/", // AWS metadata
	}

	for _, internalURL := range internalURLs {
		t.Run(internalURL, func(t *testing.T) {
			variables := map[string]any{
				"input": map[string]any{
					"destination": internalURL,
					"payload":     map[string]any{"test": "ssrf"},
				},
				"idempotencyKey": uuid.New().String(),
			}

			_, errors := executeGraphQLWithErrors(t, apiURL, createEventQuery, variables)

			if len(errors) == 0 {
				t.Errorf("Expected error for internal URL %s, but request succeeded", internalURL)
			} else {
				foundSSRFError := false
				for _, err := range errors {
					if err == "invalid destination: internal/private URLs are not allowed" {
						foundSSRFError = true
						break
					}
				}
				if !foundSSRFError {
					t.Errorf("Expected SSRF protection error for %s, got: %v", internalURL, errors)
				}
			}
		})
	}
}

// TestReplayEventE2E tests replaying a delivered event
func TestReplayEventE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)
	skipIfNoWebhookSite(t)

	tokenUUID, webhookURL := createWebhookSiteToken(t)

	// Create and wait for event to be delivered
	idempotencyKey := uuid.New().String()
	createEventQuery := `
		mutation CreateEvent($input: CreateEventInput!, $idempotencyKey: String!) {
			createEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
			}
		}
	`

	variables := map[string]any{
		"input": map[string]any{
			"destination": webhookURL,
			"payload":     map[string]any{"test": "replay"},
		},
		"idempotencyKey": idempotencyKey,
	}

	data := executeGraphQL(t, apiURL, createEventQuery, variables)
	var createResp struct {
		CreateEvent struct {
			ID string `json:"id"`
		} `json:"createEvent"`
	}
	_ = json.Unmarshal(data, &createResp)
	eventID := createResp.CreateEvent.ID

	// Wait for initial delivery
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		getEventQuery := `query GetEvent($id: ID!) { event(id: $id) { status } }`
		data = executeGraphQL(t, apiURL, getEventQuery, map[string]any{"id": eventID})

		var resp struct {
			Event struct {
				Status string `json:"status"`
			} `json:"event"`
		}
		_ = json.Unmarshal(data, &resp)

		if resp.Event.Status == "DELIVERED" {
			t.Log("Initial delivery completed")
			break
		}
		time.Sleep(2 * time.Second)
	}

	// Count initial requests
	initialRequests := getWebhookSiteRequests(t, tokenUUID)
	initialCount := len(initialRequests)
	t.Logf("Initial request count: %d", initialCount)

	// Note: replayEvent only works for DEAD or FAILED events
	// For this test, we verify the replay mutation exists and check behavior
	replayQuery := `
		mutation ReplayEvent($id: ID!) {
			replayEvent(id: $id) {
				id
				status
			}
		}
	`

	_, errors := executeGraphQLWithErrors(t, apiURL, replayQuery, map[string]any{"id": eventID})

	// Should get an error since the event is DELIVERED (not DEAD/FAILED)
	if len(errors) == 0 {
		t.Log("Replay succeeded (event might have been in failed state)")
	} else {
		expectedError := false
		for _, err := range errors {
			if err == "can only replay dead or failed events" {
				expectedError = true
				break
			}
		}
		if expectedError {
			t.Log("Correctly prevented replay of delivered event")
		} else {
			t.Logf("Replay errors: %v", errors)
		}
	}
}

// Integration test helper to clean up test data
// nolint:unused // kept for manual cleanup during development
func cleanupTestData(t *testing.T, dbURL, redisURL string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Clean PostgreSQL
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Logf("Could not connect to PostgreSQL for cleanup: %v", err)
		return
	}
	defer pool.Close()

	_, _ = pool.Exec(ctx, "DELETE FROM delivery_attempts WHERE event_id IN (SELECT id FROM events WHERE created_at > NOW() - INTERVAL '1 hour')")
	_, _ = pool.Exec(ctx, "DELETE FROM events WHERE created_at > NOW() - INTERVAL '1 hour'")

	// Clean Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})
	defer func() { _ = rdb.Close() }()

	_ = rdb.FlushDB(ctx)
}
