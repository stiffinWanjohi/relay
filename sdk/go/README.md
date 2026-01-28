# Relay Go SDK

Official Go client library for the Relay webhook delivery service.

## Installation

```bash
go get github.com/stiffinWanjohi/relay/sdk/go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    relay "github.com/stiffinWanjohi/relay/sdk/go"
)

func main() {
    // Create client
    client := relay.NewClient("http://localhost:8080", "rly_your_api_key")

    // Send an event
    event, err := client.CreateEvent(context.Background(), "order-123", relay.CreateEventRequest{
        Destination: "https://example.com/webhook",
        EventType:   "order.created",
        Payload:     map[string]any{"order_id": 123, "amount": 99.99},
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Event created: %s (status: %s)\n", event.ID, event.Status)
}
```

## Events

### Create Event

```go
event, err := client.CreateEvent(ctx, "idempotency-key", relay.CreateEventRequest{
    Destination: "https://example.com/webhook",
    EventType:   "order.created",
    Payload:     map[string]any{"order_id": 123},
    Headers:     map[string]string{"X-Custom": "value"},
    MaxAttempts: intPtr(10),  // Optional
})
```

### Get Event

```go
event, err := client.GetEvent(ctx, "evt_123")
fmt.Printf("Status: %s, Attempts: %d\n", event.Status, event.Attempts)

// Includes delivery attempts
for _, attempt := range event.DeliveryAttempts {
    fmt.Printf("  Attempt %d: %d (%dms)\n", 
        attempt.AttemptNumber, *attempt.StatusCode, attempt.DurationMs)
}
```

### List Events

```go
status := relay.EventStatusFailed
list, err := client.ListEvents(ctx, relay.ListEventsOptions{
    Status: &status,
    Limit:  50,
})
for _, e := range list.Data {
    fmt.Println(e.ID, e.Status)
}
```

### Replay Event

```go
event, err := client.ReplayEvent(ctx, "evt_123")
```

### Batch Retry

```go
// Retry specific events
result, err := client.BatchRetryByIDs(ctx, []string{"evt_1", "evt_2", "evt_3"})
fmt.Printf("Succeeded: %d, Failed: %d\n", 
    len(result.Succeeded), len(result.Failed))

// Retry all failed events
result, err := client.BatchRetryByStatus(ctx, relay.EventStatusFailed, 100)

// Retry failed events for an endpoint
result, err := client.BatchRetryByEndpoint(ctx, "ep_123", relay.EventStatusFailed, 50)
```

## Endpoints

### Create Endpoint

```go
endpoint, err := client.CreateEndpoint(ctx, relay.CreateEndpointRequest{
    URL:              "https://example.com/webhook",
    EventTypes:       []string{"order.*", "payment.completed"},
    Description:      "Order notifications",
    MaxRetries:       10,
    TimeoutMs:        5000,
    RateLimitPerSec:  100,
    CircuitThreshold: 5,
})
```

### Update Endpoint

```go
newURL := "https://new.example.com/webhook"
newStatus := relay.EndpointStatusPaused

endpoint, err := client.UpdateEndpoint(ctx, "ep_123", relay.UpdateEndpointRequest{
    URL:    &newURL,
    Status: &newStatus,
})
```

### Delete Endpoint

```go
err := client.DeleteEndpoint(ctx, "ep_123")
```

## Webhook Verification

Verify incoming webhook signatures:

```go
// Single secret
verifier := relay.NewWebhookVerifier("whsec_your_secret")

// During secret rotation (accepts either)
verifier := relay.NewWebhookVerifierWithSecrets([]string{
    "whsec_new_secret",
    "whsec_old_secret",
})

// Verify
err := verifier.Verify(
    payload,                           // []byte - raw request body
    r.Header.Get("X-Relay-Signature"), // signature header
    r.Header.Get("X-Relay-Timestamp"), // timestamp header
)
if err != nil {
    http.Error(w, "Invalid signature", http.StatusUnauthorized)
    return
}
```

### Custom Tolerance

```go
verifier := relay.NewWebhookVerifier("whsec_secret").
    WithTolerance(10 * time.Minute)  // Default is 5 minutes
```

### Manual Verification

```go
err := relay.VerifySignature(payload, signature, timestamp, secret)

// With custom tolerance
err := relay.VerifySignatureWithTolerance(payload, signature, timestamp, secret, 10*time.Minute)

// With multiple secrets
err := relay.VerifySignatureWithSecrets(payload, signature, timestamp, secrets, 5*time.Minute)
```

### Computing Signatures

```go
signature := relay.ComputeSignature(payload, timestamp, secret)
```

## Configuration

### Custom HTTP Client

```go
client := relay.NewClient(baseURL, apiKey, 
    relay.WithHTTPClient(&http.Client{
        Timeout: 60 * time.Second,
        Transport: &http.Transport{
            MaxIdleConns: 100,
        },
    }),
)
```

### Custom Timeout

```go
client := relay.NewClient(baseURL, apiKey, 
    relay.WithTimeout(10 * time.Second),
)
```

## Error Handling

```go
event, err := client.GetEvent(ctx, "nonexistent")
if err != nil {
    if apiErr, ok := err.(*relay.APIError); ok {
        fmt.Printf("API Error: %s (code: %s, status: %d)\n", 
            apiErr.Message, apiErr.Code, apiErr.StatusCode)
    } else {
        fmt.Printf("Network error: %v\n", err)
    }
}
```

## Constants

```go
// Headers
relay.SignatureHeader  // "X-Relay-Signature"
relay.TimestampHeader  // "X-Relay-Timestamp"

// Event statuses
relay.EventStatusQueued
relay.EventStatusDelivering
relay.EventStatusDelivered
relay.EventStatusFailed
relay.EventStatusDead

// Endpoint statuses
relay.EndpointStatusActive
relay.EndpointStatusPaused
relay.EndpointStatusDisabled
```
