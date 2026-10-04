"use client";

import dynamic from "next/dynamic";
import { X } from "lucide-react";
import type { DirectionFilter, Grouping, Protocol } from "@/contracts";
import { Rate } from "@/components/common/rate";
import { MIN_BPS_OPTIONS, OptionGroup, PROTOCOL_OPTIONS, SelectField } from "@/components/common/fields";
import { FilterSection, SummaryItem, Workspace } from "@/components/shell/workspace";
import { Button } from "@/components/ui/button";
import { useLiveQuery } from "@/lib/data/data-context";
import { withGrouping } from "@/lib/state/context";
import { useHistoryWindow } from "@/lib/state/use-history-window";
import { useInvestigation, useReducedMotionPreference } from "@/lib/state/use-investigation";
import { useUiStore } from "@/lib/state/ui-store";
import { COLORS } from "@/lib/viz/colors";
import { DestinationInspector } from "./destination-inspector";

const GlobeCanvas = dynamic(() => import("./globe-canvas"), {
  ssr: false,
  loading: () => <div className="absolute inset-0 grid place-items-center text-sm text-muted-foreground">Loading globe…</div>,
});

const GROUPING_OPTIONS: Array<{ value: Grouping; label: string }> = [
  { value: "country", label: "Country" },
  { value: "city", label: "City" },
  { value: "asn", label: "ASN" },
  { value: "ip", label: "IP" },
];

const TOP_OPTIONS = ["10", "25", "50", "100"].map((v) => ({ value: v, label: `Top ${v}` }));

