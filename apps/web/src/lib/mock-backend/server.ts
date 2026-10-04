/**
 * MockBackend — an in-process stand-in for the Go API (docs/API.md).
 *
 * It answers the same queries with the same DTOs the HTTP API will, from a
 * single attributed-flow set per window. Globe and Home are therefore always
 * consistent: they are two projections of one dataset (docs/MOCK_DATA.md §10).
 */
import { compareStrings } from "./strings";
import type {
  DestinationKey,
  DestinationMember,
  DestinationSource,
  DeviceDetail,
  DeviceExternalDestination,
  DevicePeer,
  GlobeDestination,
  GlobeDestinationDetail,
  GlobeQuery,
  GlobeResponse,
  Grouping,
  HomeNode,
  HomeTrafficQuery,
  HomeTrafficResponse,
  Measurement,
  ObservationPoint,
  ProtocolShare,
  StatusResponse,
  TopologyResponse,
  TrafficEdge,
  WanSummary,
} from "@/contracts";
import type { Device, Inventory } from "./domain";
import { MockEngine, WINDOW_SECONDS, COUNTER_INTERVAL_SECONDS } from "./engine";
import { INTERNET_NODE_ID } from "./inventory";
import { CidrClassifier } from "./net/ip";
import { attributeFlows, DeviceRegistry, type AttributedFlow } from "./pipeline/attribute";
import { counterRate } from "./pipeline/counters";
import {
  destinationKey,
  destinationLabel,
  groupLocation,
  parseDestinationKey,
} from "./pipeline/destinations";
import { buildScenario, START_TICK, type ScenarioName } from "./scenarios";

export interface MockBackendOptions {
  seed: number;
  scenario: ScenarioName;
  speed: number;
  /** Wall-clock time (ms) corresponding to sim tick 0. */
  epochMs: number;
}

export const DEFAULT_GLOBE_LIMIT = 100;
export const DEFAULT_HOME_EDGE_LIMIT = 250;

const SERVICES: Record<string, string> = {
  "tcp/443": "https",
  "udp/443": "quic",
  "tcp/445": "smb",
  "tcp/2049": "nfs",
  "tcp/22": "ssh",
  "tcp/8883": "mqtts",
  "tcp/32400": "plex",
  "tcp/873": "rsync",
  "tcp/8443": "https-alt",
  "udp/123": "ntp",
  "udp/53": "dns",
};

export function destinationNodeId(key: DestinationKey): string {
  return `dst:${key}`;
}

interface Frame {
  tick: number;
  windowEndTick: number;
  flows: AttributedFlow[];
}

export class MockBackend {
  readonly options: MockBackendOptions;
  private readonly engine: MockEngine;
  private readonly inventory: Inventory;
  private readonly registry: DeviceRegistry;
  private readonly classifier: CidrClassifier;
  private readonly networks: Array<{ name: string; vlanId: number | null; classifier: CidrClassifier }>;
  private tick = START_TICK;
  private frameCache: Frame | null = null;

  constructor(options: MockBackendOptions) {
    this.options = options;
    const scenario = buildScenario(options.scenario, options.seed);
    this.engine = new MockEngine(scenario, options.seed);
    this.inventory = scenario.inventory;
    this.registry = new DeviceRegistry(scenario.inventory.devices);
    this.classifier = new CidrClassifier(scenario.inventory.internalCidrs);
    this.networks = scenario.inventory.networks.map((n) => ({
      name: n.name,
      vlanId: n.vlanId,
      classifier: new CidrClassifier([n.cidr]),
    }));
  }

  get currentTick(): number {
    return this.tick;
  }

  setTick(tick: number): void {
    this.tick = Math.max(0, Math.floor(tick));
  }

  get scenarioDescription(): string {
    return this.engine.scenario.description;
  }

  // ---------------------------------------------------------------- helpers

  private iso(tick: number): string {
    return new Date(this.options.epochMs + tick * 1000).toISOString();
  }

  private frame(): Frame {
    if (this.frameCache && this.frameCache.tick === this.tick) return this.frameCache;
    const windowEndTick = this.engine.effectiveTick(this.tick);
    if (this.frameCache && this.frameCache.windowEndTick === windowEndTick) {
      this.frameCache = { ...this.frameCache, tick: this.tick };
      return this.frameCache;
    }
    const flows = attributeFlows(this.engine.windowObservations(this.tick), {
      classifier: this.classifier,
      registry: this.registry,
      geo: this.engine.scenario.geo,
      exporters: this.inventory.exporters,
      policy: this.inventory.observationPolicy,
    });
    this.frameCache = { tick: this.tick, windowEndTick, flows };
    return this.frameCache;
  }

