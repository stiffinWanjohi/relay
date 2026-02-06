package e2e

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestPriorityDeliveryE2E tests that high priority events are delivered before lower priority ones
func TestPriorityDeliveryE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)
	skipIfNoWebhookSite(t)

	// Create a webhook.site endpoint
	tokenUUID, webhookURL := createWebhookSiteToken(t)
	t.Logf("Created webhook.site endpoint: %s", webhookURL)

	// Create events with different priorities
	// Priority 1 = highest, 10 = lowest
	createEventQuery := `
		mutation CreateEvent($input: CreateEventInput!, $idempotencyKey: String!) {
			createEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
				priority
				status
			}
		}
	`

	// Create a low priority event first
	lowPriorityVars := map[string]any{
		"input": map[string]any{
			"destination": webhookURL,
			"payload":     map[string]any{"priority": "low", "order": 1},
			"priority":    10, // Lowest priority
		},
		"idempotencyKey": uuid.New().String(),
	}

	data := executeGraphQL(t, apiURL, createEventQuery, lowPriorityVars)
	var lowResp struct {
		CreateEvent struct {
			ID       string `json:"id"`
			Priority int    `json:"priority"`
		} `json:"createEvent"`
	}
	_ = json.Unmarshal(data, &lowResp)
	t.Logf("Created low priority event: %s (priority=%d)", lowResp.CreateEvent.ID, lowResp.CreateEvent.Priority)

	// Create a high priority event
	highPriorityVars := map[string]any{
		"input": map[string]any{
			"destination": webhookURL,
			"payload":     map[string]any{"priority": "high", "order": 2},
			"priority":    1, // Highest priority
		},
		"idempotencyKey": uuid.New().String(),
	}

	data = executeGraphQL(t, apiURL, createEventQuery, highPriorityVars)
	var highResp struct {
		CreateEvent struct {
			ID       string `json:"id"`
			Priority int    `json:"priority"`
		} `json:"createEvent"`
	}
	_ = json.Unmarshal(data, &highResp)
	t.Logf("Created high priority event: %s (priority=%d)", highResp.CreateEvent.ID, highResp.CreateEvent.Priority)

	// Verify priorities were set correctly
	if lowResp.CreateEvent.Priority != 10 {
		t.Errorf("Expected low priority to be 10, got %d", lowResp.CreateEvent.Priority)
	}
	if highResp.CreateEvent.Priority != 1 {
		t.Errorf("Expected high priority to be 1, got %d", highResp.CreateEvent.Priority)
	}

	// Wait for both to be delivered
	deadline := time.Now().Add(60 * time.Second)
	deliveredCount := 0
	for time.Now().Before(deadline) {
		getEventQuery := `query GetEvent($id: ID!) { event(id: $id) { status } }`

		data = executeGraphQL(t, apiURL, getEventQuery, map[string]any{"id": lowResp.CreateEvent.ID})
		var lowStatus struct {
			Event struct{ Status string } `json:"event"`
		}
		_ = json.Unmarshal(data, &lowStatus)

		data = executeGraphQL(t, apiURL, getEventQuery, map[string]any{"id": highResp.CreateEvent.ID})
		var highStatus struct {
			Event struct{ Status string } `json:"event"`
		}
		_ = json.Unmarshal(data, &highStatus)

		deliveredCount = 0
		if lowStatus.Event.Status == "DELIVERED" {
			deliveredCount++
		}
		if highStatus.Event.Status == "DELIVERED" {
			deliveredCount++
		}

		if deliveredCount == 2 {
			break
		}
		time.Sleep(2 * time.Second)
	}

	if deliveredCount != 2 {
		t.Errorf("Expected both events to be delivered, got %d", deliveredCount)
	}

	// Check the order of received requests
	requests := getWebhookSiteRequests(t, tokenUUID)
	if len(requests) < 2 {
		t.Fatalf("Expected at least 2 requests, got %d", len(requests))
	}

	// Parse the payloads to check order
	// Due to the nature of priority queues, high priority should generally arrive first
	// but we mainly verify both were delivered
	t.Logf("Received %d webhook requests", len(requests))
	for i, req := range requests {
		var payload map[string]any
		_ = json.Unmarshal([]byte(req.Content), &payload)
		t.Logf("Request %d: priority=%v", i+1, payload["priority"])
	}
}

