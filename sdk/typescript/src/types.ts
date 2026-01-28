// Event status
export type EventStatus = 'queued' | 'delivering' | 'delivered' | 'failed' | 'dead';

// Endpoint status
export type EndpointStatus = 'active' | 'paused' | 'disabled';

// Event represents a webhook event
export interface Event {
  id: string;
  idempotencyKey: string;
  destination: string;
  eventType?: string;
  payload?: unknown;
  headers?: Record<string, string>;
  status: EventStatus;
  attempts: number;
  maxAttempts: number;
  createdAt: string;
  deliveredAt?: string;
  nextAttemptAt?: string;
}

// DeliveryAttempt represents a single delivery attempt
export interface DeliveryAttempt {
  id: string;
  attemptNumber: number;
  statusCode?: number;
  responseBody?: string;
  error?: string;
  durationMs: number;
  attemptedAt: string;
}

// EventWithAttempts includes an event with its delivery history
export interface EventWithAttempts extends Event {
  deliveryAttempts: DeliveryAttempt[];
}

// Endpoint represents a webhook endpoint
export interface Endpoint {
  id: string;
  url: string;
  description?: string;
  eventTypes: string[];
  status: EndpointStatus;
  maxRetries: number;
  retryBackoffMs: number;
  retryBackoffMax: number;
  retryBackoffMult: number;
  timeoutMs: number;
  rateLimitPerSec: number;
  circuitThreshold: number;
  circuitResetMs: number;
  customHeaders?: Record<string, string>;
  hasCustomSecret: boolean;
  secretRotatedAt?: string;
  createdAt: string;
  updatedAt: string;
}

// EndpointStats holds statistics for an endpoint
export interface EndpointStats {
  totalEvents: number;
  delivered: number;
  failed: number;
  pending: number;
  successRate: number;
  avgLatencyMs: number;
}

// EndpointWithStats includes an endpoint with its statistics
export interface EndpointWithStats extends Endpoint {
  stats: EndpointStats;
}

// QueueStats holds queue statistics
export interface QueueStats {
  queued: number;
  delivering: number;
  delivered: number;
  failed: number;
  dead: number;
  pending: number;
  processing: number;
  delayed: number;
}

// Pagination holds pagination information
export interface Pagination {
  hasMore: boolean;
  cursor?: string;
  total: number;
}

// EventList is a paginated list of events
export interface EventList {
  data: Event[];
  pagination: Pagination;
}

// EndpointList is a paginated list of endpoints
export interface EndpointList {
  data: Endpoint[];
  pagination: Pagination;
}

// BatchRetryError represents an event that failed to retry
export interface BatchRetryError {
  eventId: string;
  error: string;
}

// BatchRetryResult is the result of a batch retry operation
export interface BatchRetryResult {
  succeeded: Event[];
  failed: BatchRetryError[];
  totalRequested: number;
  totalSucceeded: number;
}

// Request types

export interface CreateEventRequest {
  destination?: string;
  eventType?: string;
  payload: unknown;
  headers?: Record<string, string>;
  maxAttempts?: number;
}

export interface ListEventsOptions {
  status?: EventStatus;
  limit?: number;
  cursor?: string;
}

export interface CreateEndpointRequest {
  url: string;
  description?: string;
  eventTypes: string[];
  maxRetries?: number;
  retryBackoffMs?: number;
  retryBackoffMax?: number;
  retryBackoffMult?: number;
  timeoutMs?: number;
  rateLimitPerSec?: number;
  circuitThreshold?: number;
  circuitResetMs?: number;
  customHeaders?: Record<string, string>;
}

export interface UpdateEndpointRequest {
  url?: string;
  description?: string;
  eventTypes?: string[];
  status?: EndpointStatus;
  maxRetries?: number;
  retryBackoffMs?: number;
  retryBackoffMax?: number;
  retryBackoffMult?: number;
  timeoutMs?: number;
  rateLimitPerSec?: number;
  circuitThreshold?: number;
  circuitResetMs?: number;
  customHeaders?: Record<string, string>;
}

export interface ListEndpointsOptions {
  status?: EndpointStatus;
  limit?: number;
  cursor?: string;
}

export interface BatchRetryByIdsRequest {
  eventIds: string[];
}

export interface BatchRetryByStatusRequest {
  status: 'failed' | 'dead';
  limit?: number;
}

export interface BatchRetryByEndpointRequest {
  endpointId: string;
  limit?: number;
}

export type BatchRetryRequest =
  | BatchRetryByIdsRequest
  | BatchRetryByStatusRequest
  | BatchRetryByEndpointRequest;

// Secret rotation result
export interface SecretRotationResult {
  endpoint: Endpoint;
  newSecret: string;
}
