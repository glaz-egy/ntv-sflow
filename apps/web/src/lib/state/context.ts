/**
 * Investigation context shared by Globe and Home (D-002).
 *
 * The URL query string is the single source of truth so that context is
 * shareable/bookmarkable and survives view switches. Parsing never throws:
 * invalid values fall back to defaults.
 *
 *   src   source node (Globe: source-device filter, Home: focused device)
 *   dst   destination key (Globe: selected destination, Home: destination filter)
 *   proto protocol filter            min  minimum bps         top  top N
 *   g     Globe grouping             dir  Globe direction
 *   mode  Home view mode             scope Home internal/external scope
 *   sel   Home inspector selection   vlan Home VLAN filter    types Home device types
 *   inactive  Home: also show devices without traffic in the window
 */
import type {
  DestinationKey,
  DeviceType,
  DirectionFilter,
  Grouping,
  Protocol,
  ScopeFilter,
} from "@/contracts";

export type ViewName = "globe" | "home";
export type HomeMode = "traffic" | "topology" | "hybrid";

export interface InvestigationContext {
  src: string | null;
  dst: DestinationKey | null;
  proto: Protocol | null;
  min: number;
  top: number | null;
  grouping: Grouping;
  dir: DirectionFilter;
  mode: HomeMode;
  scope: ScopeFilter;
  sel: string | null;
  vlan: number | null;
  types: DeviceType[];
  inactive: boolean;
}

export const DEFAULT_CONTEXT: InvestigationContext = {
  src: null,
  dst: null,
  proto: null,
  min: 0,
  top: null,
  grouping: "asn",
  dir: "both",
  mode: "traffic",
  scope: "both",
  sel: null,
  vlan: null,
  types: [],
  inactive: false,
};

const GROUPINGS: Grouping[] = ["country", "city", "asn", "ip"];
const DIRECTIONS: DirectionFilter[] = ["inbound", "outbound", "both"];
const PROTOCOLS: Protocol[] = ["tcp", "udp", "icmp", "other"];
const MODES: HomeMode[] = ["traffic", "topology", "hybrid"];
const SCOPES: ScopeFilter[] = ["internal", "external", "both"];
const DEVICE_TYPES: DeviceType[] = [
  "internet",
  "router",
  "firewall",
  "switch",
  "wireless_ap",
  "server",
  "pc",
  "smartphone",
  "iot",
  "vm",
  "kubernetes_node",
  "unknown",
];

function oneOf<T extends string>(value: string | null, allowed: readonly T[], fallback: T): T {
  return value !== null && (allowed as readonly string[]).includes(value) ? (value as T) : fallback;
}

function nonNegativeInt(value: string | null): number | null {
  if (value === null || !/^\d+$/.test(value)) return null;
  const n = Number(value);
  return Number.isSafeInteger(n) ? n : null;
}

function nonEmpty(value: string | null): string | null {
  return value && value.trim().length > 0 ? value : null;
}

export function destinationGrouping(key: string): Grouping | null {
  const g = key.slice(0, key.indexOf(":"));
  return (GROUPINGS as string[]).includes(g) ? (g as Grouping) : null;
}

type ParamSource = { get(name: string): string | null };

export function parseContext(params: ParamSource): InvestigationContext {
  let dst = nonEmpty(params.get("dst"));
  const dstGrouping = dst ? destinationGrouping(dst) : null;
  if (dst && !dstGrouping) dst = null;
  const top = nonNegativeInt(params.get("top"));
  const types = (params.get("types") ?? "")
    .split(",")
    .filter((t): t is DeviceType => (DEVICE_TYPES as string[]).includes(t));

  return {
    src: nonEmpty(params.get("src")),
    dst,
    proto: PROTOCOLS.includes(params.get("proto") as Protocol) ? (params.get("proto") as Protocol) : null,
    min: nonNegativeInt(params.get("min")) ?? DEFAULT_CONTEXT.min,
    top: top && top > 0 ? top : null,
    // A selected destination implies its grouping, so the selection stays visible.
    grouping: dstGrouping ?? oneOf(params.get("g"), GROUPINGS, DEFAULT_CONTEXT.grouping),
    dir: oneOf(params.get("dir"), DIRECTIONS, DEFAULT_CONTEXT.dir),
    mode: oneOf(params.get("mode"), MODES, DEFAULT_CONTEXT.mode),
    scope: oneOf(params.get("scope"), SCOPES, DEFAULT_CONTEXT.scope),
    sel: nonEmpty(params.get("sel")),
    vlan: nonNegativeInt(params.get("vlan")),
    types: [...new Set(types)],
    inactive: params.get("inactive") === "1",
  };
}

export function serializeContext(ctx: InvestigationContext): URLSearchParams {
  const p = new URLSearchParams();
  if (ctx.src) p.set("src", ctx.src);
  if (ctx.dst) p.set("dst", ctx.dst);
  if (ctx.proto) p.set("proto", ctx.proto);
  if (ctx.min !== DEFAULT_CONTEXT.min) p.set("min", String(ctx.min));
  if (ctx.top !== null) p.set("top", String(ctx.top));
  if (ctx.grouping !== DEFAULT_CONTEXT.grouping) p.set("g", ctx.grouping);
  if (ctx.dir !== DEFAULT_CONTEXT.dir) p.set("dir", ctx.dir);
  if (ctx.mode !== DEFAULT_CONTEXT.mode) p.set("mode", ctx.mode);
  if (ctx.scope !== DEFAULT_CONTEXT.scope) p.set("scope", ctx.scope);
  if (ctx.sel) p.set("sel", ctx.sel);
  if (ctx.vlan !== null) p.set("vlan", String(ctx.vlan));
  if (ctx.types.length > 0) p.set("types", ctx.types.join(","));
  if (ctx.inactive) p.set("inactive", "1");
  return p;
}

export function hrefFor(view: ViewName, ctx: InvestigationContext): string {
  const qs = serializeContext(ctx).toString();
  return `/${view}${qs ? `?${qs}` : ""}`;
}

/** Changing Globe grouping invalidates a destination key of another grouping. */
export function withGrouping(ctx: InvestigationContext, grouping: Grouping): InvestigationContext {
  const keep = ctx.dst !== null && destinationGrouping(ctx.dst) === grouping;
  return { ...ctx, grouping, dst: keep ? ctx.dst : null };
}

/**
 * Globe → Home ("Open in Home Network", or the Home tab).
 * The selected destination becomes Home's destination filter; source,
 * protocol and thresholds carry over unchanged.
 */
export function globeToHome(ctx: InvestigationContext, destination?: DestinationKey): InvestigationContext {
  return { ...ctx, dst: destination ?? ctx.dst };
}

/**
 * Home → Globe ("Show external destinations", or the Globe tab).
 * An explicitly chosen device becomes the Globe source filter. If that
 * changes the source, a previously selected destination is dropped because
 * it may not be attributable to the new source.
 */
export function homeToGlobe(ctx: InvestigationContext, deviceId?: string): InvestigationContext {
  if (deviceId === undefined || deviceId === ctx.src) return { ...ctx };
  return { ...ctx, src: deviceId, dst: null };
}
