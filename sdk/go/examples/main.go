// Example usage of the Relay Go SDK
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	relay "github.com/stiffinWanjohi/relay/sdk/go"
)

func main() {
	// Get configuration from environment
	baseURL := getEnv("RELAY_URL", "http://localhost:8080")
	apiKey := getEnv("RELAY_API_KEY", "")
	if apiKey == "" {
		log.Fatal("RELAY_API_KEY environment variable is required")
	}

	// Create client with custom timeout
	client := relay.NewClient(baseURL, apiKey,
		relay.WithTimeout(10*time.Second),
	)

	ctx := context.Background()

	// Example 1: Create and send an event
	fmt.Println("=== Creating Event ===")
	event, err := client.CreateEvent(ctx, "order-123-v1", relay.CreateEventRequest{
		Destination: "https://webhook.site/your-endpoint",
		EventType:   "order.created",
		Payload: map[string]any{
			"order_id":    "123",
			"customer_id": "456",
			"amount":      99.99,
			"items": []map[string]any{
				{"sku": "ITEM-001", "quantity": 2},
				{"sku": "ITEM-002", "quantity": 1},
			},
		},
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
	})
	if err != nil {
		log.Printf("Failed to create event: %v", err)
	} else {
		fmt.Printf("Event created: ID=%s, Status=%s\n", event.ID, event.Status)
	}

	// Example 2: Get event with delivery attempts
	if event != nil {
		fmt.Println("\n=== Getting Event Details ===")
		eventDetails, err := client.GetEvent(ctx, event.ID)
		if err != nil {
			log.Printf("Failed to get event: %v", err)
		} else {
			fmt.Printf("Event: ID=%s, Status=%s, Attempts=%d\n",
				eventDetails.ID, eventDetails.Status, eventDetails.Attempts)
			for _, attempt := range eventDetails.DeliveryAttempts {
				fmt.Printf("  Attempt %d: Status=%v, Duration=%dms\n",
					attempt.AttemptNumber, attempt.StatusCode, attempt.DurationMs)
			}
		}
	}

	// Example 3: List failed events
	fmt.Println("\n=== Listing Failed Events ===")
	failedStatus := relay.EventStatusFailed
	failedEvents, err := client.ListEvents(ctx, relay.ListEventsOptions{
		Status: &failedStatus,
		Limit:  10,
	})
	if err != nil {
		log.Printf("Failed to list events: %v", err)
	} else {
		fmt.Printf("Found %d failed events\n", len(failedEvents.Data))
		for _, e := range failedEvents.Data {
			fmt.Printf("  - %s: %s (attempts: %d)\n", e.ID, e.Destination, e.Attempts)
		}
	}

	// Example 4: Batch retry failed events
	if failedEvents != nil && len(failedEvents.Data) > 0 {
		fmt.Println("\n=== Batch Retry Failed Events ===")
		ids := make([]string, 0, len(failedEvents.Data))
		for _, e := range failedEvents.Data {
			ids = append(ids, e.ID)
		}

		result, err := client.BatchRetryByIDs(ctx, ids)
		if err != nil {
			log.Printf("Failed to batch retry: %v", err)
		} else {
			fmt.Printf("Retry result: %d succeeded, %d failed\n",
				len(result.Succeeded), len(result.Failed))
		}
	}

	// Example 5: Create an endpoint
	fmt.Println("\n=== Creating Endpoint ===")
	maxRetries := 10
	timeoutMs := 5000
	endpoint, err := client.CreateEndpoint(ctx, relay.CreateEndpointRequest{
		URL:         "https://webhook.site/your-endpoint",
		EventTypes:  []string{"order.*", "payment.completed"},
		Description: "Order notifications endpoint",
		MaxRetries:  &maxRetries,
		TimeoutMs:   &timeoutMs,
	})
	if err != nil {
		log.Printf("Failed to create endpoint: %v", err)
	} else {
		fmt.Printf("Endpoint created: ID=%s, URL=%s\n", endpoint.ID, endpoint.URL)
	}

	// Example 6: Update endpoint
	if endpoint != nil {
		fmt.Println("\n=== Updating Endpoint ===")
		newDesc := "Updated order notifications"
		updated, err := client.UpdateEndpoint(ctx, endpoint.ID, relay.UpdateEndpointRequest{
			Description: &newDesc,
		})
		if err != nil {
			log.Printf("Failed to update endpoint: %v", err)
		} else {
			fmt.Printf("Endpoint updated: Description=%s\n", updated.Description)
		}
	}

	// Example 7: Webhook verification (receiver side)
	fmt.Println("\n=== Webhook Verification Example ===")
	demonstrateWebhookVerification()
}

// demonstrateWebhookVerification shows how to verify incoming webhooks
func demonstrateWebhookVerification() {
	secret := "whsec_your_webhook_secret"

	// Create a verifier (typically done once at startup)
	verifier := relay.NewWebhookVerifier(secret)

	// Simulate an incoming webhook
	payload := []byte(`{"order_id":"123","status":"completed"}`)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	signature := relay.ComputeSignature(payload, timestamp, secret)

	// Verify the webhook
	err := verifier.Verify(payload, signature, timestamp)
	if err != nil {
		fmt.Printf("Webhook verification failed: %v\n", err)
	} else {
		fmt.Println("Webhook verified successfully!")
	}

	// Example with multiple secrets (during rotation)
	fmt.Println("\n=== Secret Rotation Example ===")
	oldSecret := "whsec_old_secret"
	newSecret := "whsec_new_secret"

	rotationVerifier := relay.NewWebhookVerifierWithSecrets([]string{newSecret, oldSecret})

	// Webhook signed with old secret still works
	oldSignature := relay.ComputeSignature(payload, timestamp, oldSecret)
	err = rotationVerifier.Verify(payload, oldSignature, timestamp)
	if err != nil {
		fmt.Printf("Old secret verification failed: %v\n", err)
	} else {
		fmt.Println("Old secret still valid during rotation!")
	}
}

// exampleWebhookHandler shows how to implement a webhook receiver.
// This is not called directly but serves as documentation.
var _ = exampleWebhookHandler

func exampleWebhookHandler(secret string) http.HandlerFunc {
	verifier := relay.NewWebhookVerifier(secret)

	return func(w http.ResponseWriter, r *http.Request) {
		// Read the body
		payload := make([]byte, r.ContentLength)
		_, err := r.Body.Read(payload)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		// Get signature headers
		signature := r.Header.Get(relay.SignatureHeader)
		timestamp := r.Header.Get(relay.TimestampHeader)

		// Verify
		if err := verifier.Verify(payload, signature, timestamp); err != nil {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}

		// Process the webhook...
		fmt.Printf("Received valid webhook: %s\n", string(payload))
		w.WriteHeader(http.StatusOK)
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
