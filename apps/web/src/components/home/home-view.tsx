"use client";

import Link from "next/link";
import { X } from "lucide-react";
import type { DeviceType, HomeTrafficResponse, Protocol, ScopeFilter } from "@/contracts";
import { MIN_BPS_OPTIONS, OptionGroup, PROTOCOL_OPTIONS, SelectField } from "@/components/common/fields";
import { Rate } from "@/components/common/rate";
import { FilterSection, SummaryItem, Workspace } from "@/components/shell/workspace";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useLiveQuery } from "@/lib/data/data-context";
import { hrefFor, type HomeMode } from "@/lib/state/context";
import { useHistoryWindow } from "@/lib/state/use-history-window";
import { useInvestigation, useReducedMotionPreference } from "@/lib/state/use-investigation";
import { useUiStore } from "@/lib/state/ui-store";
import { COLORS } from "@/lib/viz/colors";
import { EVIDENCE_STYLE } from "./graph-elements";
import { HomeCanvas } from "./home-canvas";
import { HomeInspector } from "./home-inspector";

const TYPE_OPTIONS: Array<{ value: DeviceType; label: string }> = [
  { value: "server", label: "Server" },
  { value: "pc", label: "PC" },
  { value: "smartphone", label: "Smartphone" },
  { value: "iot", label: "IoT" },
  { value: "kubernetes_node", label: "Kubernetes node" },
  { value: "vm", label: "VM" },
  { value: "unknown", label: "Unknown" },
];

const TOP_OPTIONS = ["25", "50", "100", "250"].map((v) => ({ value: v, label: `Top ${v} edges` }));

export function HomeView() {
  const [ctx, update] = useInvestigation();
  const particles = useUiStore((s) => s.particles);
  const motion = useUiStore((s) => s.motion);
  const reduced = useReducedMotionPreference(motion);
  const { range } = useHistoryWindow();

  const query = {
    start: range?.start,
    end: range?.end,
    focus_node_id: ctx.src,
    destination_key: ctx.dst,
    vlan_id: ctx.vlan,
    device_types: ctx.types,
    protocol: ctx.proto,
    min_bps: ctx.min,
    limit: ctx.top ?? 250,
    include_inactive: ctx.inactive,
    scope: ctx.scope,
  };
  const { data: traffic } = useLiveQuery((p) => p.getHomeTraffic(query), [JSON.stringify(query)], { live: !range });
  const { data: topology } = useLiveQuery((p) => p.getTopology(range ?? undefined), [range?.start, range?.end], {
    live: !range,
  });

  const vlans = [...new Set((topology?.nodes ?? []).map((n) => n.vlan_id).filter((v): v is number => v !== null))].sort(
    (a, b) => a - b,
  );
  const labelOf = (id: string | null) =>
    id ? ([...(traffic?.nodes ?? []), ...(topology?.nodes ?? [])].find((n) => n.id === id)?.label ?? id) : null;

  const summary = (
    <div className="flex items-center divide-x divide-border/60">
      <SummaryItem label="WAN total">
        <Rate m={traffic?.summary.wan_total} />
      </SummaryItem>
      <SummaryItem label="Internal" accent={COLORS.internal}>
        <Rate m={traffic?.summary.internal_estimated} />
      </SummaryItem>
      <SummaryItem label="Devices">
        <span className="tabular">{traffic?.summary.device_count ?? "—"}</span>
      </SummaryItem>
      <SummaryItem label="Online">
        <span className="tabular">{traffic?.summary.online_count ?? "—"}</span>
      </SummaryItem>
      {traffic && traffic.truncated_count > 0 && (
        <span className="px-3 text-xs text-muted-foreground">+{traffic.truncated_count} edges hidden by top-N</span>
      )}
    </div>
  );

  const filters = (
    <>
      <FilterSection title="View">
        <OptionGroup<HomeMode>
          ariaLabel="Home view mode"
          value={ctx.mode}
          onChange={(mode) => update({ mode })}
          options={[
            { value: "traffic", label: "Traffic", title: "Who talks to whom (logical)" },
            { value: "topology", label: "Topology", title: "Known physical/logical links" },
            { value: "hybrid", label: "Hybrid", title: "Topology with traffic overlay" },
          ]}
        />
      </FilterSection>
      <FilterSection title="Scope">
        <OptionGroup<ScopeFilter>
          ariaLabel="Traffic scope"
          value={ctx.scope}
          onChange={(scope) => update({ scope })}
          options={[
            { value: "both", label: "Both" },
            { value: "internal", label: "Internal" },
            { value: "external", label: "External" },
          ]}
        />
      </FilterSection>
      <FilterSection title="VLAN">
        <SelectField<string>
          ariaLabel="VLAN"
          value={ctx.vlan === null ? null : String(ctx.vlan)}
          onChange={(v) => update({ vlan: v === null ? null : Number(v) })}
          options={vlans.map((v) => ({ value: String(v), label: `VLAN ${v}` }))}
          allowNone
          noneLabel="All VLANs"
        />
      </FilterSection>
      <FilterSection title="Device type">
        <SelectField<DeviceType>
          ariaLabel="Device type"
          value={ctx.types[0] ?? null}
          onChange={(t) => update({ types: t ? [t] : [] })}
          options={TYPE_OPTIONS}
          allowNone
          noneLabel="All types"
        />
      </FilterSection>
      <FilterSection title="Protocol">
        <SelectField<Protocol>
          ariaLabel="Protocol"
          value={ctx.proto}
          onChange={(proto) => update({ proto })}
          options={PROTOCOL_OPTIONS}
          allowNone
        />
      </FilterSection>
      <FilterSection title="Minimum traffic">
        <SelectField<string>
          ariaLabel="Minimum traffic"
          value={String(ctx.min)}
          onChange={(v) => update({ min: Number(v ?? 0) })}
          options={MIN_BPS_OPTIONS}
        />
      </FilterSection>
      <FilterSection title="Show">
        <SelectField<string>
          ariaLabel="Top N edges"
          value={String(ctx.top ?? 250)}
          onChange={(v) => update({ top: v === "250" ? null : Number(v) })}
          options={TOP_OPTIONS}
        />
        <div className="flex items-center justify-between pt-1">
          <Label htmlFor="inactive" className="text-xs">
            Include inactive devices
          </Label>
          <Switch id="inactive" checked={ctx.inactive} onCheckedChange={(inactive) => update({ inactive })} />
        </div>
      </FilterSection>
      <HomeLegend mode={ctx.mode} />
    </>
  );

  return (
    <Workspace summary={summary} filters={filters} inspectorTitle="Device and flow inspector" inspector={<HomeInspector traffic={traffic} />}>
      {traffic && topology ? (
        <HomeCanvas
          mode={ctx.mode}
          traffic={traffic}
          topology={topology}
          selectedId={ctx.sel}
          focusId={ctx.src}
          animate={particles && !reduced}
          onSelect={(sel) => update({ sel })}
        />
      ) : (
        <div className="grid h-full place-items-center text-sm text-muted-foreground">Loading network…</div>
      )}
      <ContextChips traffic={traffic} focusLabel={labelOf(ctx.src)} />
      {ctx.mode === "traffic" && traffic && traffic.edges.length === 0 && (
        <div className="pointer-events-none absolute inset-0 grid place-items-center">
          <p className="rounded-md bg-background/80 px-4 py-2 text-sm text-muted-foreground">
            No matching traffic in this window
          </p>
        </div>
      )}
    </Workspace>
  );
}