export function GlobeView() {
  const [ctx, update] = useInvestigation();
  const particles = useUiStore((s) => s.particles);
  const motion = useUiStore((s) => s.motion);
  const reduced = useReducedMotionPreference(motion);
  const { range } = useHistoryWindow();

  const query = {
    start: range?.start,
    end: range?.end,
    grouping: ctx.grouping,
    source_node_id: ctx.src,
    direction: ctx.dir,
    protocol: ctx.proto,
    min_bps: ctx.min,
    limit: ctx.top ?? 100,
  };
  const { data, loading } = useLiveQuery((p) => p.getGlobe(query), [JSON.stringify(query)], { live: !range });
  const { data: topology } = useLiveQuery((p) => p.getTopology(range ?? undefined), [range?.start, range?.end], {
    live: !range,
  });

  const sourceOptions = (topology?.nodes ?? [])
    .filter((n) => n.kind === "device" && !["router", "switch", "wireless_ap", "firewall"].includes(n.device_type))
    .map((n) => ({ value: n.id, label: n.label }));
  if (ctx.src && !sourceOptions.some((o) => o.value === ctx.src)) {
    sourceOptions.push({ value: ctx.src, label: ctx.src.replace(/^ep:/, "") });
  }
  const sourceLabel = sourceOptions.find((o) => o.value === ctx.src)?.label ?? ctx.src;

  const destinations = data?.destinations ?? [];
  const unlocated = destinations.filter((d) => d.location === null);

  const summary = (
    <div className="flex items-center divide-x divide-border/60">
      <SummaryItem label="↓ Download" accent={COLORS.inbound}>
        <Rate m={data?.summary.download} />
      </SummaryItem>
      <SummaryItem label="↑ Upload" accent={COLORS.outbound}>
        <Rate m={data?.summary.upload} />
      </SummaryItem>
      <SummaryItem label="Total">
        <Rate
          m={
            data && {
              ...data.summary.download,
              value: data.summary.download.value + data.summary.upload.value,
              sample_count: undefined,
            }
          }
        />
      </SummaryItem>
      <SummaryItem label="Destinations">
        <span className="tabular">
          {destinations.length}
          {data && data.truncated_count > 0 && (
            <span className="text-muted-foreground"> (+{data.truncated_count} hidden)</span>
          )}
        </span>
      </SummaryItem>
      <span className="px-3 text-[11px] text-muted-foreground max-xl:hidden">
        WAN totals: {data?.summary.basis === "boundary_counter" ? "boundary interface counters" : "sampled estimate"}
      </span>
    </div>
  );

  const filters = (
    <>
      <FilterSection title="Grouping">
        <OptionGroup<Grouping>
          ariaLabel="Destination grouping"
          value={ctx.grouping}
          onChange={(g) => update((c) => withGrouping(c, g))}
          options={GROUPING_OPTIONS}
        />
      </FilterSection>
      <FilterSection title="Direction">
        <OptionGroup<DirectionFilter>
          ariaLabel="Direction"
          value={ctx.dir}
          onChange={(dir) => update({ dir })}
          options={[
            { value: "both", label: "Both" },
            { value: "inbound", label: "↓ In" },
            { value: "outbound", label: "↑ Out" },
          ]}
        />
      </FilterSection>
      <FilterSection title="Source device">
        <SelectField<string>
          ariaLabel="Source device"
          value={ctx.src}
          onChange={(src) => update({ src })}
          options={sourceOptions}
          allowNone
          noneLabel="All devices"
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
          ariaLabel="Top N destinations"
          value={String(ctx.top ?? 100)}
          onChange={(v) => update({ top: v === "100" ? null : Number(v) })}
          options={TOP_OPTIONS}
        />
      </FilterSection>
    </>
  );

  return (
    <Workspace
      summary={summary}
      filters={filters}
      inspectorTitle="Destination inspector"
      inspector={
        <DestinationInspector
          globe={data}
          ctx={ctx}
          onSelect={(dst) => update({ dst })}
          sourceLabel={sourceLabel ?? null}
        />
      }
    >
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_center,oklch(0.2_0.04_255)_0%,transparent_70%)]" />
      {data && (
        <GlobeCanvas
          origin={data.origin}
          destinations={destinations}
          selectedKey={ctx.dst}
          direction={ctx.dir}
          animate={particles && !reduced}
          onSelect={(dst) => update({ dst })}
        />
      )}

      <div className="pointer-events-none absolute inset-x-3 top-3 flex flex-wrap items-start gap-2">
        {ctx.src && (
          <span className="pointer-events-auto flex items-center gap-1 rounded-full border border-border/60 bg-background/80 py-0.5 pr-1 pl-2.5 text-xs backdrop-blur">
            Source: <strong className="font-medium">{sourceLabel}</strong>
            <Button variant="ghost" size="icon-xs" aria-label="Clear source filter" onClick={() => update({ src: null })}>
              <X />
            </Button>
          </span>
        )}
        {ctx.proto && (
          <span className="pointer-events-auto flex items-center gap-1 rounded-full border border-border/60 bg-background/80 py-0.5 pr-1 pl-2.5 text-xs backdrop-blur">
            Protocol: <strong className="font-medium uppercase">{ctx.proto}</strong>
            <Button variant="ghost" size="icon-xs" aria-label="Clear protocol filter" onClick={() => update({ proto: null })}>
              <X />
            </Button>
          </span>
        )}
      </div>

      {data && destinations.length === 0 && !loading && (
        <div className="absolute inset-0 grid place-items-center">
          <p className="rounded-md bg-background/80 px-4 py-2 text-sm text-muted-foreground backdrop-blur">
            No matching traffic in this window
          </p>
        </div>
      )}

      <div className="absolute bottom-3 left-3 flex max-w-[calc(100%-1.5rem)] flex-col gap-2">
        {unlocated.length > 0 && (
          <div className="w-64 rounded-md border border-border/60 bg-background/80 p-2 text-xs backdrop-blur">
            <div className="mb-1 font-medium text-muted-foreground">Unknown location ({unlocated.length})</div>
            <ul className="space-y-0.5">
              {unlocated.slice(0, 5).map((d) => (
                <li key={d.key}>
                  <button
                    className="flex w-full justify-between gap-2 rounded px-1 text-left hover:bg-accent aria-pressed:bg-accent"
                    aria-pressed={ctx.dst === d.key}
                    onClick={() => update({ dst: d.key })}
                  >
                    <span className="truncate">{d.label}</span>
                    <Rate m={{ ...d.inbound_bps, value: d.inbound_bps.value + d.outbound_bps.value }} showKind={false} />
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}
        <GlobeLegend />
      </div>
    </Workspace>
  );
}

function GlobeLegend() {
  return (
    <div className="w-64 rounded-md border border-border/60 bg-background/80 p-2 text-[11px] text-muted-foreground backdrop-blur">
      <div className="flex gap-3">
        <span className="flex items-center gap-1">
          <span className="h-0.5 w-4 rounded" style={{ background: COLORS.outbound }} />↑ outbound
        </span>
        <span className="flex items-center gap-1">
          <span className="h-0.5 w-4 rounded" style={{ background: COLORS.inbound }} />↓ inbound
        </span>
        <span className="flex items-center gap-1">
          <span className="size-2 rounded-full" style={{ background: COLORS.origin }} />origin
        </span>
      </div>
      <p className="mt-1 leading-snug">
        Arcs link the origin to approximate GeoIP locations. They are not network paths. Animation is
        illustrative, not per-packet.
      </p>
    </div>
  );
}
