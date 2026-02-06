import { APIError } from './errors';
import type {
  Event,
  EventWithAttempts,
  EventList,
  Endpoint,
  EndpointList,
  BatchRetryResult,
  QueueStats,
  PriorityQueueStats,
  FIFOEndpointStats,
  FIFOQueueStats,
  FIFODrainResult,
  FIFORecoveryResult,
  CreateEventRequest,
  SendEventRequest,
  ListEventsOptions,
  CreateEndpointRequest,
  UpdateEndpointRequest,
  ListEndpointsOptions,
  BatchRetryRequest,
  SecretRotationResult,
  AnalyticsTimeRange,
  TimeGranularity,
  AnalyticsStats,
  TimeSeriesPoint,
  BreakdownItem,
  LatencyPercentiles,
  AlertRule,
  Alert,
  CreateAlertRuleRequest,
  UpdateAlertRuleRequest,
  Connector,
  CreateConnectorRequest,
  UpdateConnectorRequest,
} from './types';

export interface RelayClientOptions {
  timeout?: number;
  headers?: Record<string, string>;
}

export class RelayClient {
  private readonly baseUrl: string;
  private readonly apiKey: string;
  private readonly timeout: number;
  private readonly defaultHeaders: Record<string, string>;

  constructor(baseUrl: string, apiKey: string, options: RelayClientOptions = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.apiKey = apiKey;
    this.timeout = options.timeout ?? 30000;
    this.defaultHeaders = options.headers ?? {};
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
    headers?: Record<string, string>
  ): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), this.timeout);

    try {
      const response = await fetch(url, {
        method,
        headers: {
          'Content-Type': 'application/json',
          'X-API-Key': this.apiKey,
          ...this.defaultHeaders,
          ...headers,
        },
        body: body ? JSON.stringify(body) : undefined,
        signal: controller.signal,
      });

      clearTimeout(timeoutId);

      const responseBody = response.headers.get('content-type')?.includes('application/json')
        ? await response.json()
        : await response.text();

      if (!response.ok) {
        throw APIError.fromResponse(response.status, responseBody);
      }

      return responseBody as T;
    } catch (error) {
      clearTimeout(timeoutId);
      if (error instanceof APIError) {
        throw error;
      }
      if (error instanceof Error && error.name === 'AbortError') {
        throw new APIError('Request timeout', 408);
      }
      throw error;
    }
  }

  // ==========================================
  // Event operations
  // ==========================================

  /**
   * Create a new webhook event with direct destination URL.
   * Supports priority (1-10) and scheduled delivery.
   */
  async createEvent(req: CreateEventRequest, idempotencyKey: string): Promise<Event> {
    return this.request<Event>('POST', '/api/v1/events', req, {
      'X-Idempotency-Key': idempotencyKey,
    });
  }

  /**
   * Send an event by event type (fan-out to all subscribed endpoints).
   * Returns an array of created events, one per subscribed endpoint.
   */
  async sendEvent(req: SendEventRequest, idempotencyKey: string): Promise<Event[]> {
    return this.request<Event[]>('POST', '/api/v1/events/send', req, {
      'X-Idempotency-Key': idempotencyKey,
    });
  }

  /**
   * Get an event by ID with delivery history.
   */
  async getEvent(id: string): Promise<EventWithAttempts> {
    return this.request<EventWithAttempts>('GET', `/api/v1/events/${id}`);
  }

  /**
   * List events with pagination.
   */
  async listEvents(options: ListEventsOptions = {}): Promise<EventList> {
    const params = new URLSearchParams();
    if (options.status) params.set('status', options.status);
    if (options.limit) params.set('limit', String(options.limit));
    if (options.cursor) params.set('cursor', options.cursor);

    const query = params.toString();
    return this.request<EventList>('GET', `/api/v1/events${query ? `?${query}` : ''}`);
  }

  /**
   * Replay a failed or dead event.
   */
  async replayEvent(id: string): Promise<Event> {
    return this.request<Event>('POST', `/api/v1/events/${id}/replay`);
  }

  /**
   * Cancel a scheduled event before it is delivered.
   * Only works for events that are still in QUEUED status with a future scheduledAt.
   */
  async cancelScheduledEvent(id: string): Promise<Event> {
    return this.request<Event>('POST', `/api/v1/events/${id}/cancel`);
  }

  /**
   * Batch retry multiple events.
   */
  async batchRetry(req: BatchRetryRequest): Promise<BatchRetryResult> {
    let body: Record<string, unknown>;

    if ('eventIds' in req) {
      body = { event_ids: req.eventIds };
    } else if ('status' in req) {
      body = { status: req.status, limit: req.limit };
    } else {
      body = { endpoint_id: req.endpointId, limit: req.limit };
    }

    return this.request<BatchRetryResult>('POST', '/api/v1/events/batch/retry', body);
  }

  // ==========================================
  // Endpoint operations
  // ==========================================

  /**
   * Create a new webhook endpoint.
   * Supports FIFO (ordered delivery), transformations, and content filters.
   */
  async createEndpoint(req: CreateEndpointRequest): Promise<Endpoint> {
    return this.request<Endpoint>('POST', '/api/v1/endpoints', req);
  }

  /**
   * Get an endpoint by ID.
   */
  async getEndpoint(id: string): Promise<Endpoint> {
    return this.request<Endpoint>('GET', `/api/v1/endpoints/${id}`);
  }

  /**
   * List endpoints with pagination.
   */
  async listEndpoints(options: ListEndpointsOptions = {}): Promise<EndpointList> {
    const params = new URLSearchParams();
    if (options.status) params.set('status', options.status);
    if (options.limit) params.set('limit', String(options.limit));
    if (options.cursor) params.set('cursor', options.cursor);

    const query = params.toString();
    return this.request<EndpointList>('GET', `/api/v1/endpoints${query ? `?${query}` : ''}`);
  }

  /**
   * Update an endpoint.
   */
  async updateEndpoint(id: string, req: UpdateEndpointRequest): Promise<Endpoint> {
    return this.request<Endpoint>('PATCH', `/api/v1/endpoints/${id}`, req);
  }

  /**
   * Delete an endpoint.
   */
  async deleteEndpoint(id: string): Promise<void> {
    await this.request<void>('DELETE', `/api/v1/endpoints/${id}`);
  }

  /**
   * Pause an endpoint (stops delivery but keeps events queued).
   */
  async pauseEndpoint(id: string): Promise<Endpoint> {
    return this.request<Endpoint>('POST', `/api/v1/endpoints/${id}/pause`);
  }

  /**
   * Resume a paused endpoint.
   */
  async resumeEndpoint(id: string): Promise<Endpoint> {
    return this.request<Endpoint>('POST', `/api/v1/endpoints/${id}/resume`);
  }

  /**
   * Rotate an endpoint's signing secret.
   * The new secret is returned only once. Previous secret remains valid during grace period.
   */
  async rotateEndpointSecret(id: string): Promise<SecretRotationResult> {
    return this.request<SecretRotationResult>('POST', `/api/v1/endpoints/${id}/rotate-secret`);
  }

  /**
   * Clear the previous secret after rotation is complete.
   */
  async clearPreviousSecret(id: string): Promise<Endpoint> {
    return this.request<Endpoint>('POST', `/api/v1/endpoints/${id}/clear-previous-secret`);
  }

  // ==========================================
  // Stats & Health
  // ==========================================

  /**
   * Get queue statistics.
   */
  async getStats(): Promise<QueueStats> {
    return this.request<QueueStats>('GET', '/api/v1/stats');
  }

  /**
   * Get priority queue statistics.
   */
  async getPriorityQueueStats(): Promise<PriorityQueueStats> {
    return this.request<PriorityQueueStats>('GET', '/api/v1/stats/priority');
  }

  /**
   * Check service health.
   */
  async healthCheck(): Promise<{ status: string }> {
    return this.request<{ status: string }>('GET', '/health');
  }

  // ==========================================
  // FIFO Queue Management
  // ==========================================

  /**
   * Get FIFO queue stats for a specific endpoint.
   */
  async getFIFOQueueStats(endpointId: string): Promise<FIFOEndpointStats> {
    return this.request<FIFOEndpointStats>('GET', `/api/v1/fifo/${endpointId}/stats`);
  }

  /**
   * Get FIFO queue stats for a specific partition.
   */
  async getFIFOPartitionStats(endpointId: string, partitionKey: string): Promise<FIFOQueueStats> {
    return this.request<FIFOQueueStats>(
      'GET',
      `/api/v1/fifo/${endpointId}/partitions/${encodeURIComponent(partitionKey)}/stats`
    );
  }

  /**
   * List all active FIFO queues.
   */
  async listActiveFIFOQueues(): Promise<FIFOQueueStats[]> {
    return this.request<FIFOQueueStats[]>('GET', '/api/v1/fifo/active');
  }

  /**
   * Forcibly release a stuck FIFO lock (admin operation).
   */
  async releaseFIFOLock(endpointId: string, partitionKey?: string): Promise<boolean> {
    const body: Record<string, string> = {};
    if (partitionKey) body.partitionKey = partitionKey;
    return this.request<boolean>('POST', `/api/v1/fifo/${endpointId}/release-lock`, body);
  }

  /**
   * Drain a FIFO queue and optionally move messages to standard queue.
   */
  async drainFIFOQueue(
    endpointId: string,
    partitionKey?: string,
    moveToStandard?: boolean
  ): Promise<FIFODrainResult> {
    const body: Record<string, unknown> = {};
    if (partitionKey) body.partitionKey = partitionKey;
    if (moveToStandard !== undefined) body.moveToStandard = moveToStandard;
    return this.request<FIFODrainResult>('POST', `/api/v1/fifo/${endpointId}/drain`, body);
  }

  /**
   * Recover all stale in-flight FIFO messages (admin operation).
   */
  async recoverStaleFIFOMessages(): Promise<FIFORecoveryResult> {
    return this.request<FIFORecoveryResult>('POST', '/api/v1/fifo/recover');
  }

  // ==========================================
  // Analytics
  // ==========================================

  /**
   * Get aggregated delivery statistics for a time range.
   */
  async getAnalyticsStats(timeRange: AnalyticsTimeRange): Promise<AnalyticsStats> {
    const params = new URLSearchParams();
    params.set('start', timeRange.start);
    params.set('end', timeRange.end);
    return this.request<AnalyticsStats>('GET', `/api/v1/analytics/stats?${params}`);
  }

  /**
   * Get success rate time series.
   */
  async getSuccessRateTimeSeries(
    timeRange: AnalyticsTimeRange,
    granularity: TimeGranularity
  ): Promise<TimeSeriesPoint[]> {
    const params = new URLSearchParams();
    params.set('start', timeRange.start);
    params.set('end', timeRange.end);
    params.set('granularity', granularity);
    return this.request<TimeSeriesPoint[]>('GET', `/api/v1/analytics/success-rate?${params}`);
  }

  /**
   * Get latency time series (average).
   */
  async getLatencyTimeSeries(
    timeRange: AnalyticsTimeRange,
    granularity: TimeGranularity
  ): Promise<TimeSeriesPoint[]> {
    const params = new URLSearchParams();
    params.set('start', timeRange.start);
    params.set('end', timeRange.end);
    params.set('granularity', granularity);
    return this.request<TimeSeriesPoint[]>('GET', `/api/v1/analytics/latency?${params}`);
  }

  /**
   * Get delivery count time series.
   */
  async getDeliveryCountTimeSeries(
    timeRange: AnalyticsTimeRange,
    granularity: TimeGranularity
  ): Promise<TimeSeriesPoint[]> {
    const params = new URLSearchParams();
    params.set('start', timeRange.start);
    params.set('end', timeRange.end);
    params.set('granularity', granularity);
    return this.request<TimeSeriesPoint[]>('GET', `/api/v1/analytics/delivery-count?${params}`);
  }

  /**
   * Get breakdown by event type.
   */
  async getBreakdownByEventType(
    timeRange: AnalyticsTimeRange,
    limit?: number
  ): Promise<BreakdownItem[]> {
    const params = new URLSearchParams();
    params.set('start', timeRange.start);
    params.set('end', timeRange.end);
    if (limit) params.set('limit', String(limit));
    return this.request<BreakdownItem[]>('GET', `/api/v1/analytics/breakdown/event-type?${params}`);
  }

  /**
   * Get breakdown by endpoint.
   */
  async getBreakdownByEndpoint(
    timeRange: AnalyticsTimeRange,
    limit?: number
  ): Promise<BreakdownItem[]> {
    const params = new URLSearchParams();
    params.set('start', timeRange.start);
    params.set('end', timeRange.end);
    if (limit) params.set('limit', String(limit));
    return this.request<BreakdownItem[]>('GET', `/api/v1/analytics/breakdown/endpoint?${params}`);
  }

  /**
   * Get latency percentiles for a time range.
   */
  async getLatencyPercentiles(timeRange: AnalyticsTimeRange): Promise<LatencyPercentiles> {
    const params = new URLSearchParams();
    params.set('start', timeRange.start);
    params.set('end', timeRange.end);
    return this.request<LatencyPercentiles>('GET', `/api/v1/analytics/latency-percentiles?${params}`);
  }

  // ==========================================
  // Alert Rules
  // ==========================================

  /**
   * Create a new alert rule.
   */
  async createAlertRule(req: CreateAlertRuleRequest): Promise<AlertRule> {
    return this.request<AlertRule>('POST', '/api/v1/alerts/rules', req);
  }

  /**
   * Get an alert rule by ID.
   */
  async getAlertRule(id: string): Promise<AlertRule> {
    return this.request<AlertRule>('GET', `/api/v1/alerts/rules/${id}`);
  }

  /**
   * List all alert rules.
   */
  async listAlertRules(): Promise<AlertRule[]> {
    return this.request<AlertRule[]>('GET', '/api/v1/alerts/rules');
  }

  /**
   * Update an alert rule.
   */
  async updateAlertRule(id: string, req: UpdateAlertRuleRequest): Promise<AlertRule> {
    return this.request<AlertRule>('PATCH', `/api/v1/alerts/rules/${id}`, req);
  }

  /**
   * Delete an alert rule.
   */
  async deleteAlertRule(id: string): Promise<void> {
    await this.request<void>('DELETE', `/api/v1/alerts/rules/${id}`);
  }

  /**
   * Enable an alert rule.
   */
  async enableAlertRule(id: string): Promise<AlertRule> {
    return this.request<AlertRule>('POST', `/api/v1/alerts/rules/${id}/enable`);
  }

  /**
   * Disable an alert rule.
   */
  async disableAlertRule(id: string): Promise<AlertRule> {
    return this.request<AlertRule>('POST', `/api/v1/alerts/rules/${id}/disable`);
  }

  /**
   * Get alert history.
   */
  async getAlertHistory(limit?: number): Promise<Alert[]> {
    const params = new URLSearchParams();
    if (limit) params.set('limit', String(limit));
    const query = params.toString();
    return this.request<Alert[]>('GET', `/api/v1/alerts/history${query ? `?${query}` : ''}`);
  }

  // ==========================================
  // Connectors
  // ==========================================

  /**
   * Create a new connector.
   */
  async createConnector(req: CreateConnectorRequest): Promise<Connector> {
    return this.request<Connector>('POST', '/api/v1/connectors', req);
  }

  /**
   * Get a connector by name.
   */
  async getConnector(name: string): Promise<Connector> {
    return this.request<Connector>('GET', `/api/v1/connectors/${encodeURIComponent(name)}`);
  }

  /**
   * List all connectors.
   */
  async listConnectors(): Promise<Connector[]> {
    return this.request<Connector[]>('GET', '/api/v1/connectors');
  }

  /**
   * Update a connector.
   */
  async updateConnector(name: string, req: UpdateConnectorRequest): Promise<Connector> {
    return this.request<Connector>('PATCH', `/api/v1/connectors/${encodeURIComponent(name)}`, req);
  }

  /**
   * Delete a connector.
   */
  async deleteConnector(name: string): Promise<void> {
    await this.request<void>('DELETE', `/api/v1/connectors/${encodeURIComponent(name)}`);
  }
}
