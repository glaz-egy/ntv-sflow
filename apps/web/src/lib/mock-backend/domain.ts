/**
 * Internal domain model of the mock backend. Mirrors docs/DATA_MODEL.md.
 * These types are NOT wire contracts — handlers map them to `@/contracts`.
 */
import type {
  DeviceAddress,
  DeviceType,
  Protocol,
  TopologyEvidence,
  TopologyLinkType,
} from "@/contracts";

export interface Device {
  /** Opaque, persistent identity (D-009). Never an IP. */
  id: string;
  displayName: string;
  type: DeviceType;
  online: boolean;
  addresses: DeviceAddress[];
  macs: string[];
  vlanId: number | null;
  ssid: string | null;
  vendor: string | null;
  model: string | null;
}

export interface Network {
  name: string;
  cidr: string;
  vlanId: number | null;
}

export type ExporterRole = "boundary" | "core" | "access";

export interface Exporter {
  id: string;
  name: string;
  agentAddress: string;
  role: ExporterRole;
  samplingRate: number;
  /** ifIndex of the configured WAN/boundary interface, if any. */
  boundaryIfIndex: number | null;
  ifSpeedBps: number;
}

export interface TopologyLinkDef {
  id: string;
  a: string;
  b: string;
  linkType: TopologyLinkType;
  evidence: TopologyEvidence;
  confidence: number;
  aInterface: string | null;
  bInterface: string | null;
}

export interface GeoRecord {
  countryCode: string | null;
  countryName: string | null;
  city: string | null;
  latitude: number | null;
  longitude: number | null;
  asn: number | null;
  organization: string | null;
}

/**
 * One window's worth of sampled observations for a unidirectional flow key at
 * one exporter. This is what the Go aggregator will produce from per-sample
 * FlowObservations (docs/DATA_MODEL.md §3) after window bucketing.
 */
export interface WindowFlowObservation {
  exporterId: string;
  inputIfIndex: number | null;
  outputIfIndex: number | null;
  srcIp: string;
  dstIp: string;
  protocol: Protocol;
  srcPort: number | null;
  dstPort: number | null;
  samplingRate: number;
  sampleCount: number;
  /** Σ sampled_packet_length × sampling_rate. An estimate, not exact bytes. */
  estimatedBytes: number;
  /** Sim second of the last sample in the window (null if no samples). */
  lastSampleAt: number | null;
}

export interface InterfaceCounterReading {
  exporterId: string;
  ifIndex: number;
  at: number;
  inOctets: number;
  outOctets: number;
}

export interface Origin {
  label: string;
  latitude: number;
  longitude: number;
  precision: "country" | "region" | "city" | "custom";
}

export interface ObservationPolicy {
  /** Exporter roles in preference order for flows crossing the boundary. */
  external: ExporterRole[];
  /** Exporter roles in preference order for internal-only flows. */
  internal: ExporterRole[];
}

export interface Inventory {
  devices: Device[];
  networks: Network[];
  exporters: Exporter[];
  topology: TopologyLinkDef[];
  internalCidrs: string[];
  origin: Origin;
  observationPolicy: ObservationPolicy;
}