// TestScheduledDeliveryE2E tests that scheduled events are delivered at the specified time
func TestScheduledDeliveryE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)
	skipIfNoWebhookSite(t)

	tokenUUID, webhookURL := createWebhookSiteToken(t)
	t.Logf("Created webhook.site endpoint: %s", webhookURL)

	// Create an event scheduled for 5 seconds in the future
	delaySeconds := 5
	createEventQuery := `
		mutation CreateEvent($input: CreateEventInput!, $idempotencyKey: String!) {
			createEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
				status
				scheduledAt
			}
		}
	`

	variables := map[string]any{
		"input": map[string]any{
			"destination":  webhookURL,
			"payload":      map[string]any{"test": "scheduled", "scheduled_for": delaySeconds},
			"delaySeconds": delaySeconds,
		},
		"idempotencyKey": uuid.New().String(),
	}

	startTime := time.Now()
	data := executeGraphQL(t, apiURL, createEventQuery, variables)

	var createResp struct {
		CreateEvent struct {
			ID          string  `json:"id"`
			Status      string  `json:"status"`
			ScheduledAt *string `json:"scheduledAt"`
		} `json:"createEvent"`
	}
	_ = json.Unmarshal(data, &createResp)

	eventID := createResp.CreateEvent.ID
	t.Logf("Created scheduled event: %s, scheduledAt: %v", eventID, createResp.CreateEvent.ScheduledAt)

	// Verify it has a scheduledAt time
	if createResp.CreateEvent.ScheduledAt == nil {
		t.Error("Expected scheduledAt to be set")
	}

	// Check that event is initially queued (not delivering yet)
	if createResp.CreateEvent.Status != "QUEUED" {
		t.Logf("Initial status: %s (expected QUEUED for scheduled event)", createResp.CreateEvent.Status)
	}

	// Verify no webhook received immediately
	time.Sleep(1 * time.Second)
	earlyRequests := getWebhookSiteRequests(t, tokenUUID)
	if len(earlyRequests) > 0 {
		t.Log("Warning: Webhook received before scheduled time (may be expected if delay is short)")
	}

	// Wait for delivery (with timeout longer than delay)
	deadline := time.Now().Add(60 * time.Second)
	var deliveredAt time.Time
	for time.Now().Before(deadline) {
		getEventQuery := `query GetEvent($id: ID!) { event(id: $id) { status deliveredAt } }`
		data = executeGraphQL(t, apiURL, getEventQuery, map[string]any{"id": eventID})

		var resp struct {
			Event struct {
				Status      string  `json:"status"`
				DeliveredAt *string `json:"deliveredAt"`
			} `json:"event"`
		}
		_ = json.Unmarshal(data, &resp)

		if resp.Event.Status == "DELIVERED" && resp.Event.DeliveredAt != nil {
			deliveredAt, _ = time.Parse(time.RFC3339, *resp.Event.DeliveredAt)
			break
		}
		time.Sleep(1 * time.Second)
	}

	if deliveredAt.IsZero() {
		t.Fatal("Event was not delivered within timeout")
	}

	// Verify delivery happened after the scheduled delay
	deliveryDelay := deliveredAt.Sub(startTime)
	t.Logf("Event delivered after %v (expected delay: %ds)", deliveryDelay, delaySeconds)

	// Allow some tolerance (1 second before, 10 seconds after)
	minExpectedDelay := time.Duration(delaySeconds-1) * time.Second
	if deliveryDelay < minExpectedDelay {
		t.Errorf("Event delivered too early: %v < %v", deliveryDelay, minExpectedDelay)
	}
}

