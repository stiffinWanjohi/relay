// Main client
export { RelayClient } from './client';
export type { RelayClientOptions } from './client';

// Types
export type {
  Event,
  EventStatus,
  EventWithAttempts,
  EventList,
  DeliveryAttempt,
  Endpoint,
  EndpointStatus,
  EndpointStats,
  EndpointWithStats,
  EndpointList,
  QueueStats,
  Pagination,
  BatchRetryResult,
  BatchRetryError,
  CreateEventRequest,
  ListEventsOptions,
  CreateEndpointRequest,
  UpdateEndpointRequest,
  ListEndpointsOptions,
  BatchRetryRequest,
  BatchRetryByIdsRequest,
  BatchRetryByStatusRequest,
  BatchRetryByEndpointRequest,
  SecretRotationResult,
} from './types';

// Errors
export {
  RelayError,
  APIError,
  SignatureError,
  InvalidSignatureError,
  MissingSignatureError,
  InvalidTimestampError,
  MissingTimestampError,
} from './errors';

// Signature verification
export {
  verifySignature,
  verifySignatureWithRotation,
  computeSignature,
  extractWebhookHeaders,
} from './signature';
export type { VerifyOptions, WebhookHeaders } from './signature';