  private window(frame: Frame) {
    return {
      start: this.iso(frame.windowEndTick - WINDOW_SECONDS),
      end: this.iso(frame.windowEndTick),
    };
  }

  private sampled(bytes: number, samples: number): Measurement {
    return {
      value: (bytes * 8) / WINDOW_SECONDS,
      unit: "bps",
      measurement_kind: "sampled_estimate",
      window_seconds: WINDOW_SECONDS,
      sample_count: samples,
    };
  }

  private counter(bps: number, interval: number): Measurement {
    return { value: bps, unit: "bps", measurement_kind: "counter", interval_seconds: interval };
  }

  private networkOf(addresses: string[]) {
    for (const a of addresses) {
      const n = this.networks.find((x) => x.classifier.isInternal(a));
      if (n) return n;
    }
    return null;
  }

  private nodeTraffic(frame: Frame) {
    const t = new Map<string, { rx: number; rxS: number; tx: number; txS: number; last: number | null }>();
    const add = (id: string | null, kind: "rx" | "tx", f: AttributedFlow) => {
      if (!id) return;
      const e = t.get(id) ?? { rx: 0, rxS: 0, tx: 0, txS: 0, last: null };
      if (kind === "rx") {
        e.rx += f.estimatedBytes;
        e.rxS += f.sampleCount;
      } else {
        e.tx += f.estimatedBytes;
        e.txS += f.sampleCount;
      }
      if (f.lastSeenAt !== null && (e.last === null || f.lastSeenAt > e.last)) e.last = f.lastSeenAt;
      t.set(id, e);
    };
    for (const f of frame.flows) {
      if (f.scope === "transit") continue;
      add(f.srcNodeId, "tx", f);
      add(f.dstNodeId, "rx", f);
      if (f.scope === "external") {
        // Internet aggregate: receives outbound, sends inbound.
        add(INTERNET_NODE_ID, f.direction === "outbound" ? "rx" : "tx", f);
      }
    }
    return t;
  }

  private deviceNode(device: Device, traffic: ReturnType<MockBackend["nodeTraffic"]>): HomeNode {
    const addresses = device.addresses.map((a) => a.address);
    const net = this.networkOf(addresses);
    const tr = traffic.get(device.id);
    return {
      id: device.id,
      kind: "device",
      label: device.displayName,
      device_type: device.type,
      status: device.online ? "online" : "offline",
      addresses,
      vlan_id: device.vlanId ?? net?.vlanId ?? null,
      network_name: net?.name ?? null,
      rx_bps: tr ? this.sampled(tr.rx, tr.rxS) : null,
      tx_bps: tr ? this.sampled(tr.tx, tr.txS) : null,
      last_seen: tr?.last != null ? this.iso(tr.last) : null,
    };
  }

  private nodeFor(id: string, traffic: ReturnType<MockBackend["nodeTraffic"]>, labelHint?: string): HomeNode {
    const tr = traffic.get(id);
    const rates = {
      rx_bps: tr ? this.sampled(tr.rx, tr.rxS) : null,
      tx_bps: tr ? this.sampled(tr.tx, tr.txS) : null,
      last_seen: tr?.last != null ? this.iso(tr.last) : null,
    };
    const device = this.registry.get(id);
    if (device) return this.deviceNode(device, traffic);
    if (id === INTERNET_NODE_ID) {
      return {
        id,
        kind: "internet",
        label: "Internet",
        device_type: "internet",
        status: "unknown",
        addresses: [],
        vlan_id: null,
        network_name: null,
        ...rates,
      };
    }
    if (id.startsWith("dst:")) {
      return {
        id,
        kind: "external_destination",
        label: labelHint ?? id.slice(4),
        device_type: "internet",
        status: "unknown",
        addresses: [],
        vlan_id: null,
        network_name: null,
        rx_bps: null,
        tx_bps: null,
        last_seen: null,
      };
    }
    // Unresolved internal endpoint: temporary identity, no fabricated metadata.
    const ip = id.startsWith("ep:") ? id.slice(3) : id;
    const net = this.networkOf([ip]);
    return {
      id,
      kind: "unresolved_endpoint",
      label: ip,
      device_type: "unknown",
      status: "unknown",
      addresses: [ip],
      vlan_id: net?.vlanId ?? null,
      network_name: net?.name ?? null,
      ...rates,
    };
  }

