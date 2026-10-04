import type { Grouping, TimeRangeQuery, WindowUpdatePayload } from "@/contracts";
import { MockBackend } from "@/lib/mock-backend/server";
import { START_TICK, type ScenarioName } from "@/lib/mock-backend/scenarios";
import type { TrafficDataProvider } from "./provider";

export interface MockProviderConfig {
  seed: number;
  scenario: ScenarioName;
  speed: number;
}

const NO_HISTORY = "History needs the API backend (NEXT_PUBLIC_DATA_MODE=api); the in-browser mock keeps none.";

/** Never answer a historical request with live data. */
function liveOnly<T>(range: TimeRangeQuery | undefined, answer: () => T): Promise<T> {
  if (range?.start || range?.end) return Promise.reject(new Error(NO_HISTORY));
  return Promise.resolve(answer());
}

/**
 * In-browser mock provider. Runs the mock backend on a 1 s wall-clock cadence
 * (sim time advances by `speed` per second). No collector, database, GeoIP
 * file or network access needed (rule 13).
 */
export class MockDataProvider implements TrafficDataProvider {
  readonly transport = "in-browser" as const;
  private readonly backend: MockBackend;
  private readonly listeners = new Set<(u: WindowUpdatePayload) => void>();
  private timer: ReturnType<typeof setInterval> | null = null;
  private simTime = START_TICK;

  constructor(config: MockProviderConfig) {
    this.backend = new MockBackend({
      ...config,
      epochMs: Math.floor(Date.now() / 1000) * 1000 - START_TICK * 1000,
    });
    this.backend.setTick(START_TICK);
  }

  get scenarioDescription(): string {
    return this.backend.scenarioDescription;
  }

  private start() {
    if (this.timer) return;
    this.timer = setInterval(() => {
      this.simTime += this.backend.options.speed;
      const tick = Math.floor(this.simTime);
      if (tick === this.backend.currentTick) return;
      this.backend.setTick(tick);
      const status = this.backend.getStatus();
      const update: WindowUpdatePayload = { window_end: status.last_aggregate_at ?? status.server_time, status };
      for (const l of this.listeners) l(update);
    }, 1000);
  }

  getStatus() {
    return Promise.resolve(this.backend.getStatus());
  }
  getGlobe(query: Parameters<MockBackend["getGlobe"]>[0]) {
    return liveOnly(query, () => this.backend.getGlobe(query));
  }
  getDestination(...args: Parameters<MockBackend["getDestination"]>) {
    return liveOnly(args[1], () => this.backend.getDestination(...args));
  }
  getHomeTraffic(query: Parameters<MockBackend["getHomeTraffic"]>[0]) {
    return liveOnly(query, () => this.backend.getHomeTraffic(query));
  }
  getTopology(range?: TimeRangeQuery) {
    return liveOnly(range, () => this.backend.getTopology());
  }
  getDevice(id: string, grouping: Grouping, range?: TimeRangeQuery) {
    return liveOnly(range, () => this.backend.getDevice(id, grouping));
  }
  getHistoryTimeline(): Promise<never> {
    return Promise.reject(new Error(NO_HISTORY));
  }

  subscribe(listener: (u: WindowUpdatePayload) => void) {
    this.listeners.add(listener);
    this.start();
    return () => {
      this.listeners.delete(listener);
    };
  }

  dispose() {
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
    this.listeners.clear();
  }
}
