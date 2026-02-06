// Event status
export type EventStatus = 'queued' | 'delivering' | 'delivered' | 'failed' | 'dead';

// Endpoint status
export type EndpointStatus = 'active' | 'paused' | 'disabled';

// Priority levels (1=highest, 10=lowest)
export type Priority = 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10;

// Event represents a webhook event
export interface Event {
  id: string;
  idempotencyKey: string;
  destination: string;
  eventType?: string;
  payload?: unknown;
  headers?: Record<string, string>;
  status: EventStatus;
  priority: number;
  scheduledAt?: string;
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

  // Content-based routing filter
  filter?: Record<string, unknown>;

  // Payload transformation (JavaScript code)
  transformation?: string;

  // FIFO (ordered delivery) configuration
  fifo: boolean;
  fifoPartitionKey?: string;

  // Retry configuration
  maxRetries: number;
  retryBackoffMs: number;
  retryBackoffMax: number;
  retryBackoffMult: number;

  // Delivery configuration
  timeoutMs: number;
  rateLimitPerSec: number;

  // Circuit breaker configuration
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

// PriorityQueueStats holds priority queue statistics
export interface PriorityQueueStats {
  high: number;
  normal: number;
  low: number;
  delayed: number;
}

// FIFOQueueStats holds FIFO queue statistics for a partition
export interface FIFOQueueStats {
  endpointId: string;
  partitionKey: string;
  queueLength: number;
  isLocked: boolean;
  hasInFlight: boolean;
}

// FIFOEndpointStats holds FIFO statistics for an endpoint
export interface FIFOEndpointStats {
  endpointId: string;
  totalPartitions: number;
  totalQueuedMessages: number;
  partitions: FIFOQueueStats[];
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
  // Priority: 1-10 (1=highest, 10=lowest, default=5)
  priority?: Priority;
  // Schedule for future delivery (ISO 8601 timestamp)
  deliverAt?: string;
  // Schedule for future delivery (relative delay in seconds)
  delaySeconds?: number;
}

// SendEventRequest for fan-out delivery via event types
export interface SendEventRequest {
  eventType: string;
  payload: unknown;
  headers?: Record<string, string>;
  // Priority: 1-10 (1=highest, 10=lowest, default=5)
  priority?: Priority;
  // Schedule for future delivery (ISO 8601 timestamp)
  deliverAt?: string;
  // Schedule for future delivery (relative delay in seconds)
  delaySeconds?: number;
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

  // Content-based routing filter
  filter?: Record<string, unknown>;

  // Payload transformation (JavaScript code)
  transformation?: string;

  // FIFO (ordered delivery) configuration
  fifo?: boolean;
  fifoPartitionKey?: string;

  // Retry configuration
  maxRetries?: number;
  retryBackoffMs?: number;
  retryBackoffMax?: number;
  retryBackoffMult?: number;

  // Delivery configuration
  timeoutMs?: number;
  rateLimitPerSec?: number;

  // Circuit breaker configuration
  circuitThreshold?: number;
  circuitResetMs?: number;

  customHeaders?: Record<string, string>;
}

export interface UpdateEndpointRequest {
  url?: string;
  description?: string;
  eventTypes?: string[];
  status?: EndpointStatus;

  // Content-based routing filter (set to null to remove)
  filter?: Record<string, unknown> | null;

  // Payload transformation (set to null to remove)
  transformation?: string | null;

  // FIFO configuration
  fifo?: boolean;
  fifoPartitionKey?: string;

  // Retry configuration
  maxRetries?: number;
  retryBackoffMs?: number;
  retryBackoffMax?: number;
  retryBackoffMult?: number;

  // Delivery configuration
  timeoutMs?: number;
  rateLimitPerSec?: number;

  // Circuit breaker configuration
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

// Analytics types

export interface AnalyticsTimeRange {
  start: string; // ISO 8601 timestamp
  end: string; // ISO 8601 timestamp
}

export type TimeGranularity = 'MINUTE' | 'HOUR' | 'DAY' | 'WEEK';

export interface AnalyticsStats {
  period: string;
  totalCount: number;
  successCount: number;
  failureCount: number;
  timeoutCount: number;
  successRate: number;
  failureRate: number;
  avgLatencyMs: number;
  p50LatencyMs: number;
  p95LatencyMs: number;
  p99LatencyMs: number;
  minLatencyMs: number;
  maxLatencyMs: number;
}

export interface TimeSeriesPoint {
  timestamp: string;
  value: number;
}

export interface BreakdownItem {
  key: string;
  count: number;
  successRate: number;
  avgLatencyMs: number;
}

export interface LatencyPercentiles {
  p50: number;
  p95: number;
  p99: number;
}

// Alert types

export type AlertMetric =
  | 'FAILURE_RATE'
  | 'SUCCESS_RATE'
  | 'LATENCY'
  | 'QUEUE_DEPTH'
  | 'ERROR_COUNT'
  | 'DELIVERY_COUNT';

export type AlertOperator = 'GT' | 'GTE' | 'LT' | 'LTE' | 'EQ' | 'NE';

export type AlertActionType = 'SLACK' | 'EMAIL' | 'WEBHOOK' | 'PAGERDUTY';

export interface AlertCondition {
  metric: AlertMetric;
  operator: AlertOperator;
  value: number;
  window: string;
}

export interface AlertAction {
  type: AlertActionType;
  webhookUrl?: string;
  channel?: string;
  to?: string[];
  subject?: string;
  routingKey?: string;
  severity?: string;
  message?: string;
}

export interface AlertRule {
  id: string;
  name: string;
  description?: string;
  enabled: boolean;
  condition: AlertCondition;
  action: AlertAction;
  cooldown: string;
  lastFiredAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface Alert {
  id: string;
  ruleId: string;
  ruleName: string;
  metric: string;
  value: number;
  threshold: number;
  message: string;
  firedAt: string;
}

export interface CreateAlertRuleRequest {
  name: string;
  description?: string;
  condition: AlertCondition;
  action: AlertAction;
  cooldown?: string;
}

export interface UpdateAlertRuleRequest {
  name?: string;
  description?: string;
  enabled?: boolean;
  condition?: AlertCondition;
  action?: AlertAction;
  cooldown?: string;
}

// Connector types

export type ConnectorType = 'SLACK' | 'DISCORD' | 'TEAMS' | 'EMAIL' | 'WEBHOOK';

export interface ConnectorConfig {
  webhookUrl?: string;
  channel?: string;
  username?: string;
  iconEmoji?: string;
  iconUrl?: string;
  smtpHost?: string;
  smtpPort?: number;
  fromEmail?: string;
  toEmails?: string[];
}

export interface ConnectorTemplate {
  text?: string;
  title?: string;
  body?: string;
  color?: string;
  subject?: string;
}

export interface Connector {
  name: string;
  type: ConnectorType;
  config: ConnectorConfig;
  template: ConnectorTemplate;
}

export interface CreateConnectorRequest {
  name: string;
  type: ConnectorType;
  config: ConnectorConfig;
  template?: ConnectorTemplate;
}

export interface UpdateConnectorRequest {
  config?: ConnectorConfig;
  template?: ConnectorTemplate;
}

// FIFO management types

export interface FIFODrainResult {
  endpointId: string;
  partitionKey?: string;
  messagesDrained: number;
  movedToStandard: boolean;
}

export interface FIFORecoveryResult {
  messagesRecovered: number;
}
