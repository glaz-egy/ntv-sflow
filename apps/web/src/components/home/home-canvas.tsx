"use client";

import {
  Background,
  BackgroundVariant,
  Controls,
  ReactFlow,
  type Edge,
  type Node,
  type NodeChange,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useCallback, useMemo, useState } from "react";
import type { HomeNode, HomeTrafficResponse, TopologyResponse } from "@/contracts";
import type { HomeMode } from "@/lib/state/context";
import { computeHomeLayout } from "@/lib/viz/home-layout";
import {
  EDGE_TYPES,
  NODE_TYPES,
  type DeviceFlowNode,
  type Emphasis,
  type TopologyFlowEdge,
  type TrafficFlowEdge,
} from "./graph-elements";

const LABEL_MIN_BPS = 20_000_000;

export interface HomeCanvasProps {
  mode: HomeMode;
  traffic: HomeTrafficResponse | undefined;
  topology: TopologyResponse | undefined;
  selectedId: string | null;
  focusId: string | null;
  animate: boolean;
  onSelect: (id: string | null) => void;
}

export function HomeCanvas({ mode, traffic, topology, selectedId, focusId, animate, onSelect }: HomeCanvasProps) {
  // Nodes are rebuilt from data every window; React Flow's measured sizes must
  // be fed back in or nodes count as uninitialised and edges are not drawn.
  const [measured, setMeasured] = useState<Record<string, { width: number; height: number }>>({});
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    const dims = changes.flatMap((c) => (c.type === "dimensions" && c.dimensions ? [{ id: c.id, ...c.dimensions }] : []));
    if (dims.length === 0) return;
    setMeasured((m) => {
      const next = { ...m };
      for (const d of dims) next[d.id] = { width: d.width, height: d.height };
      return next;
    });
  }, []);


  const visibleNodes: HomeNode[] = useMemo(() => {
    const t = traffic?.nodes ?? [];
    if (mode === "traffic") return t;
    const byId = new Map((topology?.nodes ?? []).map((n) => [n.id, n]));
    if (mode === "hybrid") for (const n of t) byId.set(n.id, n);
    return [...byId.values()];
  }, [mode, traffic, topology]);

  // Layout depends only on node identity/metadata (see computeHomeLayout), so
  // nodes keep their positions while traffic values change.
  const layoutKey = [...(topology?.nodes ?? []), ...(traffic?.nodes ?? [])]
    .map((n) => `${n.id}|${n.kind}|${n.device_type}|${n.vlan_id}|${n.label}`)
    .sort()
    .join(";");
  const positions = useMemo(
    () => computeHomeLayout([...(topology?.nodes ?? []), ...(traffic?.nodes ?? [])]),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [layoutKey],
  );

  const dest = traffic?.destination_context ?? null;
  const highlighted = useMemo(() => new Set(dest?.source_node_ids ?? []), [dest]);
  const destNodeId = dest ? `dst:${dest.key}` : null;

  const nodes: DeviceFlowNode[] = visibleNodes.map((n) => {
    let emphasis: Emphasis = "normal";
    if (dest) {
      emphasis = highlighted.has(n.id) || n.id === destNodeId ? "highlight" : n.kind === "internet" ? "normal" : "dimmed";
    }
    return {
      id: n.id,
      type: "device",
      position: positions.get(n.id) ?? { x: 0, y: 0 },
      measured: measured[n.id],
      data: { node: n, emphasis, focused: n.id === focusId },
      selected: n.id === selectedId,
      draggable: false,
    };
  });
  const nodeIds = new Set(nodes.map((n) => n.id));

  const edges: Edge[] = [];
  if (mode !== "traffic") {
    for (const link of topology?.links ?? []) {
      if (!nodeIds.has(link.source_node_id) || !nodeIds.has(link.target_node_id)) continue;
      edges.push({
        id: link.id,
        type: "topology",
        source: link.source_node_id,
        target: link.target_node_id,
        data: { link, subdued: mode === "hybrid" },
        selectable: false,
        zIndex: 0,
      } satisfies TopologyFlowEdge);
    }
  }
  if (mode !== "topology") {
    for (const e of traffic?.edges ?? []) {
      if (!nodeIds.has(e.source) || !nodeIds.has(e.target)) continue;
      const related = dest ? e.target === destNodeId : true;
      const total = e.forward_bps.value + e.reverse_bps.value;
      edges.push({
        id: e.id,
        type: "traffic",
        source: e.source,
        target: e.target,
        selected: e.id === selectedId,
        zIndex: 1,
        data: {
          edge: e,
          emphasis: dest ? (related ? "highlight" : "dimmed") : "normal",
          animate,
          showLabel: e.id === selectedId || (total >= LABEL_MIN_BPS && (!dest || related)),
          curvature: mode === "hybrid" ? 0.22 : 0.12,
        },
      } satisfies TrafficFlowEdge);
    }
  }

  return (
    <ReactFlow
      key={mode}
      nodes={nodes as Node[]}
      edges={edges}
      onNodesChange={onNodesChange}
      nodeTypes={NODE_TYPES}
      edgeTypes={EDGE_TYPES}
      colorMode="dark"
      fitView
      fitViewOptions={{ padding: 0.15 }}
      minZoom={0.2}
      maxZoom={2.5}
      nodesDraggable={false}
      nodesConnectable={false}
      elementsSelectable
      onNodeClick={(_, n) => onSelect(n.id)}
      onEdgeClick={(_, e) => e.type === "traffic" && onSelect(e.id)}
      onPaneClick={() => onSelect(null)}
      style={{ background: "transparent" }}
    >
      <Background variant={BackgroundVariant.Dots} gap={24} size={1} color="rgba(148,163,184,0.15)" />
      <Controls showInteractive={false} position="bottom-right" />
    </ReactFlow>
  );
}
