/**
 * Turns window observations into attributed flows:
 *   1. de-duplicate observation points (D-007, D-024)
 *   2. classify internal/external from configured CIDRs (D-008)
 *   3. resolve internal endpoints to device identities (D-009)
 *   4. enrich external endpoints with GeoIP/ASN (unknown stays unknown)
 *
 * Reference for Go `internal/aggregation` + `internal/enrichment`.
 */
import { compareStrings } from "../strings";
import type { Direction, Protocol } from "@/contracts";
import type {
  Device,
  Exporter,
  GeoRecord,
  ObservationPolicy,
  WindowFlowObservation,
} from "../domain";
import { UNKNOWN_GEO } from "../geodb";
import { CidrClassifier, parseIp } from "../net/ip";

export type FlowScope = "internal" | "external" | "transit";

export interface AttributedFlow {
  key: string;
  srcIp: string;
  dstIp: string;
  protocol: Protocol;
  srcPort: number | null;
  dstPort: number | null;
  /** The well-known side of the conversation, where determinable. */
  servicePort: number | null;
  scope: FlowScope;
  /** For external flows; internal flows have direction null. */
  direction: Direction | null;
  srcNodeId: string | null;
  dstNodeId: string | null;
  /** Internal endpoint of an external flow. */
  internalNodeId: string | null;
  /** External endpoint of an external flow. */
  externalIp: string | null;
  geo: GeoRecord;
  estimatedBytes: number;
  sampleCount: number;
  /** Observation used for the values above. */
  used: WindowFlowObservation;
  /** Every exporter that observed the flow (values NOT summed). */
  observations: WindowFlowObservation[];
  lastSeenAt: number | null;
}

export function unresolvedNodeId(ip: string): string {
  return `ep:${ip}`;
}

/** IP → device registry. Addresses compared in canonical form. */
export class DeviceRegistry {
  private readonly byIp = new Map<string, Device>();
  readonly devices: Device[];

  constructor(devices: Device[]) {
    this.devices = devices;
    for (const d of devices) {
      for (const a of d.addresses) {
        const key = canonical(a.address);
        if (key) this.byIp.set(key, d);
      }
    }
  }

  lookup(ip: string): Device | null {
    const key = canonical(ip);
    return key ? (this.byIp.get(key) ?? null) : null;
  }

  get(id: string): Device | null {
    return this.devices.find((d) => d.id === id) ?? null;
  }

  /** Device id, or a temporary endpoint id for unresolved addresses. */
  nodeIdFor(ip: string): string {
    return this.lookup(ip)?.id ?? unresolvedNodeId(ip);
  }
}

function canonical(ip: string): string | null {
  const p = parseIp(ip);
  return p ? `${p.family}:${p.value.toString(16)}` : null;
}

function servicePort(src: number | null, dst: number | null): number | null {
  if (src === null || dst === null) return src ?? dst;
  // Lower port is assumed to be the service side (simple heuristic).
  return Math.min(src, dst);
}

/**
 * Chooses which exporter's observation counts for a flow. Preference by role
 * per scope; ties broken by exporter id. Never "pick the largest" — that
 * biases estimates upward.
 */
export function chooseObservation(
  candidates: WindowFlowObservation[],
  exporters: Map<string, Exporter>,
  roles: ObservationPolicy["external"],
): WindowFlowObservation {
  const rank = (o: WindowFlowObservation) => {
    const role = exporters.get(o.exporterId)?.role;
    const i = role ? roles.indexOf(role) : -1;
    return i < 0 ? roles.length : i;
  };
  return [...candidates].sort(
    (a, b) => rank(a) - rank(b) || compareStrings(a.exporterId, b.exporterId),
  )[0]!;
}

export interface AttributeContext {
  classifier: CidrClassifier;
  registry: DeviceRegistry;
  geo: Map<string, GeoRecord>;
  exporters: Exporter[];
  policy: ObservationPolicy;
}

export function attributeFlows(
  observations: WindowFlowObservation[],
  ctx: AttributeContext,
): AttributedFlow[] {
  const exporters = new Map(ctx.exporters.map((e) => [e.id, e]));
  const flowKey = (o: WindowFlowObservation) =>
    `${o.srcIp}|${o.dstIp}|${o.protocol}|${o.srcPort ?? ""}|${o.dstPort ?? ""}`;
  // Canonical order: results must not depend on arrival order (D-052).
  const sorted = [...observations].sort(
    (a, b) => compareStrings(flowKey(a), flowKey(b)) || compareStrings(a.exporterId, b.exporterId),
  );
  const groups = new Map<string, WindowFlowObservation[]>();
  for (const o of sorted) {
    const key = flowKey(o);
    const list = groups.get(key);
    if (list) list.push(o);
    else groups.set(key, [o]);
  }

  const out: AttributedFlow[] = [];
  for (const [key, obs] of groups) {
    const sample = obs[0]!;
    const srcInternal = ctx.classifier.isInternal(sample.srcIp);
    const dstInternal = ctx.classifier.isInternal(sample.dstIp);
    if (srcInternal === null || dstInternal === null) continue; // unparseable

    let scope: FlowScope;
    let direction: Direction | null = null;
    if (srcInternal && dstInternal) scope = "internal";
    else if (!srcInternal && !dstInternal) scope = "transit";
    else {
      scope = "external";
      direction = srcInternal ? "outbound" : "inbound";
    }

    const used = chooseObservation(
      obs,
      exporters,
      scope === "internal" ? ctx.policy.internal : ctx.policy.external,
    );
    const srcNodeId = srcInternal ? ctx.registry.nodeIdFor(sample.srcIp) : null;
    const dstNodeId = dstInternal ? ctx.registry.nodeIdFor(sample.dstIp) : null;
    const externalIp = scope === "external" ? (srcInternal ? sample.dstIp : sample.srcIp) : null;

    out.push({
      key,
      srcIp: sample.srcIp,
      dstIp: sample.dstIp,
      protocol: sample.protocol,
      srcPort: sample.srcPort,
      dstPort: sample.dstPort,
      servicePort: servicePort(sample.srcPort, sample.dstPort),
      scope,
      direction,
      srcNodeId,
      dstNodeId,
      internalNodeId: scope === "external" ? (srcNodeId ?? dstNodeId) : null,
      externalIp,
      geo: externalIp ? (ctx.geo.get(externalIp) ?? UNKNOWN_GEO) : UNKNOWN_GEO,
      estimatedBytes: used.estimatedBytes,
      sampleCount: used.sampleCount,
      used,
      observations: obs,
      lastSeenAt: obs.reduce<number | null>(
        (m, o) => (o.lastSampleAt !== null && (m === null || o.lastSampleAt > m) ? o.lastSampleAt : m),
        null,
      ),
    });
  }
  return out;
}
