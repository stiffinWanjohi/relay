// Main client
export { RelayClient } from './client';
export type { RelayClientOptions } from './client';

// Types
export type {
  // Event types
  Event,
  EventStatus,
  EventWithAttempts,
  EventList,
  DeliveryAttempt,
  Priority,

  // Endpoint types
  Endpoint,
  EndpointStatus,
  EndpointStats,
  EndpointWithStats,
  EndpointList,

  // Queue stats
  QueueStats,
  PriorityQueueStats,
  FIFOQueueStats,
  FIFOEndpointStats,
  FIFODrainResult,
  FIFORecoveryResult,

  // Pagination
  Pagination,

  // Batch operations
  BatchRetryResult,
  BatchRetryError,

  // Request types
  CreateEventRequest,
  SendEventRequest,
  ListEventsOptions,
  CreateEndpointRequest,
  UpdateEndpointRequest,
  ListEndpointsOptions,
  BatchRetryRequest,
  BatchRetryByIdsRequest,
  BatchRetryByStatusRequest,
  BatchRetryByEndpointRequest,
  SecretRotationResult,

  // Analytics types
  AnalyticsTimeRange,
  TimeGranularity,
  AnalyticsStats,
  TimeSeriesPoint,
  BreakdownItem,
  LatencyPercentiles,

  // Alert types
  AlertRule,
  Alert,
  AlertCondition,
  AlertAction,
  AlertMetric,
  AlertOperator,
  AlertActionType,
  CreateAlertRuleRequest,
  UpdateAlertRuleRequest,

  // Connector types
  Connector,
  ConnectorType,
  ConnectorConfig,
  ConnectorTemplate,
  CreateConnectorRequest,
  UpdateConnectorRequest,
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
