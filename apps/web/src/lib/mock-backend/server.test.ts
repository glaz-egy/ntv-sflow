import { describe, expect, it } from "vitest";
import { MockEngine, WINDOW_SECONDS } from "./engine";
import { destinationNodeId, MockBackend } from "./server";
import { buildScenario, SCENARIO_NAMES, START_TICK, type ScenarioName } from "./scenarios";

const EPOCH = Date.UTC(2026, 9, 3, 14, 45, 0);

function backend(scenario: ScenarioName = "default", seed = 42, tick = START_TICK + 10) {
  const b = new MockBackend({ seed, scenario, speed: 1, epochMs: EPOCH });
  b.setTick(tick);
  return b;
}

describe("mock determinism", () => {
  it("produces identical snapshots for the same seed/scenario/tick", () => {
    expect(backend().getGlobe({ grouping: "asn" })).toEqual(backend().getGlobe({ grouping: "asn" }));
    expect(backend().getHomeTraffic({})).toEqual(backend().getHomeTraffic({}));
  });

  it("differs across seeds", () => {
    const a = backend("default", 1).getGlobe({ grouping: "ip" });
    const b = backend("default", 2).getGlobe({ grouping: "ip" });
    expect(a.destinations.map((d) => d.inbound_bps.value)).not.toEqual(
      b.destinations.map((d) => d.inbound_bps.value),
    );
  });

  it("changes over time (live)", () => {
    const a = backend("default", 42, START_TICK).getGlobe({ grouping: "asn" });
    const b = backend("default", 42, START_TICK + 3).getGlobe({ grouping: "asn" });
    expect(a.window.end).not.toBe(b.window.end);
    expect(a.destinations[0]!.inbound_bps.value).not.toBe(b.destinations[0]!.inbound_bps.value);
  });

  it("builds every documented scenario", () => {
    for (const name of SCENARIO_NAMES) {
      const g = backend(name).getGlobe({ grouping: "asn" });
      expect(g.destinations.length, name).toBeGreaterThan(0);
    }
  });
});

describe("sampling estimates", () => {
  it("tracks the true rate within statistical tolerance for large flows", () => {
    const scenario = buildScenario("default", 7);
    const engine = new MockEngine(scenario, 7);
    const end = START_TICK + 20;
    const conv = scenario.conversations.find((c) => c.id === "pc-nas-smb")!;
    let trueBytes = 0;
    for (let t = end - WINDOW_SECONDS + 1; t <= end; t++) trueBytes += engine.trueBytes({ conv, leg: "up" }, t);
    const est = engine
      .windowObservations(end)
      .find((o) => o.exporterId === "exp_core" && o.srcIp === conv.client && o.dstIp === conv.server)!;
    // estimated_bytes = Σ sampled_packet_length × sampling_rate
    expect(est.estimatedBytes).toBe(est.sampleCount * 600 * est.samplingRate);
    expect(Math.abs(est.estimatedBytes - trueBytes) / trueBytes).toBeLessThan(0.1);
  });

  it("labels every rate as a sampled estimate with window and sample count", () => {
    const d = backend().getGlobe({ grouping: "asn" }).destinations[0]!;
    expect(d.inbound_bps.measurement_kind).toBe("sampled_estimate");
    expect(d.inbound_bps.window_seconds).toBe(WINDOW_SECONDS);
    expect(d.inbound_bps.sample_count).toBeGreaterThan(0);
  });

  it("uses boundary counters for the WAN summary", () => {
    const s = backend().getGlobe({ grouping: "asn" }).summary;
    expect(s.basis).toBe("boundary_counter");
    expect(s.download.measurement_kind).toBe("counter");
    expect(s.download.value).toBeGreaterThan(0);
  });

  it("falls back to sampled sum before a second counter poll exists", () => {
    const s = backend("default", 42, 2).getGlobe({ grouping: "asn" }).summary;
    expect(s.basis).toBe("sampled_sum");
    expect(s.download.measurement_kind).toBe("sampled_estimate");
  });
});

describe("mock globe scenario requirements", () => {
  const g = backend().getGlobe({ grouping: "asn" });

  it("has 5–15 active destination groups", () => {
    expect(g.destinations.length).toBeGreaterThanOrEqual(5);
    expect(g.destinations.length).toBeLessThanOrEqual(15);
  });

  it("includes a destination with unknown location (no fake coordinate)", () => {
    const unknown = g.destinations.filter((d) => d.location === null);
    expect(unknown.length).toBeGreaterThan(0);
  });

  it("includes an ASN with multiple internal source devices", () => {
    expect(g.destinations.some((d) => d.source_device_count > 1)).toBe(true);
  });

  it("mixes inbound and outbound", () => {
    expect(g.destinations.some((d) => d.outbound_bps.value > d.inbound_bps.value)).toBe(true);
    expect(g.destinations.some((d) => d.inbound_bps.value > d.outbound_bps.value)).toBe(true);
  });

  it("includes IPv6 destinations", () => {
    const ips = backend().getGlobe({ grouping: "ip" }).destinations.map((d) => d.ip!);
    expect(ips.some((ip) => ip.includes(":"))).toBe(true);
  });

  it("caps destinations with limit and reports truncation", () => {
    const many = backend("many-destinations").getGlobe({ grouping: "ip", limit: 100 });
    expect(many.destinations).toHaveLength(100);
    expect(many.truncated_count).toBeGreaterThan(0);
  });
});