function ContextChips({ traffic, focusLabel }: { traffic: HomeTrafficResponse | undefined; focusLabel: string | null }) {
  const [ctx, update] = useInvestigation();
  const dest = traffic?.destination_context;
  if (!ctx.src && !ctx.dst) return null;
  return (
    <div className="pointer-events-none absolute inset-x-3 top-3 z-10 flex flex-wrap gap-2">
      {ctx.dst && (
        <span className="pointer-events-auto flex items-center gap-1 rounded-full border border-amber-300/50 bg-background/85 py-0.5 pr-1 pl-2.5 text-xs backdrop-blur">
          Destination: <strong className="font-medium">{dest?.label ?? ctx.dst}</strong>
          <span className="text-muted-foreground">
            · {dest?.source_node_ids.length ?? 0} source{dest?.source_node_ids.length === 1 ? "" : "s"}
          </span>
          <Link href={hrefFor("globe", ctx)} className="ml-1 text-sky-300 hover:underline">
            Globe
          </Link>
          <Button variant="ghost" size="icon-xs" aria-label="Clear destination filter" onClick={() => update({ dst: null })}>
            <X />
          </Button>
        </span>
      )}
      {ctx.src && (
        <span className="pointer-events-auto flex items-center gap-1 rounded-full border border-sky-300/50 bg-background/85 py-0.5 pr-1 pl-2.5 text-xs backdrop-blur">
          Focus: <strong className="font-medium">{focusLabel}</strong>
          <Button variant="ghost" size="icon-xs" aria-label="Clear focus" onClick={() => update({ src: null })}>
            <X />
          </Button>
        </span>
      )}
    </div>
  );
}

function HomeLegend({ mode }: { mode: HomeMode }) {
  return (
    <FilterSection title="Legend">
      <ul className="space-y-1 text-[11px] text-muted-foreground">
        {mode !== "topology" && (
          <>
            <li className="flex items-center gap-2">
              <span className="h-1 w-5 rounded" style={{ background: COLORS.internal }} /> internal traffic
            </li>
            <li className="flex items-center gap-2">
              <span className="h-1 w-5 rounded" style={{ background: COLORS.outbound }} /> external, mostly ↑ out
            </li>
            <li className="flex items-center gap-2">
              <span className="h-1 w-5 rounded" style={{ background: COLORS.inbound }} /> external, mostly ↓ in
            </li>
            <li>Width = sampled estimate (log scale). ▶ = dominant direction.</li>
          </>
        )}
        {mode !== "traffic" && (
          <>
            {(["manual", "lldp", "wlc", "inferred"] as const).map((e) => (
              <li key={e} className="flex items-center gap-2">
                <svg width="20" height="4" aria-hidden>
                  <line x1="0" y1="2" x2="20" y2="2" stroke={EVIDENCE_STYLE[e].color} strokeWidth="2" strokeDasharray={EVIDENCE_STYLE[e].dash} />
                </svg>
                {e === "wlc" ? "Wi-Fi association (WLC)" : e === "inferred" ? "inferred (uncertain)" : e === "lldp" ? "LLDP/CDP" : "manual / confirmed"}
              </li>
            ))}
          </>
        )}
        {mode === "hybrid" && (
          <li>Traffic edges are logical and are not routed along topology links.</li>
        )}
      </ul>
    </FilterSection>
  );
}
