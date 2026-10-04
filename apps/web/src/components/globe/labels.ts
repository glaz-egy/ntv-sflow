/** Tooltip HTML shared by the 3D globe and the Mercator map. */
import type { GlobeDestination, GlobeOrigin, Measurement } from "@/contracts";
import { formatMeasurement } from "@/lib/format/measurement";

export function angularDistance(aLat: number, aLng: number, bLat: number, bLng: number): number {
  const r = Math.PI / 180;
  const dLat = (bLat - aLat) * r;
  const dLng = (bLng - aLng) * r;
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(aLat * r) * Math.cos(bLat * r) * Math.sin(dLng / 2) ** 2;
  return 2 * Math.asin(Math.min(1, Math.sqrt(h)));
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

function tooltip(title: string, lines: string[]): string {
  return `<div style="font:12px var(--font-geist-sans),sans-serif;background:rgba(15,23,42,.92);border:1px solid rgba(148,163,184,.25);border-radius:6px;padding:6px 8px;color:#e2e8f0">
<div style="font-weight:600;margin-bottom:2px">${escapeHtml(title)}</div>${lines.map((l) => `<div>${escapeHtml(l)}</div>`).join("")}</div>`;
}

export function originLabel(origin: GlobeOrigin): string {
  return tooltip(origin.label, [`Origin (${origin.precision} precision) — not an exact location`]);
}

export function destinationLabel(d: GlobeDestination): string {
  const basis = d.location!.basis;
  return tooltip(d.label, [
    `↓ in ${formatMeasurement(d.inbound_bps)}  ↑ out ${formatMeasurement(d.outbound_bps)}`,
    `${d.source_device_count} internal source${d.source_device_count === 1 ? "" : "s"}`,
    basis === "geoip" ? "Approximate GeoIP location" : `Marker: ${basis.replace("_", " ")}`,
  ]);
}

export function arcLabel(d: GlobeDestination, dir: "inbound" | "outbound", m: Measurement): string {
  return tooltip(`${dir === "outbound" ? "↑ Outbound to" : "↓ Inbound from"} ${d.label}`, [
    formatMeasurement(m),
    "Arc = GeoIP relation, not the network path",
  ]);
}
