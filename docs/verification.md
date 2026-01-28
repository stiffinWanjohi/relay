# Webhook Signature Verification

Relay signs every outgoing webhook. Always verify signatures to ensure authenticity.

## Headers

| Header | Description |
|--------|-------------|
| `X-Relay-Event-ID` | Unique event ID |
| `X-Relay-Timestamp` | Unix timestamp when sent |
| `X-Relay-Signature` | HMAC-SHA256 signature |

## Signature Format

```
v1=HMAC-SHA256("{timestamp}.{payload}", signing_key)
```

## Verification Steps

1. Extract timestamp and signature from headers
2. Check timestamp is recent (< 5 minutes) to prevent replay attacks
3. Compute expected signature: `HMAC-SHA256("{timestamp}.{raw_body}", secret)`
4. Compare signatures using constant-time comparison

## Examples

### Go

```go
import (
    "crypto/hmac"
    "crypto/sha256"
    "fmt"
    "strconv"
    "time"
)

func VerifyWebhook(payload []byte, timestamp, signature, secret string) error {
    // Check timestamp freshness
    ts, err := strconv.ParseInt(timestamp, 10, 64)
    if err != nil {
        return fmt.Errorf("invalid timestamp")
    }
    if time.Now().Unix()-ts > 300 {
        return fmt.Errorf("timestamp too old")
    }

    // Compute expected signature
    signed := fmt.Sprintf("%s.%s", timestamp, string(payload))
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(signed))
    expected := fmt.Sprintf("v1=%x", mac.Sum(nil))

    // Constant-time comparison
    if !hmac.Equal([]byte(expected), []byte(signature)) {
        return fmt.Errorf("invalid signature")
    }

    return nil
}
```

### Python

```python
import hmac
import hashlib
import time

def verify_webhook(payload: bytes, timestamp: str, signature: str, secret: str) -> bool:
    # Check timestamp freshness
    try:
        ts = int(timestamp)
    except ValueError:
        return False
    
    if abs(time.time() - ts) > 300:
        return False
    
    # Compute expected signature
    signed = f"{timestamp}.{payload.decode()}"
    expected = "v1=" + hmac.new(
        secret.encode(),
        signed.encode(),
        hashlib.sha256
    ).hexdigest()
    
    return hmac.compare_digest(expected, signature)
```

### Node.js

```javascript
const crypto = require('crypto');

function verifyWebhook(payload, timestamp, signature, secret) {
    // Check timestamp freshness
    const ts = parseInt(timestamp, 10);
    if (isNaN(ts) || Math.abs(Date.now() / 1000 - ts) > 300) {
        return false;
    }

    // Compute expected signature
    const signed = `${timestamp}.${payload}`;
    const expected = 'v1=' + crypto
        .createHmac('sha256', secret)
        .update(signed)
        .digest('hex');

    // Constant-time comparison
    return crypto.timingSafeEqual(
        Buffer.from(expected),
        Buffer.from(signature)
    );
}
```

### Ruby

```ruby
require 'openssl'

def verify_webhook(payload, timestamp, signature, secret)
  # Check timestamp freshness
  ts = timestamp.to_i
  return false if (Time.now.to_i - ts).abs > 300

  # Compute expected signature
  signed = "#{timestamp}.#{payload}"
  expected = "v1=" + OpenSSL::HMAC.hexdigest('sha256', secret, signed)

  # Constant-time comparison
  Rack::Utils.secure_compare(expected, signature)
end
```

### PHP

```php
function verifyWebhook(string $payload, string $timestamp, string $signature, string $secret): bool {
    // Check timestamp freshness
    $ts = (int) $timestamp;
    if (abs(time() - $ts) > 300) {
        return false;
    }

    // Compute expected signature
    $signed = "{$timestamp}.{$payload}";
    $expected = 'v1=' . hash_hmac('sha256', $signed, $secret);

    // Constant-time comparison
    return hash_equals($expected, $signature);
}
```

## Framework Examples

### Express.js Middleware

```javascript
const express = require('express');
const app = express();

app.use('/webhook', express.raw({ type: '*/*' }), (req, res, next) => {
    const signature = req.headers['x-relay-signature'];
    const timestamp = req.headers['x-relay-timestamp'];
    
    if (!verifyWebhook(req.body, timestamp, signature, process.env.WEBHOOK_SECRET)) {
        return res.status(401).json({ error: 'Invalid signature' });
    }
    
    next();
});
```

### Flask Decorator

```python
from functools import wraps
from flask import request, jsonify

def verify_relay_signature(f):
    @wraps(f)
    def decorated(*args, **kwargs):
        signature = request.headers.get('X-Relay-Signature')
        timestamp = request.headers.get('X-Relay-Timestamp')
        
        if not verify_webhook(request.data, timestamp, signature, WEBHOOK_SECRET):
            return jsonify({'error': 'Invalid signature'}), 401
        
        return f(*args, **kwargs)
    return decorated

@app.route('/webhook', methods=['POST'])
@verify_relay_signature
def handle_webhook():
    # Process webhook...
```

## Testing

Send a test webhook to verify your implementation:

```bash
# Get a test event ID
EVENT_ID=$(curl -s -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"mutation { createEvent(input:{destination:\"http://localhost:9000/hook\",payload:{test:true}},idempotencyKey:\"test-123\"){id}}"}' \
  | jq -r '.data.createEvent.id')

# Check delivery attempt
curl -s -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d "{\"query\":\"{ event(id:\\\"$EVENT_ID\\\") { deliveryAttempts { statusCode } } }\"}"
```
