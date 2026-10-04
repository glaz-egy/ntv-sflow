/**
 * Destination grouping keys and labels (Country / City / ASN / IP).
 * Key format is defined in `@/contracts` (DestinationKey).
 */
import { compareStrings } from "../strings";
import type { DestinationKey, GeoLocation, Grouping } from "@/contracts";
import type { GeoRecord } from "../domain";
import { COUNTRY_ANCHORS } from "../geodb";

export const GROUPINGS: readonly Grouping[] = ["country", "city", "asn", "ip"];

export function isGrouping(value: string): value is Grouping {
  return (GROUPINGS as readonly string[]).includes(value);
}

export function destinationKey(grouping: Grouping, ip: string, geo: GeoRecord): DestinationKey {
  switch (grouping) {
    case "ip":
      return `ip:${ip}`;
    case "asn":
      return geo.asn !== null ? `asn:${geo.asn}` : "asn:unknown";
    case "country":
      return geo.countryCode ? `country:${geo.countryCode}` : "country:unknown";
    case "city":
      return geo.countryCode && geo.city ? `city:${geo.countryCode}:${geo.city}` : "city:unknown";
  }
}

export function parseDestinationKey(
  key: string,
): { grouping: Grouping; value: string } | null {
  const i = key.indexOf(":");
  if (i <= 0) return null;
  const grouping = key.slice(0, i);
  const value = key.slice(i + 1);
  if (!isGrouping(grouping) || value.length === 0) return null;
  return { grouping, value };
}

export function destinationLabel(grouping: Grouping, ip: string, geo: GeoRecord): string {
  switch (grouping) {
    case "ip":
      return ip;
    case "asn":
      if (geo.asn === null) return "Unknown ASN";
      return geo.organization ?? `AS${geo.asn}`;
    case "country":
      return geo.countryName ?? geo.countryCode ?? "Unknown country";
    case "city":
      return geo.city && geo.countryCode ? `${geo.city}, ${geo.countryCode}` : "Unknown city";
  }
}

/**
 * Marker location for a group. Returns null when unknown — callers must not
 * substitute a placeholder coordinate.
 *
 * `members` is (geo, weight) for every flow in the group; used for the
 * ASN "dominant location" anchor.
 */
export function groupLocation(
  grouping: Grouping,
  members: Array<{ geo: GeoRecord; weight: number }>,
): GeoLocation | null {
  const first = members[0]?.geo;
  if (!first) return null;
  switch (grouping) {
    case "ip":
    case "city":
      return first.latitude !== null && first.longitude !== null
        ? { latitude: first.latitude, longitude: first.longitude, basis: grouping === "ip" ? "geoip" : "city" }
        : null;
    case "country": {
      if (!first.countryCode) return null;
      const anchor = COUNTRY_ANCHORS[first.countryCode];
      if (anchor) return { ...anchor, basis: "country_anchor" };
      // No configured anchor: use the highest-traffic located member.
      const loc = dominantLocation(members);
      return loc ? { ...loc, basis: "country_dominant" } : null;
    }
    case "asn": {
      const loc = dominantLocation(members);
      return loc ? { ...loc, basis: "asn_dominant" } : null;
    }
  }
}

function dominantLocation(
  members: Array<{ geo: GeoRecord; weight: number }>,
): { latitude: number; longitude: number } | null {
  const weights = new Map<string, { lat: number; lon: number; w: number }>();
  for (const m of members) {
    if (m.geo.latitude === null || m.geo.longitude === null) continue;
    const k = `${m.geo.latitude},${m.geo.longitude}`;
    const e = weights.get(k) ?? { lat: m.geo.latitude, lon: m.geo.longitude, w: 0 };
    e.w += m.weight;
    weights.set(k, e);
  }
  let best: { lat: number; lon: number; w: number } | null = null;
  for (const [, e] of [...weights].sort(([a], [b]) => compareStrings(a, b))) {
    if (!best || e.w > best.w) best = e;
  }
  return best ? { latitude: best.lat, longitude: best.lon } : null;
}