describe("cross-view consistency (one dataset, two projections)", () => {
  it("Globe destination sources equal Home destination-context sources", () => {
    const b = backend();
    for (const d of b.getGlobe({ grouping: "asn" }).destinations) {
      const home = b.getHomeTraffic({ destination_key: d.key });
      const globeSources = b.getDestination(d.key)!.top_sources.map((s) => s.node_id).sort();
      expect(home.destination_context!.source_node_ids, d.key).toEqual(globeSources);
    }
  });

  it("Globe destination rates equal the Home edges to that destination", () => {
    const b = backend();
    const d = b.getGlobe({ grouping: "asn" }).destinations.find((x) => x.key === "asn:13335")!;
    const home = b.getHomeTraffic({ destination_key: d.key });
    const toDest = home.edges.filter((e) => e.target === destinationNodeId(d.key));
    const out = toDest.reduce((s, e) => s + e.forward_bps.value, 0);
    const inb = toDest.reduce((s, e) => s + e.reverse_bps.value, 0);
    expect(out).toBeCloseTo(d.outbound_bps.value, 6);
    expect(inb).toBeCloseTo(d.inbound_bps.value, 6);
  });

  it("Globe source filter shows exactly the device's external destinations", () => {
    const b = backend();
    const filtered = b.getGlobe({ grouping: "asn", source_node_id: "dev_pc01" });
    const device = b.getDevice("dev_pc01", "asn")!;
    expect(filtered.destinations.map((d) => d.key).sort()).toEqual(
      device.top_external_destinations.map((d) => d.key).sort(),
    );
    for (const d of filtered.destinations) expect(d.source_node_ids).toEqual(["dev_pc01"]);
  });
});

describe("home traffic", () => {
  it("has PC → NAS as the largest internal edge", () => {
    const h = backend().getHomeTraffic({ scope: "internal" });
    expect(h.edges[0]!.id).toBe("e:dev_nas01~dev_pc01");
    // IPv4 and IPv6 sessions merge into one device-to-device edge.
    expect(h.edges.filter((e) => e.id === "e:dev_nas01~dev_pc01")).toHaveLength(1);
  });

  it("collapses external endpoints into the Internet node by default", () => {
    const h = backend().getHomeTraffic({});
    const external = h.edges.filter((e) => e.scope === "external");
    expect(external.length).toBeGreaterThan(0);
    expect(external.every((e) => e.target === "internet")).toBe(true);
  });

  it("focus keeps only edges touching the focused device", () => {
    const h = backend().getHomeTraffic({ focus_node_id: "dev_phone01" });
    expect(h.edges.every((e) => e.source === "dev_phone01" || e.target === "dev_phone01")).toBe(true);
  });

  it("shows unresolved endpoints as temporary identities", () => {
    const h = backend().getHomeTraffic({});
    const ep = h.nodes.find((n) => n.id === "ep:10.30.0.77")!;
    expect(ep.kind).toBe("unresolved_endpoint");
    expect(ep.device_type).toBe("unknown");
  });

  it("unknown-metadata scenario turns removed devices into unresolved endpoints", () => {
    const h = backend("unknown-metadata").getHomeTraffic({});
    expect(h.nodes.some((n) => n.id === "ep:10.40.0.21")).toBe(true);
    expect(h.nodes.some((n) => n.id === "dev_laptop01")).toBe(false);
  });
});

describe("topology", () => {
  it("keeps inferred links distinguishable", () => {
    const t = backend().getTopology();
    const media = t.links.find((l) => l.target_node_id === "dev_media")!;
    expect(media.evidence).toBe("inferred");
    expect(media.confidence).toBeLessThan(1);
  });

  it("does not fabricate topology for unresolved endpoints", () => {
    const t = backend().getTopology();
    expect(t.links.some((l) => l.target_node_id.startsWith("ep:"))).toBe(false);
  });
});

describe("stale-collector scenario", () => {
  it("stops advancing the window and reports stale", () => {
    const early = backend("stale-collector", 42, START_TICK + 5).getStatus();
    expect(early.collector.status).toBe("connected");
    const late = backend("stale-collector", 42, START_TICK + 40);
    const s = late.getStatus();
    expect(s.collector.status).toBe("stale");
    expect(s.live).toBe(false);
    expect(Date.parse(s.server_time) - Date.parse(s.last_aggregate_at!)).toBe(20_000);
  });
});