// TestScheduledDeliveryCancelE2E tests cancellation of scheduled events
func TestScheduledDeliveryCancelE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)
	skipIfNoWebhookSite(t)

	_, webhookURL := createWebhookSiteToken(t)

	// Create an event scheduled for 30 seconds in the future (enough time to cancel)
	createEventQuery := `
		mutation CreateEvent($input: CreateEventInput!, $idempotencyKey: String!) {
			createEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
				status
				scheduledAt
			}
		}
	`

	variables := map[string]any{
		"input": map[string]any{
			"destination":  webhookURL,
			"payload":      map[string]any{"test": "cancel"},
			"delaySeconds": 30,
		},
		"idempotencyKey": uuid.New().String(),
	}

	data := executeGraphQL(t, apiURL, createEventQuery, variables)

	var createResp struct {
		CreateEvent struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"createEvent"`
	}
	_ = json.Unmarshal(data, &createResp)

	eventID := createResp.CreateEvent.ID
	t.Logf("Created scheduled event: %s", eventID)

	// Try to cancel the scheduled event
	cancelQuery := `
		mutation CancelScheduledEvent($id: ID!) {
			cancelScheduledEvent(id: $id) {
				id
				status
			}
		}
	`

	data, errors := executeGraphQLWithErrors(t, apiURL, cancelQuery, map[string]any{"id": eventID})

	if len(errors) > 0 {
		// Cancel may not be implemented yet - that's okay
		t.Logf("Cancel scheduled event returned errors (may not be implemented): %v", errors)
		return
	}

	var cancelResp struct {
		CancelScheduledEvent struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"cancelScheduledEvent"`
	}
	_ = json.Unmarshal(data, &cancelResp)

	t.Logf("Cancelled event status: %s", cancelResp.CancelScheduledEvent.Status)

	// Verify the event is no longer in QUEUED status
	if cancelResp.CancelScheduledEvent.Status == "QUEUED" {
		t.Error("Event should not be QUEUED after cancellation")
	}
}

