# Relay TypeScript SDK

Official TypeScript/JavaScript SDK for the [Relay](https://github.com/stiffinWanjohi/relay) webhook delivery service.

## Installation

```bash
npm install @relay/sdk
# or
yarn add @relay/sdk
# or
pnpm add @relay/sdk
```

## Quick Start

```typescript
import { RelayClient } from '@relay/sdk';

const client = new RelayClient('https://your-relay-instance.com', 'your-api-key');

// Send a webhook
const event = await client.createEvent(
  {
    destination: 'https://example.com/webhook',
    payload: { order_id: 123, status: 'completed' },
  },
  'order-123' // idempotency key
);

console.log(`Event created: ${event.id}, status: ${event.status}`);
```

## Usage

### Creating Events

```typescript
// Direct delivery to a URL
const event = await client.createEvent(
  {
    destination: 'https://example.com/webhook',
    payload: { user_id: 456 },
    headers: { 'X-Custom-Header': 'value' },
  },
  'unique-idempotency-key'
);

// Fan-out by event type (delivers to all subscribed endpoints)
const event = await client.createEvent(
  {
    eventType: 'user.created',
    payload: { user_id: 456, email: 'user@example.com' },
  },
  'user-created-456'
);
```

### Managing Events

```typescript
// Get event details with delivery attempts
const event = await client.getEvent('event-uuid');
console.log(`Attempts: ${event.deliveryAttempts.length}`);

// List events with filtering
const { data: events, pagination } = await client.listEvents({
  status: 'failed',
  limit: 20,
});

// Replay a failed event
const replayed = await client.replayEvent('event-uuid');

// Batch retry multiple events
const result = await client.batchRetry({ eventIds: ['id1', 'id2', 'id3'] });
console.log(`Retried: ${result.totalSucceeded}/${result.totalRequested}`);

// Batch retry by status
const result = await client.batchRetry({ status: 'failed', limit: 100 });

// Batch retry by endpoint
const result = await client.batchRetry({ endpointId: 'endpoint-uuid', limit: 50 });
```

### Managing Endpoints

```typescript
// Create an endpoint
const endpoint = await client.createEndpoint({
  url: 'https://your-app.com/webhooks',
  eventTypes: ['order.created', 'order.updated'],
  maxRetries: 5,
  timeoutMs: 30000,
});

// List endpoints
const { data: endpoints } = await client.listEndpoints({ status: 'active' });

// Update an endpoint
await client.updateEndpoint('endpoint-uuid', {
  status: 'paused',
  maxRetries: 10,
});

// Rotate signing secret
const { newSecret } = await client.rotateEndpointSecret('endpoint-uuid');
console.log(`New secret: ${newSecret}`); // Save this securely!

// Clear previous secret (after rotation grace period)
await client.clearPreviousSecret('endpoint-uuid');

// Delete an endpoint
await client.deleteEndpoint('endpoint-uuid');
```

### Stats and Health

```typescript
// Get queue statistics
const stats = await client.getStats();
console.log(`Queued: ${stats.queued}, Delivered: ${stats.delivered}`);

// Health check
const health = await client.healthCheck();
console.log(`Status: ${health.status}`);
```

## Verifying Webhook Signatures

When receiving webhooks, verify the signature to ensure authenticity:

```typescript
import { verifySignature, extractWebhookHeaders } from '@relay/sdk';

// Express.js example
app.post('/webhook', express.raw({ type: 'application/json' }), (req, res) => {
  const { signature, timestamp } = extractWebhookHeaders(req.headers);

  try {
    verifySignature(req.body, signature, timestamp, process.env.WEBHOOK_SECRET!);
    
    // Signature valid - process the webhook
    const payload = JSON.parse(req.body.toString());
    console.log('Received:', payload);
    
    res.status(200).send('OK');
  } catch (error) {
    console.error('Invalid signature:', error);
    res.status(401).send('Invalid signature');
  }
});
```

### Handling Secret Rotation

During secret rotation, verify against both current and previous secrets:

```typescript
import { verifySignatureWithRotation, extractWebhookHeaders } from '@relay/sdk';

app.post('/webhook', express.raw({ type: 'application/json' }), (req, res) => {
  const { signature, timestamp } = extractWebhookHeaders(req.headers);

  try {
    verifySignatureWithRotation(
      req.body,
      signature,
      timestamp,
      [process.env.CURRENT_SECRET!, process.env.PREVIOUS_SECRET!].filter(Boolean)
    );
    
    // Valid - process webhook
    res.status(200).send('OK');
  } catch (error) {
    res.status(401).send('Invalid signature');
  }
});
```

## Error Handling

```typescript
import { RelayClient, APIError } from '@relay/sdk';

try {
  await client.getEvent('non-existent-id');
} catch (error) {
  if (error instanceof APIError) {
    if (error.isNotFound()) {
      console.log('Event not found');
    } else if (error.isUnauthorized()) {
      console.log('Invalid API key');
    } else if (error.isRateLimited()) {
      console.log('Rate limited, retry later');
    } else {
      console.log(`API error: ${error.message} (${error.statusCode})`);
    }
  }
}
```

### Error Types

| Error | Description |
|-------|-------------|
| `APIError` | HTTP error from the API |
| `InvalidSignatureError` | Webhook signature mismatch |
| `MissingSignatureError` | Missing signature header |
| `InvalidTimestampError` | Timestamp invalid or expired |
| `MissingTimestampError` | Missing timestamp header |

## Configuration

```typescript
const client = new RelayClient('https://relay.example.com', 'api-key', {
  timeout: 60000, // Request timeout in ms (default: 30000)
  headers: {
    'X-Custom-Header': 'value', // Additional headers for all requests
  },
});
```

## TypeScript Support

Full TypeScript support with exported types:

```typescript
import type {
  Event,
  EventStatus,
  Endpoint,
  EndpointStatus,
  CreateEventRequest,
  CreateEndpointRequest,
  BatchRetryResult,
} from '@relay/sdk';
```

## License

MIT
