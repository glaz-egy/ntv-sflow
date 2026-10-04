/**
 * Cross-language golden fixtures (D-025, D-039).
 *
 * The TypeScript mock backend is the reference. This test writes/validates
 * internal/mock/testdata/golden.json; Go's golden_test.go replays the same
 * requests against the Go port and must produce identical JSON.
 *
 *   UPDATE_GOLDEN=1 pnpm test   # regenerate after an intentional change
 */
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { MockBackend } from "./server";
import { SCENARIO_NAMES, START_TICK } from "./scenarios";

const FILE = fileURLToPath(new URL("../../../../../internal/mock/testdata/golden.json", import.meta.url));
const EPOCH_MS = Date.UTC(2026, 9, 3, 14, 45, 0);
const SEED = 42;

type Request =
  | { kind: "status" }
  | { kind: "globe"; params: Parameters<MockBackend["getGlobe"]>[0] }
  | { kind: "destination"; key: string; params: { source_node_id?: string; protocol?: "tcp" | "udp" } }
  | { kind: "home"; params: Parameters<MockBackend["getHomeTraffic"]>[0] }
  | { kind: "topology" }
  | { kind: "device"; id: string; grouping: "asn" | "country" | "city" | "ip" };

const FULL: Request[] = [
  { kind: "status" },
  { kind: "globe", params: { grouping: "asn" } },
  { kind: "globe", params: { grouping: "ip", source_node_id: "dev_pc01" } },
  { kind: "globe", params: { grouping: "country", direction: "inbound", min_bps: 1_000_000 } },
  { kind: "globe", params: { grouping: "city", protocol: "tcp", limit: 3 } },
  { kind: "destination", key: "asn:13335", params: {} },
  { kind: "destination", key: "asn:16509", params: { source_node_id: "dev_media" } },
  { kind: "home", params: {} },
  { kind: "home", params: { destination_key: "asn:13335" } },
  { kind: "home", params: { scope: "internal", vlan_id: 10, include_inactive: true } },
  { kind: "home", params: { focus_node_id: "dev_phone01", protocol: "tcp", min_bps: 100_000, limit: 5 } },
  { kind: "topology" },
  { kind: "device", id: "dev_pc01", grouping: "asn" },
  { kind: "device", id: "ep:10.30.0.77", grouping: "country" },
];

const LIGHT: Request[] = [
  { kind: "status" },
  { kind: "globe", params: { grouping: "asn" } },
  { kind: "globe", params: { grouping: "ip", limit: 20 } },
  { kind: "home", params: {} },
];

function run(b: MockBackend, r: Request): unknown {
  switch (r.kind) {
    case "status":
      return b.getStatus();
    case "globe":
      return b.getGlobe(r.params);
    case "destination":
      return b.getDestination(r.key, r.params);
    case "home":
      return b.getHomeTraffic(r.params);
    case "topology":
      return b.getTopology();
    case "device":
      return b.getDevice(r.id, r.grouping);
  }
}

function build() {
  const cases = [];
  for (const scenario of SCENARIO_NAMES) {
    const ticks = scenario === "stale-collector" ? [START_TICK + 10, START_TICK + 40] : [START_TICK, START_TICK + 17];
    for (const tick of ticks) {
      const b = new MockBackend({ seed: SEED, scenario, speed: 1, epochMs: EPOCH_MS });
      b.setTick(tick);
      const requests = scenario === "many-destinations" ? LIGHT : FULL;
      for (const request of requests) {
        cases.push({ scenario, tick, request, response: JSON.parse(JSON.stringify(run(b, request) ?? null)) });
      }
    }
  }
  return { epoch_ms: EPOCH_MS, seed: SEED, cases };
}

describe("golden fixtures", () => {
  it("are up to date with the TypeScript mock", () => {
    const current = build();
    if (process.env.UPDATE_GOLDEN) {
      writeFileSync(FILE, JSON.stringify(current) + "\n");
      return;
    }
    const stored = JSON.parse(readFileSync(FILE, "utf8"));
    expect(stored).toEqual(current);
  });
});