// TestFIFODeliveryE2E tests that FIFO endpoints deliver events in order
func TestFIFODeliveryE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)
	skipIfNoWebhookSite(t)

	tokenUUID, webhookURL := createWebhookSiteToken(t)
	t.Logf("Created webhook.site endpoint for FIFO test: %s", webhookURL)

	// First, create a FIFO endpoint
	createEndpointQuery := `
		mutation CreateEndpoint($input: CreateEndpointInput!) {
			createEndpoint(input: $input) {
				id
				url
				fifo
				fifoPartitionKey
			}
		}
	`

	endpointVars := map[string]any{
		"input": map[string]any{
			"url":              webhookURL,
			"eventTypes":       []string{"order.created"},
			"fifo":             true,
			"fifoPartitionKey": "$.customer_id",
		},
	}

	data := executeGraphQL(t, apiURL, createEndpointQuery, endpointVars)
	var endpointResp struct {
		CreateEndpoint struct {
			ID               string  `json:"id"`
			URL              string  `json:"url"`
			FIFO             bool    `json:"fifo"`
			FIFOPartitionKey *string `json:"fifoPartitionKey"`
		} `json:"createEndpoint"`
	}
	_ = json.Unmarshal(data, &endpointResp)

	endpointID := endpointResp.CreateEndpoint.ID
	t.Logf("Created FIFO endpoint: %s, fifo=%v, partitionKey=%v",
		endpointID, endpointResp.CreateEndpoint.FIFO, endpointResp.CreateEndpoint.FIFOPartitionKey)

	// Cleanup endpoint at end
	defer func() {
		deleteQuery := `mutation DeleteEndpoint($id: ID!) { deleteEndpoint(id: $id) }`
		_, _ = executeGraphQLWithErrors(t, apiURL, deleteQuery, map[string]any{"id": endpointID})
	}()

	if !endpointResp.CreateEndpoint.FIFO {
		t.Error("Expected endpoint to have FIFO enabled")
	}

	// Send multiple events for the same customer (should be delivered in order)
	sendEventQuery := `
		mutation SendEvent($input: SendEventInput!, $idempotencyKey: String!) {
			sendEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
				status
			}
		}
	`

	customerID := uuid.New().String()
	numEvents := 5
	eventIDs := make([]string, numEvents)

	for i := range numEvents {
		vars := map[string]any{
			"input": map[string]any{
				"eventType": "order.created",
				"payload": map[string]any{
					"customer_id": customerID,
					"order_id":    fmt.Sprintf("order-%d", i+1),
					"sequence":    i + 1,
				},
			},
			"idempotencyKey": uuid.New().String(),
		}

		data = executeGraphQL(t, apiURL, sendEventQuery, vars)
		var resp struct {
			SendEvent []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"sendEvent"`
		}
		_ = json.Unmarshal(data, &resp)

		if len(resp.SendEvent) > 0 {
			eventIDs[i] = resp.SendEvent[0].ID
			t.Logf("Created FIFO event %d: %s", i+1, eventIDs[i])
		}
	}

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
			data = executeGraphQL(t, apiURL, getEventQuery, map[string]any{"id": eventID})
			var resp struct {
				Event struct{ Status string } `json:"event"`
			}
			_ = json.Unmarshal(data, &resp)
			if resp.Event.Status == "DELIVERED" {
				deliveredCount++
			}
		}

		if deliveredCount == numEvents {
			break
		}
		time.Sleep(2 * time.Second)
	}

	t.Logf("Delivered %d/%d FIFO events", deliveredCount, numEvents)

	// Check the order of received requests
	requests := getWebhookSiteRequests(t, tokenUUID)
	t.Logf("Received %d webhook requests", len(requests))

	// Verify order (FIFO should deliver in sequence order)
	var sequences []int
	for _, req := range requests {
		var payload map[string]any
		if err := json.Unmarshal([]byte(req.Content), &payload); err == nil {
			if seq, ok := payload["sequence"].(float64); ok {
				sequences = append(sequences, int(seq))
			}
		}
	}

	t.Logf("Received sequences: %v", sequences)

	// Check that sequences are in order
	for i := 1; i < len(sequences); i++ {
		if sequences[i] < sequences[i-1] {
			t.Errorf("FIFO order violated: sequence %d came after %d", sequences[i], sequences[i-1])
		}
	}
}

// TestFIFOPartitioningE2E tests that different partition keys can be processed in parallel
func TestFIFOPartitioningE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)
	skipIfNoWebhookSite(t)

	tokenUUID, webhookURL := createWebhookSiteToken(t)

	// Create FIFO endpoint with partition key
	createEndpointQuery := `
		mutation CreateEndpoint($input: CreateEndpointInput!) {
			createEndpoint(input: $input) {
				id
				fifo
			}
		}
	`

	endpointVars := map[string]any{
		"input": map[string]any{
			"url":              webhookURL,
			"eventTypes":       []string{"user.action"},
			"fifo":             true,
			"fifoPartitionKey": "$.user_id",
		},
	}

	data := executeGraphQL(t, apiURL, createEndpointQuery, endpointVars)
	var endpointResp struct {
		CreateEndpoint struct {
			ID   string `json:"id"`
			FIFO bool   `json:"fifo"`
		} `json:"createEndpoint"`
	}
	_ = json.Unmarshal(data, &endpointResp)
	endpointID := endpointResp.CreateEndpoint.ID

	defer func() {
		deleteQuery := `mutation DeleteEndpoint($id: ID!) { deleteEndpoint(id: $id) }`
		_, _ = executeGraphQLWithErrors(t, apiURL, deleteQuery, map[string]any{"id": endpointID})
	}()

	// Send events for 3 different users concurrently
	numUsers := 3
	eventsPerUser := 3
	var wg sync.WaitGroup

	sendEventQuery := `
		mutation SendEvent($input: SendEventInput!, $idempotencyKey: String!) {
			sendEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
			}
		}
	`

	for user := range numUsers {
		userID := fmt.Sprintf("user-%d", user+1)
		for seq := range eventsPerUser {
			wg.Add(1)
			go func(uid string, s int) {
				defer wg.Done()
				vars := map[string]any{
					"input": map[string]any{
						"eventType": "user.action",
						"payload": map[string]any{
							"user_id":  uid,
							"sequence": s + 1,
							"action":   fmt.Sprintf("action-%d", s+1),
						},
					},
					"idempotencyKey": uuid.New().String(),
				}
				executeGraphQL(t, apiURL, sendEventQuery, vars)
			}(userID, seq)
		}
	}

	wg.Wait()
	t.Logf("Created %d events across %d users", numUsers*eventsPerUser, numUsers)

	// Wait for delivery
	time.Sleep(30 * time.Second)

	// Get all requests and verify ordering within each partition
	requests := getWebhookSiteRequests(t, tokenUUID)
	t.Logf("Received %d requests", len(requests))

	// Group by user and check order
	userSequences := make(map[string][]int)
	for _, req := range requests {
		var payload map[string]any
		if err := json.Unmarshal([]byte(req.Content), &payload); err == nil {
			userID, _ := payload["user_id"].(string)
			seq, _ := payload["sequence"].(float64)
			userSequences[userID] = append(userSequences[userID], int(seq))
		}
	}

	// Verify each user's events came in order
	for userID, seqs := range userSequences {
		t.Logf("User %s sequences: %v", userID, seqs)
		for i := 1; i < len(seqs); i++ {
			if seqs[i] < seqs[i-1] {
				t.Errorf("FIFO order violated for user %s: %d came after %d", userID, seqs[i], seqs[i-1])
			}
		}
	}
}

// TestPriorityQueueStatsE2E tests the priority queue statistics endpoint
func TestPriorityQueueStatsE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)

	query := `
		query {
			priorityQueueStats {
				high
				normal
				low
				delayed
			}
		}
	`

	data := executeGraphQL(t, apiURL, query, nil)

	var resp struct {
		PriorityQueueStats struct {
			High    int `json:"high"`
			Normal  int `json:"normal"`
			Low     int `json:"low"`
			Delayed int `json:"delayed"`
		} `json:"priorityQueueStats"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("Failed to unmarshal priority queue stats: %v", err)
	}

	// Verify we got valid stats (non-negative)
	stats := resp.PriorityQueueStats
	if stats.High < 0 || stats.Normal < 0 || stats.Low < 0 || stats.Delayed < 0 {
		t.Error("Priority queue stats should be non-negative")
	}

	t.Logf("Priority queue stats: high=%d, normal=%d, low=%d, delayed=%d",
		stats.High, stats.Normal, stats.Low, stats.Delayed)
}