  private labelForNode(id: string): { label: string; type: HomeNode["device_type"]; resolved: boolean } {
    const d = this.registry.get(id);
    if (d) return { label: d.displayName, type: d.type, resolved: true };
    return { label: id.startsWith("ep:") ? id.slice(3) : id, type: "unknown", resolved: false };
  }

  private wanSummary(frame: Frame): WanSummary {
    const { previous, current } = this.engine.wanCounterReadings(this.tick);
    const exporter = this.engine.boundaryExporter;
    const prevIn = previous && { at: previous.at, octets: previous.inOctets, width: 64 as const };
    const prevOut = previous && { at: previous.at, octets: previous.outOctets, width: 64 as const };
    const opts = { maxIntervalSeconds: COUNTER_INTERVAL_SECONDS * 3, ifSpeedBps: exporter.ifSpeedBps };
    const rIn = counterRate(prevIn, { at: current.at, octets: current.inOctets, width: 64 }, opts);
    const rOut = counterRate(prevOut, { at: current.at, octets: current.outOctets, width: 64 }, opts);
    if (rIn.ok && rOut.ok) {
      return {
        download: this.counter(rIn.bps, rIn.intervalSeconds),
        upload: this.counter(rOut.bps, rOut.intervalSeconds),
        basis: "boundary_counter",
      };
    }
    // Fallback: de-duplicated sampled estimates for external flows.
    let inB = 0, inS = 0, outB = 0, outS = 0;
    for (const f of frame.flows) {
      if (f.scope !== "external") continue;
      if (f.direction === "inbound") {
        inB += f.estimatedBytes;
        inS += f.sampleCount;
      } else {
        outB += f.estimatedBytes;
        outS += f.sampleCount;
      }
    }
    return { download: this.sampled(inB, inS), upload: this.sampled(outB, outS), basis: "sampled_sum" };
  }

  private observationPoints(flows: AttributedFlow[]): ObservationPoint[] {
    const exporters = new Map(this.inventory.exporters.map((e) => [e.id, e]));
    const points = new Map<string, ObservationPoint>();
    for (const f of flows) {
      for (const o of f.observations) {
        const existing = points.get(o.exporterId);
        const used = f.used.exporterId === o.exporterId;
        if (existing) {
          existing.used_for_aggregate ||= used;
          continue;
        }
        points.set(o.exporterId, {
          exporter_id: o.exporterId,
          exporter_name: exporters.get(o.exporterId)?.name ?? o.exporterId,
          input_if_index: o.inputIfIndex,
          output_if_index: o.outputIfIndex,
          used_for_aggregate: used,
          sampling_rate: o.samplingRate,
        });
      }
    }
    return [...points.values()].sort((a, b) => compareStrings(a.exporter_id, b.exporter_id));
  }

  private protocolShares(flows: AttributedFlow[], limit = 5): ProtocolShare[] {
    const groups = new Map<string, { f: AttributedFlow; bytes: number; samples: number }>();
    for (const f of flows) {
      const k = `${f.protocol}/${f.servicePort ?? ""}`;
      const g = groups.get(k) ?? { f, bytes: 0, samples: 0 };
      g.bytes += f.estimatedBytes;
      g.samples += f.sampleCount;
      groups.set(k, g);
    }
    return [...groups.entries()]
      .sort(([ka, a], [kb, b]) => b.bytes - a.bytes || compareStrings(ka, kb))
      .slice(0, limit)
      .map(([k, g]) => ({
        protocol: g.f.protocol,
        port: g.f.servicePort,
        service: SERVICES[k] ?? null,
        bps: this.sampled(g.bytes, g.samples),
      }));
  }

  private externalFlows(frame: Frame, sourceNodeId?: string | null, protocol?: string | null) {
    return frame.flows.filter(
      (f) =>
        f.scope === "external" &&
        (!sourceNodeId || f.internalNodeId === sourceNodeId) &&
        (!protocol || f.protocol === protocol),
    );
  }

