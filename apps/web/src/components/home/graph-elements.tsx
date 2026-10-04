"use client";

import {
  BaseEdge,
  EdgeLabelRenderer,
  Handle,
  Position,
  useInternalNode,
  type Edge,
  type EdgeProps,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import {
  Boxes,
  Box,
  Cpu,
  Globe2,
  HelpCircle,
  Monitor,
  Network,
  Router,
  Server,
  ShieldHalf,
  Smartphone,
  Wifi,
  type LucideIcon,
} from "lucide-react";
import { memo } from "react";
import type { DeviceType, HomeNode, TopologyLink, TrafficEdge } from "@/contracts";
import { formatMeasurement } from "@/lib/format/measurement";
import { cn } from "@/lib/utils";
import { COLORS, withAlpha } from "@/lib/viz/colors";
import { edgeWidth } from "@/lib/viz/scales";

export const DEVICE_ICONS: Record<DeviceType, LucideIcon> = {
  internet: Globe2,
  router: Router,
  firewall: ShieldHalf,
  switch: Network,
  wireless_ap: Wifi,
  server: Server,
  pc: Monitor,
  smartphone: Smartphone,
  iot: Cpu,
  vm: Box,
  kubernetes_node: Boxes,
  unknown: HelpCircle,
};

export type Emphasis = "normal" | "highlight" | "dimmed";

export type DeviceNodeData = {
  node: HomeNode;
  emphasis: Emphasis;
  focused: boolean;
};
export type DeviceFlowNode = Node<DeviceNodeData, "device">;

const hiddenHandle = "!size-1 !min-h-0 !min-w-0 !border-0 !bg-transparent";

export const DeviceNode = memo(function DeviceNode({ data, selected }: NodeProps<DeviceFlowNode>) {
  const { node, emphasis, focused } = data;
  const Icon = DEVICE_ICONS[node.device_type];
  const isInternet = node.kind === "internet" || node.kind === "external_destination";
  const unresolved = node.kind === "unresolved_endpoint";
  const rx = node.rx_bps?.value ?? 0;
  const tx = node.tx_bps?.value ?? 0;

  return (
    <div
      className={cn(
        "w-[164px] rounded-lg border bg-card/95 px-2.5 py-1.5 text-left shadow-sm transition-opacity",
        isInternet && "rounded-2xl border-sky-400/40 bg-sky-950/60",
        node.kind === "external_destination" && "border-amber-300/60",
        unresolved && "border-dashed",
        emphasis === "dimmed" && "opacity-30",
        emphasis === "highlight" && "ring-2 ring-amber-300/80",
        focused && "ring-2 ring-sky-300",
        selected && "ring-2 ring-white",
      )}
    >
      <Handle type="target" position={Position.Top} className={hiddenHandle} isConnectable={false} />
      <div className="flex items-center gap-1.5">
        <Icon className={cn("size-4 shrink-0", isInternet ? "text-sky-300" : "text-slate-300")} aria-hidden />
        <span className="min-w-0 flex-1 truncate text-xs font-medium">{node.label}</span>
        {node.kind === "device" && (
          <span
            title={node.status}
            className={cn(
              "size-1.5 shrink-0 rounded-full",
              node.status === "online" ? "bg-emerald-400" : node.status === "stale" ? "bg-amber-400" : "bg-slate-500",
            )}
          />
        )}
      </div>
      <div className="mt-0.5 flex justify-between gap-1 text-[10px] text-muted-foreground">
        <span className="truncate font-mono">
          {unresolved ? "Unresolved device" : (node.addresses[0] ?? (isInternet ? "external" : ""))}
        </span>
        {node.vlan_id !== null && <span>VLAN {node.vlan_id}</span>}
      </div>
      {(rx > 0 || tx > 0) && (
        <div className="tabular mt-0.5 flex gap-2 text-[10px]">
          <span className="text-cyan-300">↓{formatMeasurement(node.rx_bps).replace("≈", "")}</span>
          <span className="text-amber-300">↑{formatMeasurement(node.tx_bps).replace("≈", "")}</span>
        </div>
      )}
      <Handle type="source" position={Position.Bottom} className={hiddenHandle} isConnectable={false} />
    </div>
  );
});

function center(node: ReturnType<typeof useInternalNode>) {
  if (!node) return null;
  const { x, y } = node.internals.positionAbsolute;
  return { x: x + (node.measured.width ?? 0) / 2, y: y + (node.measured.height ?? 0) / 2 };
}

export type TrafficEdgeData = {
  edge: TrafficEdge;
  emphasis: Emphasis;
  animate: boolean;
  showLabel: boolean;
  /** Extra curvature so overlays don't sit on top of topology lines. */
  curvature: number;
};
export type TrafficFlowEdge = Edge<TrafficEdgeData, "traffic">;

/**
 * Logical communication edge between endpoints. Drawn curved and never along
 * topology links: it does not claim a physical path.
 */
export const TrafficEdgeView = memo(function TrafficEdgeView({
  id,
  source,
  target,
  data,
  selected,
}: EdgeProps<TrafficFlowEdge>) {
  const s = center(useInternalNode(source));
  const t = center(useInternalNode(target));
  if (!s || !t || !data) return null;
  const { edge, emphasis, animate, showLabel, curvature } = data;
  const fwd = edge.forward_bps.value;
  const rev = edge.reverse_bps.value;
  // Draw from the dominant sender so dash animation and arrow point the right way.
  const [a, b] = fwd >= rev ? [s, t] : [t, s];
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  const len = Math.hypot(dx, dy) || 1;
  const off = len * curvature;
  const cx = (a.x + b.x) / 2 - (dy / len) * off;
  const cy = (a.y + b.y) / 2 + (dx / len) * off;
  const path = `M ${a.x},${a.y} Q ${cx},${cy} ${b.x},${b.y}`;
  const mx = 0.25 * a.x + 0.5 * cx + 0.25 * b.x;
  const my = 0.25 * a.y + 0.5 * cy + 0.25 * b.y;
  const angle = (Math.atan2(dy, dx) * 180) / Math.PI;

  const color =
    edge.scope === "internal" ? COLORS.internal : fwd >= rev ? COLORS.outbound : COLORS.inbound;
  const width = edgeWidth(fwd + rev) * (selected ? 1.3 : 1);
  const alpha = emphasis === "dimmed" ? 0.12 : selected || emphasis === "highlight" ? 1 : 0.75;

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        interactionWidth={Math.max(12, width + 8)}
        style={{ stroke: withAlpha(color, alpha * 0.45), strokeWidth: width }}
      />
      <path
        d={path}
        fill="none"
        className={animate ? "flow-dash" : undefined}
        style={{
          stroke: withAlpha(color, alpha),
          strokeWidth: Math.max(1, width * 0.55),
          strokeDasharray: animate ? "6 10" : undefined,
          pointerEvents: "none",
        }}
      />
      {/* Static direction cue (survives reduced motion). */}
      <g transform={`translate(${mx},${my}) rotate(${angle})`} style={{ pointerEvents: "none" }}>
        <path d="M -5,-4.5 L 5,0 L -5,4.5 Z" fill={withAlpha(color, Math.min(1, alpha + 0.2))} />
      </g>
      {showLabel && (
        <EdgeLabelRenderer>
          <div
            className="nodrag nopan pointer-events-none absolute rounded bg-background/85 px-1 text-[10px] tabular shadow"
            style={{ transform: `translate(-50%, -140%) translate(${mx}px, ${my}px)`, opacity: emphasis === "dimmed" ? 0.3 : 1 }}
          >
            {formatMeasurement({ ...edge.forward_bps, value: fwd + rev })}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
});

export type TopologyEdgeData = { link: TopologyLink; subdued: boolean };
export type TopologyFlowEdge = Edge<TopologyEdgeData, "topology">;

const EVIDENCE_STYLE: Record<TopologyLink["evidence"], { dash?: string; color: string }> = {
  manual: { color: "#cbd5e1" },
  lldp: { color: "#93c5fd" },
  cdp: { color: "#93c5fd" },
  wlc: { color: "#5eead4", dash: "5 4" },
  inferred: { color: "#94a3b8", dash: "1 5" },
};

/** Known physical/logical structure. Style encodes evidence, not traffic. */
export const TopologyEdgeView = memo(function TopologyEdgeView({ id, source, target, data }: EdgeProps<TopologyFlowEdge>) {
  const s = center(useInternalNode(source));
  const t = center(useInternalNode(target));
  if (!s || !t || !data) return null;
  const style = EVIDENCE_STYLE[data.link.evidence];
  const logical = data.link.link_type === "logical";
  return (
    <BaseEdge
      id={id}
      path={`M ${s.x},${s.y} L ${t.x},${t.y}`}
      style={{
        stroke: style.color,
        strokeOpacity: (data.subdued ? 0.25 : 0.7) * (data.link.evidence === "inferred" ? 0.8 : 1),
        strokeWidth: data.link.evidence === "inferred" ? 1.5 : 2,
        strokeDasharray: logical ? "10 6" : style.dash,
        strokeLinecap: "round",
      }}
    />
  );
});

export const NODE_TYPES = { device: DeviceNode };
export const EDGE_TYPES = { traffic: TrafficEdgeView, topology: TopologyEdgeView };
export { EVIDENCE_STYLE };
