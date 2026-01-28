import { createHmac, timingSafeEqual } from 'crypto';
import {
  InvalidSignatureError,
  InvalidTimestampError,
  MissingSignatureError,
  MissingTimestampError,
} from './errors';

const DEFAULT_TOLERANCE_SECONDS = 300; // 5 minutes

export interface VerifyOptions {
  toleranceSeconds?: number;
  currentTimestamp?: number;
}

export interface WebhookHeaders {
  signature: string;
  timestamp: string;
}

/**
 * Verify a webhook signature from Relay.
 *
 * @param payload - The raw request body as a string or Buffer
 * @param signature - The X-Relay-Signature header value
 * @param timestamp - The X-Relay-Timestamp header value
 * @param secret - Your webhook signing secret
 * @param options - Optional verification options
 * @throws {MissingSignatureError} If signature is missing
 * @throws {MissingTimestampError} If timestamp is missing
 * @throws {InvalidTimestampError} If timestamp is invalid or too old
 * @throws {InvalidSignatureError} If signature doesn't match
 */
export function verifySignature(
  payload: string | Buffer,
  signature: string,
  timestamp: string,
  secret: string,
  options: VerifyOptions = {}
): void {
  if (!signature) {
    throw new MissingSignatureError();
  }
  if (!timestamp) {
    throw new MissingTimestampError();
  }

  const tolerance = options.toleranceSeconds ?? DEFAULT_TOLERANCE_SECONDS;
  const now = options.currentTimestamp ?? Math.floor(Date.now() / 1000);

  // Parse timestamp
  const ts = parseInt(timestamp, 10);
  if (isNaN(ts)) {
    throw new InvalidTimestampError('Timestamp is not a valid number');
  }

  // Check timestamp is within tolerance
  if (Math.abs(now - ts) > tolerance) {
    throw new InvalidTimestampError(
      `Timestamp is outside the tolerance window (${tolerance}s)`
    );
  }

  // Compute expected signature
  const payloadStr = typeof payload === 'string' ? payload : payload.toString('utf8');
  const signedPayload = `${timestamp}.${payloadStr}`;
  const expectedSignature = computeSignature(signedPayload, secret);

  // Compare signatures using timing-safe comparison
  if (!secureCompare(signature, expectedSignature)) {
    throw new InvalidSignatureError();
  }
}

/**
 * Verify a webhook using multiple secrets (for secret rotation).
 * Returns true if ANY secret validates the signature.
 *
 * @param payload - The raw request body
 * @param signature - The X-Relay-Signature header value
 * @param timestamp - The X-Relay-Timestamp header value
 * @param secrets - Array of secrets to try (current + previous)
 * @param options - Optional verification options
 * @throws {MissingSignatureError} If signature is missing
 * @throws {MissingTimestampError} If timestamp is missing
 * @throws {InvalidTimestampError} If timestamp is invalid or too old
 * @throws {InvalidSignatureError} If no secret validates the signature
 */
export function verifySignatureWithRotation(
  payload: string | Buffer,
  signature: string,
  timestamp: string,
  secrets: string[],
  options: VerifyOptions = {}
): void {
  if (!signature) {
    throw new MissingSignatureError();
  }
  if (!timestamp) {
    throw new MissingTimestampError();
  }
  if (!secrets.length) {
    throw new InvalidSignatureError();
  }

  const tolerance = options.toleranceSeconds ?? DEFAULT_TOLERANCE_SECONDS;
  const now = options.currentTimestamp ?? Math.floor(Date.now() / 1000);

  // Parse and validate timestamp
  const ts = parseInt(timestamp, 10);
  if (isNaN(ts)) {
    throw new InvalidTimestampError('Timestamp is not a valid number');
  }
  if (Math.abs(now - ts) > tolerance) {
    throw new InvalidTimestampError(
      `Timestamp is outside the tolerance window (${tolerance}s)`
    );
  }

  // Try each secret
  const payloadStr = typeof payload === 'string' ? payload : payload.toString('utf8');
  const signedPayload = `${timestamp}.${payloadStr}`;

  for (const secret of secrets) {
    const expectedSignature = computeSignature(signedPayload, secret);
    if (secureCompare(signature, expectedSignature)) {
      return; // Valid signature found
    }
  }

  throw new InvalidSignatureError();
}

/**
 * Compute an HMAC-SHA256 signature in the Relay format.
 *
 * @param payload - The signed payload (timestamp.body)
 * @param secret - The signing secret
 * @returns The signature in "v1=<hex>" format
 */
export function computeSignature(payload: string, secret: string): string {
  const hmac = createHmac('sha256', secret);
  hmac.update(payload);
  return `v1=${hmac.digest('hex')}`;
}

/**
 * Extract webhook headers from a request headers object.
 *
 * @param headers - Headers object (works with various frameworks)
 * @returns The signature and timestamp headers
 */
export function extractWebhookHeaders(
  headers: Record<string, string | string[] | undefined> | Headers
): WebhookHeaders {
  const getHeader = (name: string): string => {
    if (headers instanceof Headers) {
      return headers.get(name) ?? '';
    }
    const value = headers[name] ?? headers[name.toLowerCase()];
    return Array.isArray(value) ? value[0] : value ?? '';
  };

  return {
    signature: getHeader('X-Relay-Signature'),
    timestamp: getHeader('X-Relay-Timestamp'),
  };
}

// Timing-safe string comparison
function secureCompare(a: string, b: string): boolean {
  try {
    const bufA = Buffer.from(a);
    const bufB = Buffer.from(b);
    if (bufA.length !== bufB.length) {
      return false;
    }
    return timingSafeEqual(bufA, bufB);
  } catch {
    return false;
  }
}