  private matchesDestination(f: AttributedFlow, key: DestinationKey): boolean {
    const parsed = parseDestinationKey(key);
    if (!parsed || !f.externalIp) return false;
    return destinationKey(parsed.grouping, f.externalIp, f.geo) === key;
  }

  private destinationLabelForKey(frame: Frame, key: DestinationKey): string {
    const parsed = parseDestinationKey(key);
    if (!parsed) return key;
    const f = frame.flows.find((x) => this.matchesDestination(x, key));
    if (f?.externalIp) return destinationLabel(parsed.grouping, f.externalIp, f.geo);
    if (parsed.grouping === "asn" && parsed.value !== "unknown") return `AS${parsed.value}`;
    return parsed.value;
  }

  private buildDestinations(flows: AttributedFlow[], grouping: Grouping): GlobeDestination[] {
    interface Acc {
      key: string;
      flows: AttributedFlow[];
      inB: number;
      inS: number;
      outB: number;
      outS: number;
      sources: Map<string, number>;
      last: number | null;
    }
    const groups = new Map<string, Acc>();
    for (const f of flows) {
      const key = destinationKey(grouping, f.externalIp!, f.geo);
      const g =
        groups.get(key) ??
        ({ key, flows: [], inB: 0, inS: 0, outB: 0, outS: 0, sources: new Map(), last: null } as Acc);
      g.flows.push(f);
      if (f.direction === "inbound") {
        g.inB += f.estimatedBytes;
        g.inS += f.sampleCount;
      } else {
        g.outB += f.estimatedBytes;
        g.outS += f.sampleCount;
      }
      if (f.internalNodeId) {
        g.sources.set(f.internalNodeId, (g.sources.get(f.internalNodeId) ?? 0) + f.estimatedBytes);
      }
      if (f.lastSeenAt !== null && (g.last === null || f.lastSeenAt > g.last)) g.last = f.lastSeenAt;
      groups.set(key, g);
    }

    const same = <T,>(values: T[]): T | null =>
      values.length > 0 && values.every((v) => v === values[0]) ? values[0]! : null;

    return [...groups.values()].map((g) => {
      const first = g.flows[0]!;
      const geos = g.flows.map((f) => f.geo);
      const countryCode = same(geos.map((x) => x.countryCode));
      return {
        key: g.key,
        grouping,
        label: destinationLabel(grouping, first.externalIp!, first.geo),
        ip: grouping === "ip" ? first.externalIp : null,
        country_code: countryCode,
        country_name: countryCode ? same(geos.map((x) => x.countryName)) : null,
        city: same(geos.map((x) => x.city)),
        location: groupLocation(
          grouping,
          g.flows.map((f) => ({ geo: f.geo, weight: f.estimatedBytes })),
        ),
        asn: same(geos.map((x) => x.asn)),
        organization: same(geos.map((x) => x.organization)),
        inbound_bps: this.sampled(g.inB, g.inS),
        outbound_bps: this.sampled(g.outB, g.outS),
        source_device_count: g.sources.size,
        source_node_ids: [...g.sources.entries()]
          .sort(([a, x], [b, y]) => y - x || compareStrings(a, b))
          .slice(0, 10)
          .map(([id]) => id),
        last_seen: this.iso(g.last ?? this.engine.effectiveTick(this.tick)),
      };
    });
  }

  // ------------------------------------------------------------- endpoints

  getStatus(): StatusResponse {
    const frame = this.frame();
    const stale = this.engine.isStale(this.tick);
    return {
      mode: "mock",
      live: !stale,
      collector: {
        status: stale ? "stale" : "connected",
        last_datagram_at: this.iso(frame.windowEndTick),
      },
      last_aggregate_at: this.iso(frame.windowEndTick),
      server_time: this.iso(this.tick),
      update_interval_seconds: 1,
      window_seconds: WINDOW_SECONDS,
      mock: { seed: this.options.seed, scenario: this.options.scenario, speed: this.options.speed },
    };
  }

