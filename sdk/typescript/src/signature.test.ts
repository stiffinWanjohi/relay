import { describe, it, expect } from "vitest";
import {
  verifySignature,
  verifySignatureWithRotation,
  computeSignature,
  extractWebhookHeaders,
} from "./signature";
import {
  InvalidSignatureError,
  InvalidTimestampError,
  MissingSignatureError,
  MissingTimestampError,
} from "./errors";

describe("computeSignature", () => {
  it("should compute HMAC-SHA256 signature", () => {
    const payload = '1234567890.{"test":true}';
    const secret = "whsec_test_secret";
    const signature = computeSignature(payload, secret);

    expect(signature).toMatch(/^v1=[a-f0-9]{64}$/);
  });

  it("should produce consistent signatures", () => {
    const payload = '1234567890.{"order_id":123}';
    const secret = "my_secret";

    const sig1 = computeSignature(payload, secret);
    const sig2 = computeSignature(payload, secret);

    expect(sig1).toBe(sig2);
  });

  it("should produce different signatures for different secrets", () => {
    const payload = '1234567890.{"test":true}';

    const sig1 = computeSignature(payload, "secret1");
    const sig2 = computeSignature(payload, "secret2");

    expect(sig1).not.toBe(sig2);
  });
});

describe("verifySignature", () => {
  const secret = "whsec_test_secret";
  const payload = '{"order_id":123}';

  it("should verify a valid signature", () => {
    const timestamp = String(Math.floor(Date.now() / 1000));
    const signedPayload = `${timestamp}.${payload}`;
    const signature = computeSignature(signedPayload, secret);

    expect(() => {
      verifySignature(payload, signature, timestamp, secret);
    }).not.toThrow();
  });

  it("should throw MissingSignatureError when signature is empty", () => {
    const timestamp = String(Math.floor(Date.now() / 1000));

    expect(() => {
      verifySignature(payload, "", timestamp, secret);
    }).toThrow(MissingSignatureError);
  });

  it("should throw MissingTimestampError when timestamp is empty", () => {
    expect(() => {
      verifySignature(payload, "v1=abc123", "", secret);
    }).toThrow(MissingTimestampError);
  });

  it("should throw InvalidTimestampError for non-numeric timestamp", () => {
    expect(() => {
      verifySignature(payload, "v1=abc123", "not-a-number", secret);
    }).toThrow(InvalidTimestampError);
  });

  it("should throw InvalidTimestampError for expired timestamp", () => {
    const oldTimestamp = String(Math.floor(Date.now() / 1000) - 600); // 10 minutes ago
    const signedPayload = `${oldTimestamp}.${payload}`;
    const signature = computeSignature(signedPayload, secret);

    expect(() => {
      verifySignature(payload, signature, oldTimestamp, secret);
    }).toThrow(InvalidTimestampError);
  });

  it("should throw InvalidSignatureError for wrong signature", () => {
    const timestamp = String(Math.floor(Date.now() / 1000));

    expect(() => {
      verifySignature(payload, "v1=wrong_signature", timestamp, secret);
    }).toThrow(InvalidSignatureError);
  });

  it("should throw InvalidSignatureError for wrong secret", () => {
    const timestamp = String(Math.floor(Date.now() / 1000));
    const signedPayload = `${timestamp}.${payload}`;
    const signature = computeSignature(signedPayload, secret);

    expect(() => {
      verifySignature(payload, signature, timestamp, "wrong_secret");
    }).toThrow(InvalidSignatureError);
  });

  it("should accept custom tolerance", () => {
    const oldTimestamp = String(Math.floor(Date.now() / 1000) - 400); // 6+ minutes ago
    const signedPayload = `${oldTimestamp}.${payload}`;
    const signature = computeSignature(signedPayload, secret);

    // Should fail with default 5 minute tolerance
    expect(() => {
      verifySignature(payload, signature, oldTimestamp, secret);
    }).toThrow(InvalidTimestampError);

    // Should pass with 10 minute tolerance
    expect(() => {
      verifySignature(payload, signature, oldTimestamp, secret, {
        toleranceSeconds: 600,
      });
    }).not.toThrow();
  });

  it("should work with Buffer payload", () => {
    const timestamp = String(Math.floor(Date.now() / 1000));
    const bufferPayload = Buffer.from(payload);
    const signedPayload = `${timestamp}.${payload}`;
    const signature = computeSignature(signedPayload, secret);

    expect(() => {
      verifySignature(bufferPayload, signature, timestamp, secret);
    }).not.toThrow();
  });
});

describe("verifySignatureWithRotation", () => {
  const currentSecret = "current_secret";
  const previousSecret = "previous_secret";
  const payload = '{"test":true}';

  it("should verify with current secret", () => {
    const timestamp = String(Math.floor(Date.now() / 1000));
    const signedPayload = `${timestamp}.${payload}`;
    const signature = computeSignature(signedPayload, currentSecret);

    expect(() => {
      verifySignatureWithRotation(payload, signature, timestamp, [
        currentSecret,
        previousSecret,
      ]);
    }).not.toThrow();
  });

  it("should verify with previous secret", () => {
    const timestamp = String(Math.floor(Date.now() / 1000));
    const signedPayload = `${timestamp}.${payload}`;
    const signature = computeSignature(signedPayload, previousSecret);

    expect(() => {
      verifySignatureWithRotation(payload, signature, timestamp, [
        currentSecret,
        previousSecret,
      ]);
    }).not.toThrow();
  });

  it("should throw when no secret matches", () => {
    const timestamp = String(Math.floor(Date.now() / 1000));
    const signedPayload = `${timestamp}.${payload}`;
    const signature = computeSignature(signedPayload, "unknown_secret");

    expect(() => {
      verifySignatureWithRotation(payload, signature, timestamp, [
        currentSecret,
        previousSecret,
      ]);
    }).toThrow(InvalidSignatureError);
  });

  it("should throw with empty secrets array", () => {
    const timestamp = String(Math.floor(Date.now() / 1000));

    expect(() => {
      verifySignatureWithRotation(payload, "v1=abc", timestamp, []);
    }).toThrow(InvalidSignatureError);
  });
});

describe("extractWebhookHeaders", () => {
  it("should extract headers from plain object", () => {
    const headers = {
      "X-Relay-Signature": "v1=abc123",
      "X-Relay-Timestamp": "1234567890",
    };

    const result = extractWebhookHeaders(headers);

    expect(result.signature).toBe("v1=abc123");
    expect(result.timestamp).toBe("1234567890");
  });

  it("should extract lowercase headers", () => {
    const headers = {
      "x-relay-signature": "v1=abc123",
      "x-relay-timestamp": "1234567890",
    };

    const result = extractWebhookHeaders(headers);

    expect(result.signature).toBe("v1=abc123");
    expect(result.timestamp).toBe("1234567890");
  });

  it("should handle missing headers", () => {
    const headers = {};

    const result = extractWebhookHeaders(headers);

    expect(result.signature).toBe("");
    expect(result.timestamp).toBe("");
  });

  it("should handle Headers object (Web API)", () => {
    const headers = new Headers();
    headers.set("X-Relay-Signature", "v1=abc123");
    headers.set("X-Relay-Timestamp", "1234567890");

    const result = extractWebhookHeaders(headers);

    expect(result.signature).toBe("v1=abc123");
    expect(result.timestamp).toBe("1234567890");
  });
});