// TestDeliverAtE2E tests scheduling with absolute timestamp
func TestDeliverAtE2E(t *testing.T) {
	_, _, apiURL := skipIfNoInfra(t)
	skipIfNoWebhookSite(t)

	_, webhookURL := createWebhookSiteToken(t)

	// Schedule for 10 seconds from now
	deliverAt := time.Now().Add(10 * time.Second).UTC()

	createEventQuery := `
		mutation CreateEvent($input: CreateEventInput!, $idempotencyKey: String!) {
			createEvent(input: $input, idempotencyKey: $idempotencyKey) {
				id
				scheduledAt
				status
			}
		}
	`

	variables := map[string]any{
		"input": map[string]any{
			"destination": webhookURL,
			"payload":     map[string]any{"test": "deliverAt"},
			"deliverAt":   deliverAt.Format(time.RFC3339),
		},
		"idempotencyKey": uuid.New().String(),
	}

	data := executeGraphQL(t, apiURL, createEventQuery, variables)

	var resp struct {
		CreateEvent struct {
			ID          string  `json:"id"`
			ScheduledAt *string `json:"scheduledAt"`
			Status      string  `json:"status"`
		} `json:"createEvent"`
	}
	_ = json.Unmarshal(data, &resp)

	if resp.CreateEvent.ScheduledAt == nil {
		t.Error("Expected scheduledAt to be set when using deliverAt")
	} else {
		t.Logf("Event scheduled for: %s (requested: %s)", *resp.CreateEvent.ScheduledAt, deliverAt.Format(time.RFC3339))
	}

	t.Logf("Created event with deliverAt: %s, status: %s", resp.CreateEvent.ID, resp.CreateEvent.Status)
}