  getGlobe(q: GlobeQuery): GlobeResponse {
    const frame = this.frame();
    const direction = q.direction ?? "both";
    const minBps = q.min_bps ?? 0;
    const limit = q.limit ?? DEFAULT_GLOBE_LIMIT;
    const rank = (d: GlobeDestination) =>
      direction === "inbound"
        ? d.inbound_bps.value
        : direction === "outbound"
          ? d.outbound_bps.value
          : d.inbound_bps.value + d.outbound_bps.value;

    const all = this.buildDestinations(this.externalFlows(frame, q.source_node_id, q.protocol), q.grouping)
      .filter((d) => rank(d) > 0 && rank(d) >= minBps)
      .sort((a, b) => rank(b) - rank(a) || compareStrings(a.key, b.key));

    return {
      window: this.window(frame),
      server_time: this.iso(this.tick),
      origin: this.inventory.origin,
      grouping: q.grouping,
      destinations: all.slice(0, limit),
      truncated_count: Math.max(0, all.length - limit),
      summary: this.wanSummary(frame),
    };
  }

  getDestination(key: DestinationKey, q: Omit<GlobeQuery, "grouping"> = {}): GlobeDestinationDetail | null {
    const parsed = parseDestinationKey(key);
    if (!parsed) return null;
    const frame = this.frame();
    const flows = this.externalFlows(frame, q.source_node_id, q.protocol).filter((f) =>
      this.matchesDestination(f, key),
    );
    if (flows.length === 0) return null;
    const destination = this.buildDestinations(flows, parsed.grouping)[0]!;

    const sources = new Map<string, { inB: number; inS: number; outB: number; outS: number }>();
    const members = new Map<string, { f: AttributedFlow; bytes: number; samples: number }>();
    for (const f of flows) {
      const id = f.internalNodeId!;
      const s = sources.get(id) ?? { inB: 0, inS: 0, outB: 0, outS: 0 };
      if (f.direction === "inbound") {
        s.inB += f.estimatedBytes;
        s.inS += f.sampleCount;
      } else {
        s.outB += f.estimatedBytes;
        s.outS += f.sampleCount;
      }
      sources.set(id, s);
      const m = members.get(f.externalIp!) ?? { f, bytes: 0, samples: 0 };
      m.bytes += f.estimatedBytes;
      m.samples += f.sampleCount;
      members.set(f.externalIp!, m);
    }

    const top_sources: DestinationSource[] = [...sources.entries()]
      .sort(([a, x], [b, y]) => y.inB + y.outB - (x.inB + x.outB) || compareStrings(a, b))
      .map(([id, s]) => {
        const info = this.labelForNode(id);
        return {
          node_id: id,
          label: info.label,
          device_type: info.type,
          resolved: info.resolved,
          inbound_bps: this.sampled(s.inB, s.inS),
          outbound_bps: this.sampled(s.outB, s.outS),
        };
      });

    const memberList: DestinationMember[] = [...members.entries()]
      .sort(([a, x], [b, y]) => y.bytes - x.bytes || compareStrings(a, b))
      .slice(0, 10)
      .map(([ip, m]) => ({
        ip,
        city: m.f.geo.city,
        country_code: m.f.geo.countryCode,
        total_bps: this.sampled(m.bytes, m.samples),
      }));

    return {
      destination,
      top_sources,
      top_protocols: this.protocolShares(flows),
      members: memberList,
      observation_points: this.observationPoints(flows),
    };
  }

