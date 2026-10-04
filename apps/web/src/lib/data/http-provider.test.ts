import { describe, expect, it, vi } from "vitest";
import { ApiRequestError, HttpDataProvider, toQuery } from "./http-provider";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("toQuery", () => {
  it("drops empty values and joins arrays", () => {
    expect(
      toQuery({ grouping: "asn", source_node_id: null, protocol: undefined, min_bps: 0, device_types: ["pc", "iot"], x: "" }),
    ).toBe("?grouping=asn&min_bps=0&device_types=pc%2Ciot");
    expect(toQuery({ a: null, b: [] })).toBe("");
  });
});

describe("HttpDataProvider", () => {
  it("calls the documented endpoints", async () => {
    const fetchFn = vi.fn(async () => jsonResponse(200, {}));
    const p = new HttpDataProvider("http://api:8080/", { fetch: fetchFn as unknown as typeof fetch });
    await p.getGlobe({ grouping: "ip", source_node_id: "dev_pc01", direction: "both" });
    await p.getDestination("ip:2001:db8::1", { protocol: "tcp" });
    await p.getHomeTraffic({ destination_key: "asn:13335", include_inactive: true });
    await p.getDevice("ep:10.30.0.77", "asn");
    const urls = fetchFn.mock.calls.map((c) => String((c as unknown[])[0]));
    expect(urls).toEqual([
      "http://api:8080/api/v1/globe?grouping=ip&source_node_id=dev_pc01&direction=both",
      "http://api:8080/api/v1/globe/destinations/ip%3A2001%3Adb8%3A%3A1?protocol=tcp",
      "http://api:8080/api/v1/home/traffic?destination_key=asn%3A13335&include_inactive=true",
      "http://api:8080/api/v1/devices/ep%3A10.30.0.77?grouping=asn",
    ]);
  });

  it("maps 404 to null for inspector lookups", async () => {
    const fetchFn = vi.fn(async () => jsonResponse(404, { error: { code: "NOT_FOUND", message: "x" } }));
    const p = new HttpDataProvider("http://api", { fetch: fetchFn as unknown as typeof fetch });
    await expect(p.getDevice("dev_nope", "asn")).resolves.toBeNull();
    await expect(p.getDestination("asn:1", {})).resolves.toBeNull();
  });

  it("surfaces API errors with their code", async () => {
    const fetchFn = vi.fn(async () => jsonResponse(400, { error: { code: "INVALID_FILTER", message: "bad" } }));
    const p = new HttpDataProvider("http://api", { fetch: fetchFn as unknown as typeof fetch });
    await expect(p.getGlobe({ grouping: "asn" })).rejects.toMatchObject(
      new ApiRequestError(400, "INVALID_FILTER", "bad"),
    );
  });

  it("forwards window_update events and reconnects after close", async () => {
    vi.useFakeTimers();
    const sockets: FakeSocket[] = [];
    class FakeSocket {
      onopen: (() => void) | null = null;
      onmessage: ((ev: { data: string }) => void) | null = null;
      onclose: (() => void) | null = null;
      sent: string[] = [];
      constructor(readonly url: string) {
        sockets.push(this);
      }
      send(d: string) {
        this.sent.push(d);
      }
      close() {
        this.onclose?.();
      }
    }
    const p = new HttpDataProvider("https://api.example", {
      WebSocket: FakeSocket as unknown as typeof WebSocket,
      minBackoffMs: 100,
    });
    const listener = vi.fn();
    p.subscribe(listener);
    expect(sockets[0]!.url).toBe("wss://api.example/api/v1/ws");
    sockets[0]!.onopen?.();
    expect(JSON.parse(sockets[0]!.sent[0]!).type).toBe("subscribe");
    sockets[0]!.onmessage?.({ data: JSON.stringify({ type: "heartbeat", sequence: 1, server_time: "", payload: {} }) });
    sockets[0]!.onmessage?.({ data: JSON.stringify({ type: "window_update", sequence: 2, server_time: "", payload: { window_end: "t" } }) });
    expect(listener).toHaveBeenCalledTimes(1);
    sockets[0]!.onclose?.();
    vi.advanceTimersByTime(100);
    expect(sockets).toHaveLength(2);
    p.dispose();
    vi.advanceTimersByTime(10_000);
    expect(sockets).toHaveLength(2);
    // Strict Mode: cleanup (dispose) then setup (subscribe) again must reconnect.
    p.subscribe(listener);
    expect(sockets).toHaveLength(3);
    vi.useRealTimers();
  });
});
