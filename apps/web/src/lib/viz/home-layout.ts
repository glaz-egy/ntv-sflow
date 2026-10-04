/**
 * Deterministic tiered layout for Home View (HOME_NETWORK_VIEW.md §15).
 *
 *   tier 0  Internet / external destination nodes
 *   tier 1  routers / firewalls
 *   tier 2  switches / access points
 *   tier 3+ endpoints, one column block per VLAN (unknown VLAN last)
 *
 * Positions depend only on node identity and metadata, never on traffic, so
 * live updates do not move nodes. Callers also cache positions per id.
 */
import type { DeviceType, HomeNode } from "@/contracts";

export interface Point {
  x: number;
  y: number;
}

const COL = 200;
const ROW = 96;
const TIER_Y = [0, 150, 300, 470];
const ENDPOINT_ROWS = 5;

const INFRA_TIER: Partial<Record<DeviceType, number>> = {
  router: 1,
  firewall: 1,
  switch: 2,
  wireless_ap: 2,
};

function tierOf(n: HomeNode): number {
  if (n.kind === "internet" || n.kind === "external_destination") return 0;
  return INFRA_TIER[n.device_type] ?? 3;
}

function byLabel(a: HomeNode, b: HomeNode): number {
  return a.label.localeCompare(b.label) || a.id.localeCompare(b.id);
}

function row(nodes: HomeNode[], x0: number, y: number, out: Map<string, Point>) {
  nodes.forEach((n, i) => out.set(n.id, { x: x0 + i * COL, y }));
}

/**
 * Layout rules chosen so that adding transient nodes never moves existing ones:
 * - resolved endpoints fill VLAN column blocks from x = 0 rightwards;
 * - unresolved endpoints are appended in columns to the right of them;
 * - infrastructure is centred over the resolved endpoints only;
 * - the Internet sits above infrastructure, destination nodes to its right.
 */
export function computeHomeLayout(nodes: HomeNode[]): Map<string, Point> {
  const out = new Map<string, Point>();
  const unique = [...new Map(nodes.map((n) => [n.id, n])).values()];
  const tiers: HomeNode[][] = [[], [], [], []];
  for (const n of unique) tiers[tierOf(n)]!.push(n);

  const resolved = tiers[3]!.filter((n) => n.kind !== "unresolved_endpoint");
  const unresolved = tiers[3]!.filter((n) => n.kind === "unresolved_endpoint").sort(byLabel);

  const blocks = new Map<string, HomeNode[]>();
  for (const n of resolved) {
    const key = n.vlan_id === null ? "~none" : String(n.vlan_id).padStart(5, "0");
    blocks.set(key, [...(blocks.get(key) ?? []), n]);
  }
  const columns: HomeNode[][] = [];
  for (const [, block] of [...blocks.entries()].sort(([a], [b]) => a.localeCompare(b))) {
    block.sort(byLabel);
    for (let i = 0; i < block.length; i += ENDPOINT_ROWS) columns.push(block.slice(i, i + ENDPOINT_ROWS));
  }
  columns.forEach((c, ci) => c.forEach((n, ri) => out.set(n.id, { x: ci * COL, y: TIER_Y[3]! + ri * ROW })));
  const firstUnresolvedCol = columns.length + (columns.length > 0 ? 0.5 : 0);
  unresolved.forEach((n, i) =>
    out.set(n.id, {
      x: (firstUnresolvedCol + Math.floor(i / ENDPOINT_ROWS)) * COL,
      y: TIER_Y[3]! + (i % ENDPOINT_ROWS) * ROW,
    }),
  );

  const span = Math.max(0, columns.length - 1) * COL;
  const centre = (count: number) => span / 2 - ((count - 1) * COL) / 2;
  const infra1 = tiers[1]!.sort(byLabel);
  // Switches before APs for a stable left-to-right order.
  const infra2 = tiers[2]!.sort((a, b) => b.device_type.localeCompare(a.device_type) || byLabel(a, b));
  row(infra1, centre(infra1.length), TIER_Y[1]!, out);
  row(infra2, centre(infra2.length), TIER_Y[2]!, out);

  const internet = tiers[0]!.filter((n) => n.kind === "internet");
  const external = tiers[0]!.filter((n) => n.kind !== "internet").sort(byLabel);
  row(internet, centre(1), TIER_Y[0]!, out);
  row(external, centre(1) + COL * 1.5, TIER_Y[0]!, out);
  return out;
}