  getHomeTraffic(q: HomeTrafficQuery = {}): HomeTrafficResponse {
    const frame = this.frame();
    const traffic = this.nodeTraffic(frame);
    const scope = q.scope ?? "both";
    const minBps = q.min_bps ?? 0;
    const limit = q.limit ?? DEFAULT_HOME_EDGE_LIMIT;
    const destKey = q.destination_key ?? null;
    const destNode = destKey ? destinationNodeId(destKey) : null;

    const nodeCache = new Map<string, HomeNode>();
    const node = (id: string) => {
      let n = nodeCache.get(id);
      if (!n) {
        n = this.nodeFor(id, traffic, id === destNode && destKey ? this.destinationLabelForKey(frame, destKey) : undefined);
        nodeCache.set(id, n);
      }
      return n;
    };
    const internalNodeMatches = (id: string) => {
      const n = node(id);
      if (q.vlan_id != null && n.vlan_id !== q.vlan_id) return false;
      if (q.device_types && q.device_types.length > 0 && !q.device_types.includes(n.device_type)) return false;
      return true;
    };

    interface EdgeAcc {
      source: string;
      target: string;
      scope: "internal" | "external";
      fwdB: number;
      fwdS: number;
      revB: number;
      revS: number;
      flows: AttributedFlow[];
      last: number | null;
    }
    const edges = new Map<string, EdgeAcc>();
    const sourceIds = new Set<string>();

    for (const f of frame.flows) {
      if (f.scope === "transit") continue;
      if (q.protocol && f.protocol !== q.protocol) continue;
      if (scope !== "both" && f.scope !== scope) continue;

      let a: string;
      let b: string;
      let forward: boolean;
      if (f.scope === "internal") {
        const [x, y] = [f.srcNodeId!, f.dstNodeId!].sort();
        a = x!;
        b = y!;
        forward = f.srcNodeId === a;
        if (!internalNodeMatches(a) && !internalNodeMatches(b)) continue;
      } else {
        const internal = f.internalNodeId!;
        const matchesDest = destKey !== null && this.matchesDestination(f, destKey);
        if (matchesDest) sourceIds.add(internal);
        a = internal;
        b = matchesDest ? destNode! : INTERNET_NODE_ID;
        forward = f.direction === "outbound";
        if (!internalNodeMatches(a)) continue;
      }
      if (q.focus_node_id && a !== q.focus_node_id && b !== q.focus_node_id) continue;

      const id = `e:${a}~${b}`;
      const e =
        edges.get(id) ??
        ({ source: a, target: b, scope: f.scope, fwdB: 0, fwdS: 0, revB: 0, revS: 0, flows: [], last: null } as EdgeAcc);
      if (forward) {
        e.fwdB += f.estimatedBytes;
        e.fwdS += f.sampleCount;
      } else {
        e.revB += f.estimatedBytes;
        e.revS += f.sampleCount;
      }
      e.flows.push(f);
      if (f.lastSeenAt !== null && (e.last === null || f.lastSeenAt > e.last)) e.last = f.lastSeenAt;
      edges.set(id, e);
    }

    const total = (e: EdgeAcc) => ((e.fwdB + e.revB) * 8) / WINDOW_SECONDS;
    const ranked = [...edges.entries()]
      .filter(([, e]) => total(e) >= minBps && total(e) > 0)
      .sort(([ia, a], [ib, b]) => total(b) - total(a) || compareStrings(ia, ib));
    const kept = ranked.slice(0, limit);

    const edgeDtos: TrafficEdge[] = kept.map(([id, e]) => ({
      id,
      source: e.source,
      target: e.target,
      scope: e.scope,
      forward_bps: this.sampled(e.fwdB, e.fwdS),
      reverse_bps: this.sampled(e.revB, e.revS),
      top_protocols: this.protocolShares(e.flows, 3),
      observation_points: this.observationPoints(e.flows),
      last_seen: this.iso(e.last ?? frame.windowEndTick),
    }));

    const nodeIds = new Set<string>();
    for (const e of edgeDtos) {
      nodeIds.add(e.source);
      nodeIds.add(e.target);
    }
    if (q.focus_node_id) nodeIds.add(q.focus_node_id);
    if (destNode) nodeIds.add(destNode);
    if (q.include_inactive) {
      for (const d of this.registry.devices) if (internalNodeMatches(d.id)) nodeIds.add(d.id);
      nodeIds.add(INTERNET_NODE_ID);
    }

    let internalB = 0;
    let internalS = 0;
    for (const f of frame.flows) {
      if (f.scope !== "internal") continue;
      internalB += f.estimatedBytes;
      internalS += f.sampleCount;
    }
    const wan = this.wanSummary(frame);
    const wanTotal: Measurement = { ...wan.download, value: wan.download.value + wan.upload.value };
    if (wan.download.sample_count !== undefined) {
      wanTotal.sample_count = wan.download.sample_count + (wan.upload.sample_count ?? 0);
    }

    return {
      window: this.window(frame),
      server_time: this.iso(this.tick),
      nodes: [...nodeIds].sort().map(node),
      edges: edgeDtos,
      truncated_count: ranked.length - kept.length,
      destination_context: destKey
        ? {
            key: destKey,
            label: this.destinationLabelForKey(frame, destKey),
            source_node_ids: [...sourceIds].sort(),
          }
        : null,
      summary: {
        wan_total: wanTotal,
        internal_estimated: this.sampled(internalB, internalS),
        device_count: this.registry.devices.length,
        online_count: this.registry.devices.filter((d) => d.online).length,
      },
    };
  }

