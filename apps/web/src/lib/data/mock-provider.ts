import type { WindowUpdatePayload } from "@/contracts";
import { MockBackend } from "@/lib/mock-backend/server";
import { START_TICK, type ScenarioName } from "@/lib/mock-backend/scenarios";
import type { TrafficDataProvider } from "./provider";

export interface MockProviderConfig {
  seed: number;
  scenario: ScenarioName;
  speed: number;
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
    return Promise.resolve(this.backend.getGlobe(query));
  }
  getDestination(...args: Parameters<MockBackend["getDestination"]>) {
    return Promise.resolve(this.backend.getDestination(...args));
  }
  getHomeTraffic(query: Parameters<MockBackend["getHomeTraffic"]>[0]) {
    return Promise.resolve(this.backend.getHomeTraffic(query));
  }
  getTopology() {
    return Promise.resolve(this.backend.getTopology());
  }
  getDevice(...args: Parameters<MockBackend["getDevice"]>) {
    return Promise.resolve(this.backend.getDevice(...args));
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
