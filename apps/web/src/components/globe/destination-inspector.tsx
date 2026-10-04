"use client";

import Link from "next/link";
import { useState } from "react";
import { ArrowRight, MapPinOff, X } from "lucide-react";
import type { GlobeResponse } from "@/contracts";
import { Rate } from "@/components/common/rate";
import { Button } from "@/components/ui/button";
import { useLiveQuery } from "@/lib/data/data-context";
import { useHistoryWindow } from "@/lib/state/use-history-window";
import { globeToHome, hrefFor, type InvestigationContext } from "@/lib/state/context";
import { COLORS } from "@/lib/viz/colors";

const BASIS_TEXT = {
  geoip: "Approximate GeoIP coordinate",
  city: "City coordinate (approximate)",
  country_anchor: "Country anchor point (not a real location)",
  country_dominant: "Placed at this country's highest-traffic location in the window",
  asn_dominant: "Placed at this ASN's highest-traffic location in the window",
} as const;

export function DestinationInspector({
  globe,
  ctx,
  onSelect,
  sourceLabel,
}: {
  globe: GlobeResponse | undefined;
  ctx: InvestigationContext;
  onSelect: (key: string | null) => void;
  sourceLabel: string | null;
}) {
  const { range } = useHistoryWindow();
  const query = { source_node_id: ctx.src, protocol: ctx.proto, start: range?.start, end: range?.end };
  const { data: detail, loading } = useLiveQuery(
    (p) => (ctx.dst ? p.getDestination(ctx.dst, query) : Promise.resolve(null)),
    [ctx.dst, JSON.stringify(query)],
    { live: !range },
  );

  if (!ctx.dst) {
    return <TopDestinations globe={globe} onSelect={onSelect} />;
  }

  const d = detail?.destination ?? globe?.destinations.find((x) => x.key === ctx.dst);
  // A grouped destination can span several countries/cities: that is
  // "multiple", not "unknown".
  const members = detail?.members ?? [];
  const multiple = (values: Array<string | null>) => new Set(values.filter(Boolean)).size > 1;
  const multiCountry = multiple(members.map((m) => m.country_code));
  const multiCity = multiple(members.map((m) => m.city));
  return (
    <div className="space-y-4 text-sm">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="text-[11px] tracking-wider text-muted-foreground uppercase">{ctx.dst.split(":")[0]}</div>
          <h2 className="truncate text-base font-semibold">{d?.label ?? ctx.dst.slice(ctx.dst.indexOf(":") + 1)}</h2>
        </div>
        <Button variant="ghost" size="icon-sm" aria-label="Clear selection" onClick={() => onSelect(null)}>
          <X />
        </Button>
      </div>

      <Button asChild className="w-full">
        <Link href={hrefFor("home", globeToHome(ctx, ctx.dst))}>
          Open in Home Network <ArrowRight />
        </Link>
      </Button>

      {!detail && !loading && (
        <p className="rounded-md bg-muted/50 p-2 text-xs text-muted-foreground">
          No matching traffic for this destination in the current window
          {sourceLabel ? ` from ${sourceLabel}` : ""}.
        </p>
      )}

      {d && (
        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
          <dt className="text-muted-foreground" style={{ color: COLORS.inbound }}>↓ Inbound</dt>
          <dd><Rate m={d.inbound_bps} /></dd>
          <dt className="text-muted-foreground" style={{ color: COLORS.outbound }}>↑ Outbound</dt>
          <dd><Rate m={d.outbound_bps} /></dd>
          {d.ip && (<><dt className="text-muted-foreground">IP</dt><dd className="font-mono">{d.ip}</dd></>)}
          <dt className="text-muted-foreground">Country</dt>
          <dd>{d.country_name ?? d.country_code ?? (multiCountry ? <Multiple /> : <Unknown />)}</dd>
          <dt className="text-muted-foreground">City</dt>
          <dd>{d.city ?? (multiCity ? <Multiple /> : <Unknown />)}</dd>
          <dt className="text-muted-foreground">ASN</dt>
          <dd>{d.asn !== null ? `AS${d.asn}` : <Unknown />}</dd>
          <dt className="text-muted-foreground">Organization</dt>
          <dd>{d.organization ?? <Unknown />}</dd>
          <dt className="text-muted-foreground">Location</dt>
          <dd>
            {d.location ? (
              BASIS_TEXT[d.location.basis]
            ) : (
              <span className="inline-flex items-center gap-1 text-amber-300">
                <MapPinOff className="size-3" /> Unknown — not shown on globe
              </span>
            )}
          </dd>
          <dt className="text-muted-foreground">Last seen</dt>
          <dd className="tabular">{new Date(d.last_seen).toLocaleTimeString(undefined, { hour12: false })}</dd>
        </dl>
      )}

      {detail && (
        <>
          <Section title={`Internal sources (${detail.top_sources.length})`}>
            <ul className="space-y-1">
              {detail.top_sources.map((s) => (
                <li key={s.node_id} className="flex items-center justify-between gap-2 text-xs">
                  <span className="min-w-0 truncate">
                    {s.label}
                    {!s.resolved && <span className="ml-1 text-amber-300">(unresolved)</span>}
                  </span>
                  <span className="flex gap-2">
                    <Rate m={s.inbound_bps} showKind={false} className="text-cyan-300" />
                    <Rate m={s.outbound_bps} showKind={false} className="text-amber-300" />
                  </span>
                </li>
              ))}
            </ul>
          </Section>

          <Section title="Protocols / ports">
            <ul className="space-y-1">
              {detail.top_protocols.map((p) => (
                <li key={`${p.protocol}/${p.port}`} className="flex justify-between text-xs">
                  <span className="font-mono">
                    {p.protocol}/{p.port ?? "—"} {p.service && <span className="text-muted-foreground">{p.service}</span>}
                  </span>
                  <Rate m={p.bps} showKind={false} />
                </li>
              ))}
            </ul>
          </Section>

          {detail.members.length > 1 && (
            <Section title="Addresses in group">
              <ul className="space-y-1">
                {detail.members.map((m) => (
                  <li key={m.ip} className="flex justify-between gap-2 text-xs">
                    <span className="truncate font-mono">{m.ip}</span>
                    <span className="text-muted-foreground">{m.city ?? "?"}</span>
                    <Rate m={m.total_bps} showKind={false} />
                  </li>
                ))}
              </ul>
            </Section>
          )}

          <Section title="Observation points">
            <ul className="space-y-1 text-xs">
              {detail.observation_points.map((o) => (
                <li key={o.exporter_id} className="flex justify-between">
                  <span>
                    {o.exporter_name}
                    <span className="text-muted-foreground"> if {o.input_if_index ?? "?"}→{o.output_if_index ?? "?"} · 1:{o.sampling_rate}</span>
                  </span>
                  <span className={o.used_for_aggregate ? "text-emerald-300" : "text-muted-foreground"}>
                    {o.used_for_aggregate ? "used" : "also seen"}
                  </span>
                </li>
              ))}
            </ul>
            <p className="mt-1 text-[11px] text-muted-foreground">
              Observations at multiple exporters are not summed.
            </p>
          </Section>
        </>
      )}
    </div>
  );
}

