"use client";

import Link from "next/link";
import { ArrowRight, Crosshair, X } from "lucide-react";
import type { HomeTrafficResponse, TrafficEdge } from "@/contracts";
import { Rate } from "@/components/common/rate";
import { Button } from "@/components/ui/button";
import { useLiveQuery } from "@/lib/data/data-context";
import { useHistoryWindow } from "@/lib/state/use-history-window";
import { homeToGlobe, hrefFor, type InvestigationContext } from "@/lib/state/context";
import { useInvestigation } from "@/lib/state/use-investigation";
import { DEVICE_ICONS } from "./graph-elements";

export function HomeInspector({ traffic }: { traffic: HomeTrafficResponse | undefined }) {
  const [ctx] = useInvestigation();
  if (ctx.sel?.startsWith("e:")) return <EdgeInspector traffic={traffic} id={ctx.sel} />;
  if (ctx.sel) return <NodeInspector id={ctx.sel} traffic={traffic} />;
  return <Overview traffic={traffic} />;
}

function Header({ kicker, title, onClose }: { kicker: string; title: string; onClose: () => void }) {
  return (
    <div className="flex items-start justify-between gap-2">
      <div className="min-w-0">
        <div className="text-[11px] tracking-wider text-muted-foreground uppercase">{kicker}</div>
        <h2 className="truncate text-base font-semibold">{title}</h2>
      </div>
      <Button variant="ghost" size="icon-sm" aria-label="Clear selection" onClick={onClose}>
        <X />
      </Button>
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

function Unknown() {
  return <span className="text-muted-foreground italic">unknown</span>;
}

function nodeLabel(traffic: HomeTrafficResponse | undefined, id: string) {
  return traffic?.nodes.find((n) => n.id === id)?.label ?? id.replace(/^(ep|dst):/, "");
}

function Overview({ traffic }: { traffic: HomeTrafficResponse | undefined }) {
  const [, update] = useInvestigation();
  const dest = traffic?.destination_context;
  const edges = traffic?.edges ?? [];
  return (
    <div className="space-y-3 text-sm">
      <div>
        <h2 className="text-base font-semibold">Network</h2>
        <p className="text-xs text-muted-foreground">Select a device or edge to inspect it.</p>
      </div>
      {dest && (
        <div className="rounded-md border border-amber-300/40 bg-amber-300/5 p-2 text-xs">
          <div className="font-medium">Sources talking to {dest.label}</div>
          {dest.source_node_ids.length === 0 ? (
            <p className="text-muted-foreground">None in this window.</p>
          ) : (
            <ul className="mt-1 space-y-0.5">
              {dest.source_node_ids.map((id) => (
                <li key={id}>
                  <button className="hover:underline" onClick={() => update({ sel: id })}>
                    {nodeLabel(traffic, id)}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
      <Section title="Largest relationships">
        <ol className="space-y-0.5">
          {edges.slice(0, 15).map((e) => (
            <li key={e.id}>
              <button
                onClick={() => update({ sel: e.id })}
                className="flex w-full items-center gap-2 rounded px-1 py-0.5 text-left text-xs hover:bg-accent"
              >
                <span className="min-w-0 flex-1 truncate">
                  {nodeLabel(traffic, e.source)} ↔ {nodeLabel(traffic, e.target)}
                </span>
                <Rate m={{ ...e.forward_bps, value: e.forward_bps.value + e.reverse_bps.value }} showKind={false} />
              </button>
            </li>
          ))}
        </ol>
      </Section>
    </div>
  );
}

function NodeInspector({ id, traffic }: { id: string; traffic: HomeTrafficResponse | undefined }) {
  const [ctx, update] = useInvestigation();
  const isDevice = !id.startsWith("dst:") && id !== "internet";
  const { range } = useHistoryWindow();
  const { data: detail, loading } = useLiveQuery(
    (p) => (isDevice ? p.getDevice(id, ctx.grouping, range ?? undefined) : Promise.resolve(null)),
    [id, ctx.grouping, isDevice, range?.start, range?.end],
    { live: !range },
  );

  if (!isDevice) {
    const node = traffic?.nodes.find((n) => n.id === id);
    const globeCtx: InvestigationContext = id === "internet" ? ctx : { ...ctx, dst: id.slice(4) };
    return (
      <div className="space-y-4 text-sm">
        <Header kicker={id === "internet" ? "External aggregate" : "External destination"} title={node?.label ?? id} onClose={() => update({ sel: null })} />
        <p className="text-xs text-muted-foreground">
          External endpoints are collapsed here. Explore them geographically on the globe.
        </p>
        {node && (
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
            <dt className="text-muted-foreground">↓ To internal</dt>
            <dd><Rate m={node.tx_bps} /></dd>
            <dt className="text-muted-foreground">↑ From internal</dt>
            <dd><Rate m={node.rx_bps} /></dd>
          </dl>
        )}
        <Button asChild className="w-full">
          <Link href={hrefFor("globe", globeCtx)}>
            Open in Globe <ArrowRight />
          </Link>
        </Button>
      </div>
    );
  }

  if (!detail) {
    return (
      <div className="space-y-3 text-sm">
        <Header kicker="Device" title={nodeLabel(traffic, id)} onClose={() => update({ sel: null })} />
        <p className="text-xs text-muted-foreground">{loading ? "Loading…" : "Device not found in this window."}</p>
      </div>
    );
  }

  const n = detail.node;
  const Icon = DEVICE_ICONS[n.device_type];
  const focused = ctx.src === id;
  return (
    <div className="space-y-4 text-sm">
      <Header
        kicker={n.kind === "unresolved_endpoint" ? "Unresolved device" : n.device_type.replace("_", " ")}
        title={n.label}
        onClose={() => update({ sel: null })}
      />
      <div className="grid gap-2">
        <Button asChild>
          <Link href={hrefFor("globe", homeToGlobe(ctx, id))}>
            Show external destinations in Globe <ArrowRight />
          </Link>
        </Button>
        <Button variant="outline" onClick={() => update({ src: focused ? null : id })}>
          <Crosshair /> {focused ? "Clear focus" : "Focus device"}
        </Button>
      </div>

      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
        <dt className="text-muted-foreground">Type</dt>
        <dd className="flex items-center gap-1">
          <Icon className="size-3.5" /> {n.device_type.replace("_", " ")}
        </dd>
        <dt className="text-muted-foreground">Status</dt>
        <dd>{n.status}</dd>
        <dt className="text-muted-foreground">↓ RX</dt>
        <dd><Rate m={n.rx_bps} /></dd>
        <dt className="text-muted-foreground">↑ TX</dt>
        <dd><Rate m={n.tx_bps} /></dd>
        <dt className="text-muted-foreground">Addresses</dt>
        <dd className="space-y-0.5">
          {detail.addresses.map((a) => (
            <div key={a.address} className="font-mono">
              {a.address} <span className="font-sans text-muted-foreground">({a.source})</span>
            </div>
          ))}
        </dd>
        <dt className="text-muted-foreground">MAC</dt>
        <dd className="font-mono">{detail.macs.length ? detail.macs.join(", ") : <Unknown />}</dd>
        <dt className="text-muted-foreground">VLAN</dt>
        <dd>{n.vlan_id ?? <Unknown />}{n.network_name && <span className="text-muted-foreground"> · {n.network_name}</span>}</dd>
        <dt className="text-muted-foreground">SSID</dt>
        <dd>{detail.ssid ?? <Unknown />}</dd>
        <dt className="text-muted-foreground">Attached to</dt>
        <dd>
          {detail.attachment ? (
            <>
              {detail.attachment.via_label}
              {detail.attachment.interface && <span className="font-mono"> {detail.attachment.interface}</span>}
              <span className="text-muted-foreground"> ({detail.attachment.evidence})</span>
            </>
          ) : (
            <Unknown />
          )}
        </dd>
        <dt className="text-muted-foreground">Last seen</dt>
        <dd className="tabular">{n.last_seen ? new Date(n.last_seen).toLocaleTimeString(undefined, { hour12: false }) : <Unknown />}</dd>
      </dl>

      <Section title="Internal peers">
        {detail.top_internal_peers.length === 0 ? (
          <p className="text-xs text-muted-foreground">None in this window.</p>
        ) : (
          <ul className="space-y-0.5">
            {detail.top_internal_peers.map((p) => (
              <li key={p.node_id}>
                <button className="flex w-full items-center gap-2 rounded px-1 text-left text-xs hover:bg-accent" onClick={() => update({ sel: p.node_id })}>
                  <span className="min-w-0 flex-1 truncate">{p.label}</span>
                  <Rate m={p.tx_bps} showKind={false} className="text-amber-300" />
                  <Rate m={p.rx_bps} showKind={false} className="text-cyan-300" />
                </button>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Section title={`External destinations (${ctx.grouping.toUpperCase()})`}>
        {detail.top_external_destinations.length === 0 ? (
          <p className="text-xs text-muted-foreground">None in this window.</p>
        ) : (
          <ul className="space-y-0.5">
            {detail.top_external_destinations.map((d) => (
              <li key={d.key}>
                <Link
                  href={hrefFor("globe", { ...homeToGlobe(ctx, id), dst: d.key })}
                  className="flex items-center gap-2 rounded px-1 text-xs hover:bg-accent"
                >
                  <span className="min-w-0 flex-1 truncate">{d.label}</span>
                  <Rate m={d.outbound_bps} showKind={false} className="text-amber-300" />
                  <Rate m={d.inbound_bps} showKind={false} className="text-cyan-300" />
                </Link>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </div>
  );
}

function EdgeInspector({ traffic, id }: { traffic: HomeTrafficResponse | undefined; id: string }) {
  const [, update] = useInvestigation();
  const edge: TrafficEdge | undefined = traffic?.edges.find((e) => e.id === id);
  if (!edge) {
    return (
      <div className="space-y-3 text-sm">
        <Header kicker="Flow" title="No traffic" onClose={() => update({ sel: null })} />
        <p className="text-xs text-muted-foreground">This relationship has no matching traffic in the current window.</p>
      </div>
    );
  }
  const a = nodeLabel(traffic, edge.source);
  const b = nodeLabel(traffic, edge.target);
  return (
    <div className="space-y-4 text-sm">
      <Header kicker={`${edge.scope} traffic`} title={`${a} ↔ ${b}`} onClose={() => update({ sel: null })} />
      <p className="text-[11px] text-muted-foreground">Logical communication between endpoints, not a physical path.</p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
        <dt className="text-muted-foreground">{a} → {b}</dt>
        <dd><Rate m={edge.forward_bps} /></dd>
        <dt className="text-muted-foreground">{b} → {a}</dt>
        <dd><Rate m={edge.reverse_bps} /></dd>
        <dt className="text-muted-foreground">Samples</dt>
        <dd className="tabular">{(edge.forward_bps.sample_count ?? 0) + (edge.reverse_bps.sample_count ?? 0)} in {edge.forward_bps.window_seconds} s window</dd>
        <dt className="text-muted-foreground">Last seen</dt>
        <dd className="tabular">{new Date(edge.last_seen).toLocaleTimeString(undefined, { hour12: false })}</dd>
      </dl>
      <div className="flex gap-2">
        <Button size="sm" variant="outline" onClick={() => update({ sel: edge.source })}>{a}</Button>
        {!edge.target.startsWith("dst:") && edge.target !== "internet" && (
          <Button size="sm" variant="outline" onClick={() => update({ sel: edge.target })}>{b}</Button>
        )}
      </div>
      <Section title="Protocols / ports">
        <ul className="space-y-1">
          {edge.top_protocols.map((p) => (
            <li key={`${p.protocol}/${p.port}`} className="flex justify-between text-xs">
              <span className="font-mono">
                {p.protocol}/{p.port ?? "—"} {p.service && <span className="text-muted-foreground">{p.service}</span>}
              </span>
              <Rate m={p.bps} showKind={false} />
            </li>
          ))}
        </ul>
      </Section>
      <Section title="Observation points">
        <ul className="space-y-1 text-xs">
          {edge.observation_points.map((o) => (
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
      </Section>
    </div>
  );
}
