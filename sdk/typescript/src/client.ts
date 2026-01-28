import { APIError } from './errors';
import type {
  Event,
  EventWithAttempts,
  EventList,
  Endpoint,
  EndpointList,
  BatchRetryResult,
  QueueStats,
  CreateEventRequest,
  ListEventsOptions,
  CreateEndpointRequest,
  UpdateEndpointRequest,
  ListEndpointsOptions,
  BatchRetryRequest,
  SecretRotationResult,
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

  // Event operations

  async createEvent(req: CreateEventRequest, idempotencyKey: string): Promise<Event> {
    return this.request<Event>('POST', '/api/v1/events', req, {
      'X-Idempotency-Key': idempotencyKey,
    });
  }

  async getEvent(id: string): Promise<EventWithAttempts> {
    return this.request<EventWithAttempts>('GET', `/api/v1/events/${id}`);
  }

  async listEvents(options: ListEventsOptions = {}): Promise<EventList> {
    const params = new URLSearchParams();
    if (options.status) params.set('status', options.status);
    if (options.limit) params.set('limit', String(options.limit));
    if (options.cursor) params.set('cursor', options.cursor);

    const query = params.toString();
    return this.request<EventList>('GET', `/api/v1/events${query ? `?${query}` : ''}`);
  }

  async replayEvent(id: string): Promise<Event> {
    return this.request<Event>('POST', `/api/v1/events/${id}/replay`);
  }

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

  // Endpoint operations

  async createEndpoint(req: CreateEndpointRequest): Promise<Endpoint> {
    return this.request<Endpoint>('POST', '/api/v1/endpoints', req);
  }

  async getEndpoint(id: string): Promise<Endpoint> {
    return this.request<Endpoint>('GET', `/api/v1/endpoints/${id}`);
  }

  async listEndpoints(options: ListEndpointsOptions = {}): Promise<EndpointList> {
    const params = new URLSearchParams();
    if (options.status) params.set('status', options.status);
    if (options.limit) params.set('limit', String(options.limit));
    if (options.cursor) params.set('cursor', options.cursor);

    const query = params.toString();
    return this.request<EndpointList>('GET', `/api/v1/endpoints${query ? `?${query}` : ''}`);
  }

  async updateEndpoint(id: string, req: UpdateEndpointRequest): Promise<Endpoint> {
    return this.request<Endpoint>('PATCH', `/api/v1/endpoints/${id}`, req);
  }

  async deleteEndpoint(id: string): Promise<void> {
    await this.request<void>('DELETE', `/api/v1/endpoints/${id}`);
  }

  async rotateEndpointSecret(id: string): Promise<SecretRotationResult> {
    return this.request<SecretRotationResult>('POST', `/api/v1/endpoints/${id}/rotate-secret`);
  }

  async clearPreviousSecret(id: string): Promise<Endpoint> {
    return this.request<Endpoint>('POST', `/api/v1/endpoints/${id}/clear-previous-secret`);
  }

  // Stats & Health

  async getStats(): Promise<QueueStats> {
    return this.request<QueueStats>('GET', '/api/v1/stats');
  }

  async healthCheck(): Promise<{ status: string }> {
    return this.request<{ status: string }>('GET', '/health');
  }
}