  getTopology(): TopologyResponse {
    const frame = this.frame();
    const traffic = this.nodeTraffic(frame);
    const ids = new Set<string>([INTERNET_NODE_ID, ...this.registry.devices.map((d) => d.id)]);
    const links = this.inventory.topology
      .filter((l) => ids.has(l.a) && ids.has(l.b))
      .map((l) => ({
        id: l.id,
        source_node_id: l.a,
        target_node_id: l.b,
        link_type: l.linkType,
        evidence: l.evidence,
        confidence: l.confidence,
        source_interface: l.aInterface,
        target_interface: l.bInterface,
      }));
    return {
      nodes: [...ids].sort().map((id) => this.nodeFor(id, traffic)),
      links,
      generated_at: this.iso(this.tick),
    };
  }

  getDevice(id: string, grouping: Grouping = "asn"): DeviceDetail | null {
    const frame = this.frame();
    const traffic = this.nodeTraffic(frame);
    const device = this.registry.get(id);
    const seen = frame.flows.some((f) => f.srcNodeId === id || f.dstNodeId === id);
    if (!device && !(id.startsWith("ep:") && seen)) return null;
    const node = this.nodeFor(id, traffic);

    const peers = new Map<string, { txB: number; txS: number; rxB: number; rxS: number }>();
    const dests = new Map<string, { f: AttributedFlow; outB: number; outS: number; inB: number; inS: number }>();
    for (const f of frame.flows) {
      if (f.scope === "internal" && (f.srcNodeId === id || f.dstNodeId === id)) {
        const peer = f.srcNodeId === id ? f.dstNodeId! : f.srcNodeId!;
        const p = peers.get(peer) ?? { txB: 0, txS: 0, rxB: 0, rxS: 0 };
        if (f.srcNodeId === id) {
          p.txB += f.estimatedBytes;
          p.txS += f.sampleCount;
        } else {
          p.rxB += f.estimatedBytes;
          p.rxS += f.sampleCount;
        }
        peers.set(peer, p);
      } else if (f.scope === "external" && f.internalNodeId === id) {
        const key = destinationKey(grouping, f.externalIp!, f.geo);
        const d = dests.get(key) ?? { f, outB: 0, outS: 0, inB: 0, inS: 0 };
        if (f.direction === "outbound") {
          d.outB += f.estimatedBytes;
          d.outS += f.sampleCount;
        } else {
          d.inB += f.estimatedBytes;
          d.inS += f.sampleCount;
        }
        dests.set(key, d);
      }
    }

    const top_internal_peers: DevicePeer[] = [...peers.entries()]
      .sort(([a, x], [b, y]) => y.txB + y.rxB - (x.txB + x.rxB) || compareStrings(a, b))
      .slice(0, 10)
      .map(([peerId, p]) => {
        const info = this.labelForNode(peerId);
        return {
          node_id: peerId,
          label: info.label,
          device_type: info.type,
          tx_bps: this.sampled(p.txB, p.txS),
          rx_bps: this.sampled(p.rxB, p.rxS),
        };
      });

    const top_external_destinations: DeviceExternalDestination[] = [...dests.entries()]
      .sort(([a, x], [b, y]) => y.outB + y.inB - (x.outB + x.inB) || compareStrings(a, b))
      .slice(0, 10)
      .map(([key, d]) => ({
        key,
        label: destinationLabel(grouping, d.f.externalIp!, d.f.geo),
        country_code: d.f.geo.countryCode,
        outbound_bps: this.sampled(d.outB, d.outS),
        inbound_bps: this.sampled(d.inB, d.inS),
      }));

    const link = this.inventory.topology.find((l) => l.b === id);
    const via = link ? this.labelForNode(link.a) : null;

    return {
      node,
      addresses: device
        ? device.addresses
        : [{ address: node.addresses[0]!, family: node.addresses[0]!.includes(":") ? "ipv6" : "ipv4", source: "flow" }],
      macs: device?.macs ?? [],
      ssid: device?.ssid ?? null,
      attachment:
        link && via
          ? {
              via_node_id: link.a,
              via_label: link.a === INTERNET_NODE_ID ? "Internet" : via.label,
              interface: link.aInterface,
              evidence: link.evidence,
            }
          : null,
      vendor: device?.vendor ?? null,
      model: device?.model ?? null,
      top_internal_peers,
      top_external_destinations,
    };
  }
}
