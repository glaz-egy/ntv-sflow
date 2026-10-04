/**
 * The only data boundary the visualization layer knows about (rule 14).
 * Mock and live (HTTP + WebSocket) providers implement the same interface and
 * return the same contract DTOs.
 */
import type {
  DestinationKey,
  DeviceDetail,
  GlobeDestinationDetail,
  GlobeQuery,
  GlobeResponse,
  Grouping,
  HomeTrafficQuery,
  HomeTrafficResponse,
  StatusResponse,
  TopologyResponse,
  WindowUpdatePayload,
} from "@/contracts";

/** Where the data comes from; `status.mode` says whether it is mock or live. */
export type Transport = "in-browser" | "api";

export interface TrafficDataProvider {
  readonly transport: Transport;
  getStatus(): Promise<StatusResponse>;
  getGlobe(query: GlobeQuery): Promise<GlobeResponse>;
  getDestination(
    key: DestinationKey,
    query: Omit<GlobeQuery, "grouping">,
  ): Promise<GlobeDestinationDetail | null>;
  getHomeTraffic(query: HomeTrafficQuery): Promise<HomeTrafficResponse>;
  getTopology(): Promise<TopologyResponse>;
  getDevice(id: string, grouping: Grouping): Promise<DeviceDetail | null>;
  /** Fires when a new aggregate window is available (WS `window_update`). */
  subscribe(listener: (update: WindowUpdatePayload) => void): () => void;
  dispose(): void;
}