function TopDestinations({
  globe,
  onSelect,
}: {
  globe: GlobeResponse | undefined;
  onSelect: (key: string) => void;
}) {
  // Freeze row order while the pointer is over the list so live re-ranking
  // does not move the row under the cursor.
  const [frozen, setFrozen] = useState<string[] | null>(null);
  const live = (globe?.destinations ?? []).slice(0, 25);
  const rows = frozen
    ? [
        ...frozen.map((k) => live.find((d) => d.key === k)).filter((d): d is (typeof live)[number] => !!d),
        ...live.filter((d) => !frozen.includes(d.key)),
      ]
    : live;
  return (
    <div className="space-y-3 text-sm">
      <div>
        <h2 className="text-base font-semibold">Destinations</h2>
        <p className="text-xs text-muted-foreground">Select a marker, arc or row to inspect.</p>
      </div>
      <ol
        className="space-y-0.5"
        onPointerEnter={() => setFrozen(live.map((d) => d.key))}
        onPointerLeave={() => setFrozen(null)}
      >
        {rows.map((d, i) => (
          <li key={d.key}>
            <button
              onClick={() => onSelect(d.key)}
              className="flex w-full items-center gap-2 rounded px-1.5 py-1 text-left text-xs hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
            >
              <span className="tabular w-5 text-right text-muted-foreground">{i + 1}</span>
              <span className="min-w-0 flex-1 truncate">
                {d.label}
                {d.location === null && <MapPinOff className="ml-1 inline size-3 text-amber-300" aria-label="unknown location" />}
              </span>
              <Rate m={d.inbound_bps} showKind={false} className="text-cyan-300" />
              <Rate m={d.outbound_bps} showKind={false} className="text-amber-300" />
            </button>
          </li>
        ))}
      </ol>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-1.5 border-t border-border/60 pt-3">
      <h3 className="text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">{title}</h3>
      {children}
    </section>
  );
}

function Multiple() {
  return <span className="text-muted-foreground">multiple — see addresses</span>;
}

function Unknown() {
  return <span className="text-muted-foreground italic">unknown</span>;
}
