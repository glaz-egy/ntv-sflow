/**
 * Deterministic mock traffic engine.
 *
 * Each conversation has a smooth "true" rate. Every exporter that would see
 * the traffic independently *samples* it each second (Poisson sample counts
 * at that exporter's 1:N rate), producing estimates the same way a real
 * sFlow pipeline would: estimated_bytes = Σ sampled_packet_length × N.
 *
 * Output is a pure function of (scenario, seed, tick).
 */
import type { Exporter, InterfaceCounterReading, WindowFlowObservation } from "./domain";
import { CORE_PORTS, MOCK_DEVICES, MOCK_TOPOLOGY } from "./inventory";
import { CidrClassifier, parseIp } from "./net/ip";
import { keyedRng, keyedUniform, poisson, smoothNoise } from "./rng";
import type { Conversation, RateProfile, Scenario } from "./scenarios";

export const WINDOW_SECONDS = 5;
export const COUNTER_INTERVAL_SECONDS = 5;

type Leg = "up" | "down";

interface LegPlan {
  conv: Conversation;
  leg: Leg;
  srcIp: string;
  dstIp: string;
  srcPort: number;
  dstPort: number;
  packetBytes: number;
  observers: Array<{ exporter: Exporter; inputIfIndex: number | null; outputIfIndex: number | null }>;
  /** +1 = WAN inbound, -1 = WAN outbound, 0 = not crossing the boundary. */
  wan: 1 | -1 | 0;
}

const ROUTER_WAN_IF = 1;
const ROUTER_LAN_IF = 2;

/** Background WAN traffic not represented by conversations (ARP, DNS, ...). */
const BACKGROUND_BPS = 400_000;

function canonicalIp(ip: string): string {
  const p = parseIp(ip);
  return p ? `${p.family}:${p.value.toString(16)}` : ip;
}

/** Core-switch port facing a device, following known topology upward. */
function corePortForIp(ip: string): number | null {
  const key = canonicalIp(ip);
  const dev = MOCK_DEVICES.find((d) => d.addresses.some((a) => canonicalIp(a.address) === key));
  if (!dev) return null;
  let current = dev.id;
  for (let hops = 0; hops < 8; hops++) {
    if (CORE_PORTS[current] !== undefined) return CORE_PORTS[current]!;
    const parent = MOCK_TOPOLOGY.find((l) => l.b === current);
    if (!parent) return null;
    if (parent.a === "dev_core_sw") return CORE_PORTS[current] ?? null;
    current = parent.a;
  }
  return null;
}

export function profileFactor(seed: number, key: string, profile: RateProfile, t: number): number {
  let gate = 1;
  if (profile.shape === "burst" || profile.shape === "periodic") {
    const period = profile.periodSeconds ?? 60;
    const duty = profile.duty ?? 0.3;
    const phase = Math.floor(keyedUniform(seed, key, "phase") * period);
    const pos = (((t + phase) % period) + period) % period;
    const on = pos < duty * period;
    gate = on ? 1 : profile.shape === "burst" ? 0.02 : 0;
  }
  const jitter = profile.jitter ?? 0;
  return gate * Math.max(0, 1 + jitter * smoothNoise(seed, key, t));
}

export class MockEngine {
  readonly scenario: Scenario;
  readonly seed: number;
  private readonly plans: LegPlan[];
  private readonly router: Exporter;
  private readonly secondCache = new Map<number, WindowFlowObservation[]>();
  private readonly cumulative: Array<{ in: number; out: number }> = [];

  constructor(scenario: Scenario, seed: number) {
    this.scenario = scenario;
    this.seed = seed;
    const classifier = new CidrClassifier(scenario.inventory.internalCidrs);
    const exporters = scenario.inventory.exporters;
    const router = exporters.find((e) => e.role === "boundary");
    const core = exporters.find((e) => e.role === "core");
    if (!router || !core) throw new Error("mock inventory requires boundary and core exporters");
    this.router = router;

    this.plans = [];
    for (const conv of scenario.conversations) {
      for (const leg of ["up", "down"] as const) {
        const srcIp = leg === "up" ? conv.client : conv.server;
        const dstIp = leg === "up" ? conv.server : conv.client;
        const srcInternal = classifier.isInternal(srcIp) === true;
        const dstInternal = classifier.isInternal(dstIp) === true;
        const wan: LegPlan["wan"] = srcInternal && !dstInternal ? -1 : !srcInternal && dstInternal ? 1 : 0;
        const srcCore = srcInternal ? corePortForIp(srcIp) : CORE_PORTS.dev_router!;
        const dstCore = dstInternal ? corePortForIp(dstIp) : CORE_PORTS.dev_router!;
        const observers: LegPlan["observers"] = [
          { exporter: core, inputIfIndex: srcCore, outputIfIndex: dstCore },
        ];
        if (wan !== 0) {
          observers.push({
            exporter: router,
            inputIfIndex: wan === 1 ? ROUTER_WAN_IF : ROUTER_LAN_IF,
            outputIfIndex: wan === 1 ? ROUTER_LAN_IF : ROUTER_WAN_IF,
          });
        }
        this.plans.push({
          conv,
          leg,
          srcIp,
          dstIp,
          srcPort: leg === "up" ? conv.clientPort : conv.serverPort,
          dstPort: leg === "up" ? conv.serverPort : conv.clientPort,
          packetBytes: conv.packetBytes?.[leg] ?? (leg === "down" ? 1350 : 600),
          observers,
          wan,
        });
      }
    }
  }

