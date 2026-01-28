import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { RelayClient } from "./client";
import { APIError } from "./errors";

describe("RelayClient", () => {
  let client: RelayClient;
  const baseUrl = "https://api.relay.example.com";
  const apiKey = "rly_test_key";

  beforeEach(() => {
    client = new RelayClient(baseUrl, apiKey);
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  describe("constructor", () => {
    it("should create client with base URL and API key", () => {
      const c = new RelayClient("https://api.example.com/", "key123");
      expect(c).toBeInstanceOf(RelayClient);
    });

    it("should strip trailing slash from base URL", async () => {
      const c = new RelayClient("https://api.example.com/", "key123");

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify({ status: "ok" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      await c.healthCheck();

      expect(fetch).toHaveBeenCalledWith(
        "https://api.example.com/health",
        expect.any(Object),
      );
    });
  });

  describe("createEvent", () => {
    it("should send POST request with idempotency key", async () => {
      const event = {
        id: "123",
        status: "queued",
        destination: "https://example.com/webhook",
      };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(event), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      );

      const result = await client.createEvent(
        { destination: "https://example.com/webhook", payload: { test: true } },
        "idem-key-123",
      );

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/events`,
        expect.objectContaining({
          method: "POST",
          headers: expect.objectContaining({
            "Content-Type": "application/json",
            "X-API-Key": apiKey,
            "X-Idempotency-Key": "idem-key-123",
          }),
        }),
      );
      expect(result.id).toBe("123");
    });
  });

  describe("getEvent", () => {
    it("should fetch event by ID", async () => {
      const event = {
        id: "456",
        status: "delivered",
        deliveryAttempts: [],
      };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(event), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      const result = await client.getEvent("456");

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/events/456`,
        expect.objectContaining({ method: "GET" }),
      );
      expect(result.id).toBe("456");
    });
  });

  describe("listEvents", () => {
    it("should list events with pagination", async () => {
      const response = {
        data: [{ id: "1" }, { id: "2" }],
        pagination: { hasMore: true, cursor: "next", total: 100 },
      };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(response), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      const result = await client.listEvents({ status: "failed", limit: 10 });

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/events?status=failed&limit=10`,
        expect.any(Object),
      );
      expect(result.data).toHaveLength(2);
      expect(result.pagination.hasMore).toBe(true);
    });

    it("should list events without options", async () => {
      const response = {
        data: [],
        pagination: { hasMore: false, total: 0 },
      };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(response), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      await client.listEvents();

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/events`,
        expect.any(Object),
      );
    });
  });

  describe("replayEvent", () => {
    it("should replay event", async () => {
      const event = { id: "789", status: "queued" };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(event), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      const result = await client.replayEvent("789");

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/events/789/replay`,
        expect.objectContaining({ method: "POST" }),
      );
      expect(result.status).toBe("queued");
    });
  });

  describe("batchRetry", () => {
    it("should batch retry by IDs", async () => {
      const response = {
        succeeded: [{ id: "1" }],
        failed: [],
        totalRequested: 1,
        totalSucceeded: 1,
      };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(response), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      const result = await client.batchRetry({ eventIds: ["1", "2"] });

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/events/batch/retry`,
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ event_ids: ["1", "2"] }),
        }),
      );
      expect(result.totalSucceeded).toBe(1);
    });

    it("should batch retry by status", async () => {
      const response = {
        succeeded: [],
        failed: [],
        totalRequested: 0,
        totalSucceeded: 0,
      };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(response), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      await client.batchRetry({ status: "failed", limit: 50 });

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/events/batch/retry`,
        expect.objectContaining({
          body: JSON.stringify({ status: "failed", limit: 50 }),
        }),
      );
    });

    it("should batch retry by endpoint", async () => {
      const response = {
        succeeded: [],
        failed: [],
        totalRequested: 0,
        totalSucceeded: 0,
      };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(response), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      await client.batchRetry({ endpointId: "ep-123", limit: 100 });

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/events/batch/retry`,
        expect.objectContaining({
          body: JSON.stringify({ endpoint_id: "ep-123", limit: 100 }),
        }),
      );
    });
  });

  describe("endpoint operations", () => {
    it("should create endpoint", async () => {
      const endpoint = { id: "ep-1", url: "https://example.com/hook" };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(endpoint), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      );

      const result = await client.createEndpoint({
        url: "https://example.com/hook",
        eventTypes: ["order.created"],
      });

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/endpoints`,
        expect.objectContaining({ method: "POST" }),
      );
      expect(result.id).toBe("ep-1");
    });

    it("should update endpoint", async () => {
      const endpoint = { id: "ep-1", status: "paused" };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(endpoint), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      await client.updateEndpoint("ep-1", { status: "paused" });

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/endpoints/ep-1`,
        expect.objectContaining({ method: "PATCH" }),
      );
    });

    it("should delete endpoint", async () => {
      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(null, { status: 204 }),
      );

      await client.deleteEndpoint("ep-1");

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/endpoints/ep-1`,
        expect.objectContaining({ method: "DELETE" }),
      );
    });

    it("should rotate endpoint secret", async () => {
      const response = {
        endpoint: { id: "ep-1" },
        newSecret: "whsec_new_secret",
      };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(response), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      const result = await client.rotateEndpointSecret("ep-1");

      expect(fetch).toHaveBeenCalledWith(
        `${baseUrl}/api/v1/endpoints/ep-1/rotate-secret`,
        expect.objectContaining({ method: "POST" }),
      );
      expect(result.newSecret).toBe("whsec_new_secret");
    });
  });

  describe("error handling", () => {
    it("should throw APIError for 4xx responses", async () => {
      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(
          JSON.stringify({ error: "Not found", code: "NOT_FOUND" }),
          {
            status: 404,
            headers: { "Content-Type": "application/json" },
          },
        ),
      );

      try {
        await client.getEvent("unknown");
        expect.fail("Should have thrown");
      } catch (e) {
        expect(e).toBeInstanceOf(APIError);
        expect((e as APIError).statusCode).toBe(404);
        expect((e as APIError).isNotFound()).toBe(true);
      }
    });

    it("should throw APIError for 5xx responses", async () => {
      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify({ error: "Internal error" }), {
          status: 500,
          headers: { "Content-Type": "application/json" },
        }),
      );

      try {
        await client.healthCheck();
      } catch (e) {
        expect(e).toBeInstanceOf(APIError);
        expect((e as APIError).isServerError()).toBe(true);
      }
    });

    it("should throw APIError for unauthorized", async () => {
      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify({ error: "Invalid API key" }), {
          status: 401,
          headers: { "Content-Type": "application/json" },
        }),
      );

      try {
        await client.listEvents();
      } catch (e) {
        expect(e).toBeInstanceOf(APIError);
        expect((e as APIError).isUnauthorized()).toBe(true);
      }
    });
  });

  describe("healthCheck", () => {
    it("should check health", async () => {
      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify({ status: "ok" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      const result = await client.healthCheck();

      expect(result.status).toBe("ok");
    });
  });

  describe("getStats", () => {
    it("should get queue stats", async () => {
      const stats = {
        queued: 10,
        delivering: 5,
        delivered: 100,
        failed: 3,
        dead: 1,
      };

      vi.mocked(fetch).mockResolvedValueOnce(
        new Response(JSON.stringify(stats), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

      const result = await client.getStats();

      expect(result.queued).toBe(10);
      expect(result.delivered).toBe(100);
    });
  });
});
