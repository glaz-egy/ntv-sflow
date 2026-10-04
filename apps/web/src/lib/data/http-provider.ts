import type {
  ApiError,
  DestinationKey,
  DeviceDetail,
  GlobeDestinationDetail,
  GlobeQuery,
  GlobeResponse,
  Grouping,
  HistoryTimelineResponse,
  HomeTrafficQuery,
  HomeTrafficResponse,
  ServerEnvelope,
  StatusResponse,
  TimeRangeQuery,
  TopologyResponse,
  WindowUpdatePayload,
} from "@/contracts";
import type { TrafficDataProvider } from "./provider";

type QueryValue = string | number | boolean | null | undefined | readonly string[];

export class ApiRequestError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

/** Builds a query string, dropping empty values; arrays become comma lists. */
export function toQuery(params: Record<string, QueryValue>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === null || v === undefined || v === "") continue;
    if (Array.isArray(v)) {
      if (v.length > 0) q.set(k, v.join(","));
    } else {
      q.set(k, String(v));
    }
  }
  const s = q.toString();
  return s ? `?${s}` : "";
}

export interface HttpProviderOptions {
  fetch?: typeof fetch;
  WebSocket?: typeof WebSocket;
  /** Reconnect backoff bounds (ms). */
  minBackoffMs?: number;
  maxBackoffMs?: number;
}

/**
 * Talks to the Go API (REST snapshots + WebSocket window events).
 * Same interface and DTOs as the in-browser mock, so views do not change.
 */
export class HttpDataProvider implements TrafficDataProvider {
  readonly transport = "api" as const;
  private readonly base: string;
  private readonly fetchFn: typeof fetch;
  private readonly WS: typeof WebSocket | undefined;
  private readonly listeners = new Set<(u: WindowUpdatePayload) => void>();
  private socket: WebSocket | null = null;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private backoff: number;
  private disposed = false;

  constructor(
    baseUrl: string,
    private readonly options: HttpProviderOptions = {},
  ) {
    this.base = baseUrl.replace(/\/+$/, "") + "/api/v1";
    this.fetchFn = options.fetch ?? fetch.bind(globalThis);
    this.WS = options.WebSocket ?? (typeof WebSocket !== "undefined" ? WebSocket : undefined);
    this.backoff = options.minBackoffMs ?? 1000;
  }

  private async get<T>(path: string, params: Record<string, QueryValue> = {}): Promise<T> {
    const res = await this.fetchFn(this.base + path + toQuery(params), { headers: { Accept: "application/json" } });
    if (!res.ok) {
      let code = "HTTP_ERROR";
      let message = `${res.status} ${res.statusText}`;
      try {
        const body = (await res.json()) as ApiError;
        code = body.error.code;
        message = body.error.message;
      } catch {
        // keep generic error
      }
      throw new ApiRequestError(res.status, code, message);
    }
    return (await res.json()) as T;
  }

  private async getOrNull<T>(path: string, params: Record<string, QueryValue> = {}): Promise<T | null> {
    try {
      return await this.get<T>(path, params);
    } catch (e) {
      if (e instanceof ApiRequestError && e.status === 404) return null;
      throw e;
    }
  }

  getStatus() {
    return this.get<StatusResponse>("/status");
  }

  getGlobe(q: GlobeQuery) {
    return this.get<GlobeResponse>("/globe", { ...q });
  }

  getDestination(key: DestinationKey, q: Omit<GlobeQuery, "grouping">) {
    return this.getOrNull<GlobeDestinationDetail>(`/globe/destinations/${encodeURIComponent(key)}`, {
      source_node_id: q.source_node_id,
      protocol: q.protocol,
      start: q.start,
      end: q.end,
    });
  }

  getHomeTraffic(q: HomeTrafficQuery) {
    return this.get<HomeTrafficResponse>("/home/traffic", { ...q });
  }

  getTopology(range: TimeRangeQuery = {}) {
    return this.get<TopologyResponse>("/home/topology", { start: range.start, end: range.end });
  }

  getDevice(id: string, grouping: Grouping, range: TimeRangeQuery = {}) {
    return this.getOrNull<DeviceDetail>(`/devices/${encodeURIComponent(id)}`, {
      grouping,
      start: range.start,
      end: range.end,
    });
  }

  getHistoryTimeline(start: string, end: string, maxPoints: number) {
    return this.get<HistoryTimelineResponse>("/history/timeline", { start, end, max_points: maxPoints });
  }

  subscribe(listener: (u: WindowUpdatePayload) => void) {
    // A provider may be re-subscribed after dispose() (React Strict Mode
    // runs effect cleanup + setup twice in development).
    this.disposed = false;
    this.listeners.add(listener);
    this.connect();
    return () => {
      this.listeners.delete(listener);
    };
  }

  private connect() {
    if (this.socket || this.disposed || !this.WS) return;
    const url = this.base.replace(/^http/, "ws") + "/ws";
    const socket = new this.WS(url);
    this.socket = socket;
    socket.onopen = () => {
      this.backoff = this.options.minBackoffMs ?? 1000;
      socket.send(JSON.stringify({ type: "subscribe", channels: ["globe", "home", "status"] }));
    };
    socket.onmessage = (ev) => {
      let env: ServerEnvelope;
      try {
        env = JSON.parse(String(ev.data));
      } catch {
        return;
      }
      if (env.type === "window_update") {
        for (const l of this.listeners) l(env.payload as WindowUpdatePayload);
      }
    };
    socket.onclose = () => {
      this.socket = null;
      if (this.disposed || this.listeners.size === 0) return;
      // No updates while disconnected → the UI turns "Stale" by itself.
      this.retryTimer = setTimeout(() => this.connect(), this.backoff);
      this.backoff = Math.min(this.backoff * 2, this.options.maxBackoffMs ?? 10_000);
    };
  }

  dispose() {
    this.disposed = true;
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.retryTimer = null;
    this.socket?.close();
    this.socket = null;
    this.listeners.clear();
  }
}