  /** Last sim second for which the collector delivered data. */
  effectiveTick(tick: number): number {
    const stop = this.scenario.dataStopsAtTick;
    return stop === null ? tick : Math.min(tick, stop);
  }

  isStale(tick: number): boolean {
    const stop = this.scenario.dataStopsAtTick;
    return stop !== null && tick > stop;
  }

  /** True bytes transferred by one leg during sim second `t`. */
  trueBytes(plan: Pick<LegPlan, "conv" | "leg">, t: number): number {
    const profile = plan.conv[plan.leg];
    const factor = profileFactor(this.seed, `${plan.conv.id}/${plan.leg}`, profile, t);
    return (profile.bps * factor) / 8;
  }

  private sampleSecond(t: number): WindowFlowObservation[] {
    const cached = this.secondCache.get(t);
    if (cached) return cached;
    const out: WindowFlowObservation[] = [];
    for (const plan of this.plans) {
      const bytes = this.trueBytes(plan, t);
      const packets = bytes / plan.packetBytes;
      for (const obs of plan.observers) {
        const n = obs.exporter.samplingRate;
        const rng = keyedRng(this.seed, plan.conv.id, plan.leg, obs.exporter.id, t);
        const samples = poisson(rng, packets / n);
        out.push({
          exporterId: obs.exporter.id,
          inputIfIndex: obs.inputIfIndex,
          outputIfIndex: obs.outputIfIndex,
          srcIp: plan.srcIp,
          dstIp: plan.dstIp,
          protocol: plan.conv.protocol,
          srcPort: plan.srcPort,
          dstPort: plan.dstPort,
          samplingRate: n,
          sampleCount: samples,
          estimatedBytes: samples * plan.packetBytes * n,
          lastSampleAt: samples > 0 ? t : null,
        });
      }
    }
    this.secondCache.set(t, out);
    if (this.secondCache.size > 64) {
      const oldest = Math.min(...this.secondCache.keys());
      this.secondCache.delete(oldest);
    }
    return out;
  }

  /**
   * Window-bucketed observations for (end - WINDOW_SECONDS, end], one entry
   * per (exporter, unidirectional flow key). Keys with zero samples in the
   * whole window are omitted, as a real collector would never see them.
   */
  windowObservations(endTick: number): WindowFlowObservation[] {
    const end = this.effectiveTick(endTick);
    const merged = new Map<string, WindowFlowObservation>();
    for (let t = end - WINDOW_SECONDS + 1; t <= end; t++) {
      for (const o of this.sampleSecond(t)) {
        if (o.sampleCount === 0) continue;
        const key = `${o.exporterId}|${o.srcIp}|${o.dstIp}|${o.protocol}|${o.srcPort}|${o.dstPort}`;
        const prev = merged.get(key);
        if (!prev) {
          merged.set(key, { ...o });
        } else {
          prev.sampleCount += o.sampleCount;
          prev.estimatedBytes += o.estimatedBytes;
          prev.lastSampleAt = o.lastSampleAt ?? prev.lastSampleAt;
        }
      }
    }
    return [...merged.values()];
  }

  private cumulativeAt(t: number): { in: number; out: number } {
    // Counters start at a seeded offset so values are not trivially zero-based.
    if (this.cumulative.length === 0) {
      const offset = Math.floor(keyedUniform(this.seed, "wan-offset") * 1e12);
      this.cumulative.push({ in: offset, out: Math.floor(offset / 3) });
    }
    while (this.cumulative.length <= t) {
      const s = this.cumulative.length - 1;
      const prev = this.cumulative[s]!;
      let inBytes = 0;
      let outBytes = 0;
      for (const plan of this.plans) {
        if (plan.wan === 0) continue;
        const b = this.trueBytes(plan, s);
        if (plan.wan === 1) inBytes += b;
        else outBytes += b;
      }
      const bg = (BACKGROUND_BPS / 8) * (1 + 0.3 * smoothNoise(this.seed, "bg", s));
      this.cumulative.push({
        in: prev.in + Math.floor(inBytes + bg + 0.5),
        out: prev.out + Math.floor(outBytes + bg / 2 + 0.5),
      });
    }
    return this.cumulative[Math.max(0, t)]!;
  }

  /**
   * The two most recent counter polls of the boundary (WAN) interface at or
   * before `tick`. `previous` is null before the second poll ever happened.
   */
  wanCounterReadings(tick: number): {
    previous: InterfaceCounterReading | null;
    current: InterfaceCounterReading;
  } {
    const end = this.effectiveTick(tick);
    const at = Math.floor(end / COUNTER_INTERVAL_SECONDS) * COUNTER_INTERVAL_SECONDS;
    const reading = (s: number): InterfaceCounterReading => {
      const c = this.cumulativeAt(s);
      return {
        exporterId: this.router.id,
        ifIndex: this.router.boundaryIfIndex ?? ROUTER_WAN_IF,
        at: s,
        inOctets: c.in,
        outOctets: c.out,
      };
    };
    const prevAt = at - COUNTER_INTERVAL_SECONDS;
    return { previous: prevAt >= 0 ? reading(prevAt) : null, current: reading(at) };
  }

  get boundaryExporter(): Exporter {
    return this.router;
  }
}
