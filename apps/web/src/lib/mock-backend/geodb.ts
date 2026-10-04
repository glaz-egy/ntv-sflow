/**
 * Mock GeoIP/ASN database (docs/MOCK_DATA.md §4).
 *
 * IPs come from documentation ranges (192.0.2.0/24, 198.51.100.0/24,
 * 203.0.113.0/24, 2001:db8::/32). Organization/ASN labels are illustrative
 * only and do NOT reflect where those orgs' real addresses are located.
 * Coordinates are approximate city points; GeoIP is never a route (D-010).
 */
import type { GeoRecord } from "./domain";

export interface City {
  name: string;
  countryCode: string;
  latitude: number;
  longitude: number;
}

export const COUNTRY_NAMES: Record<string, string> = {
  JP: "Japan",
  US: "United States",
  DE: "Germany",
  NL: "Netherlands",
  SG: "Singapore",
  GB: "United Kingdom",
  AU: "Australia",
  BR: "Brazil",
  IN: "India",
  KR: "South Korea",
  IE: "Ireland",
  SE: "Sweden",
  FR: "France",
  CA: "Canada",
  HK: "Hong Kong",
};

/**
 * Representative point per country for `country` grouping (D-022).
 * A visual anchor, not a claim about where traffic terminates.
 */
export const COUNTRY_ANCHORS: Record<string, { latitude: number; longitude: number }> = {
  JP: { latitude: 36.2, longitude: 138.25 },
  US: { latitude: 39.5, longitude: -98.35 },
  DE: { latitude: 51.17, longitude: 10.45 },
  NL: { latitude: 52.13, longitude: 5.29 },
  SG: { latitude: 1.35, longitude: 103.82 },
  GB: { latitude: 54.0, longitude: -2.0 },
  AU: { latitude: -25.27, longitude: 133.78 },
  BR: { latitude: -14.24, longitude: -51.93 },
  IN: { latitude: 22.0, longitude: 79.0 },
  KR: { latitude: 36.5, longitude: 127.8 },
  IE: { latitude: 53.41, longitude: -8.24 },
  SE: { latitude: 62.0, longitude: 15.0 },
  FR: { latitude: 46.6, longitude: 2.2 },
  CA: { latitude: 56.13, longitude: -106.35 },
  HK: { latitude: 22.32, longitude: 114.17 },
};

export const CITIES: City[] = [
  { name: "Tokyo", countryCode: "JP", latitude: 35.68, longitude: 139.69 },
  { name: "Osaka", countryCode: "JP", latitude: 34.69, longitude: 135.5 },
  { name: "San Jose", countryCode: "US", latitude: 37.34, longitude: -121.89 },
  { name: "Ashburn", countryCode: "US", latitude: 39.04, longitude: -77.49 },
  { name: "Council Bluffs", countryCode: "US", latitude: 41.26, longitude: -95.86 },
  { name: "Seattle", countryCode: "US", latitude: 47.61, longitude: -122.33 },
  { name: "Los Angeles", countryCode: "US", latitude: 34.05, longitude: -118.24 },
  { name: "Frankfurt", countryCode: "DE", latitude: 50.11, longitude: 8.68 },
  { name: "Amsterdam", countryCode: "NL", latitude: 52.37, longitude: 4.9 },
  { name: "Singapore", countryCode: "SG", latitude: 1.35, longitude: 103.82 },
  { name: "London", countryCode: "GB", latitude: 51.51, longitude: -0.13 },
  { name: "Sydney", countryCode: "AU", latitude: -33.87, longitude: 151.21 },
  { name: "Sao Paulo", countryCode: "BR", latitude: -23.55, longitude: -46.63 },
  { name: "Mumbai", countryCode: "IN", latitude: 19.08, longitude: 72.88 },
  { name: "Seoul", countryCode: "KR", latitude: 37.57, longitude: 126.98 },
  { name: "Dublin", countryCode: "IE", latitude: 53.35, longitude: -6.26 },
  { name: "Stockholm", countryCode: "SE", latitude: 59.33, longitude: 18.07 },
  { name: "Paris", countryCode: "FR", latitude: 48.86, longitude: 2.35 },
  { name: "Toronto", countryCode: "CA", latitude: 43.65, longitude: -79.38 },
  { name: "Hong Kong", countryCode: "HK", latitude: 22.32, longitude: 114.17 },
];

export const ORGS: Array<{ asn: number; organization: string }> = [
  { asn: 13335, organization: "Cloudflare" },
  { asn: 15169, organization: "Google" },
  { asn: 16509, organization: "Amazon" },
  { asn: 8075, organization: "Microsoft" },
  { asn: 20940, organization: "Akamai" },
  { asn: 2906, organization: "Netflix" },
  { asn: 32590, organization: "Valve" },
];

function cityRecord(cityName: string, asn: number | null, organization: string | null): GeoRecord {
  const city = CITIES.find((c) => c.name === cityName);
  if (!city) throw new Error(`unknown mock city ${cityName}`);
  return {
    countryCode: city.countryCode,
    countryName: COUNTRY_NAMES[city.countryCode] ?? null,
    city: city.name,
    latitude: city.latitude,
    longitude: city.longitude,
    asn,
    organization,
  };
}

export const UNKNOWN_GEO: GeoRecord = {
  countryCode: null,
  countryName: null,
  city: null,
  latitude: null,
  longitude: null,
  asn: null,
  organization: null,
};

/** Base mock GeoIP entries keyed by external IP. */
export function baseGeoDb(): Map<string, GeoRecord> {
  return new Map<string, GeoRecord>([
    ["198.51.100.10", cityRecord("Tokyo", 13335, "Cloudflare")],
    ["198.51.100.11", cityRecord("San Jose", 13335, "Cloudflare")],
    ["2001:db8:1::10", cityRecord("Frankfurt", 13335, "Cloudflare")],
    ["203.0.113.20", cityRecord("Tokyo", 15169, "Google")],
    ["203.0.113.21", cityRecord("Council Bluffs", 15169, "Google")],
    ["192.0.2.30", cityRecord("Ashburn", 16509, "Amazon")],
    ["192.0.2.31", cityRecord("Singapore", 16509, "Amazon")],
    ["203.0.113.40", cityRecord("Osaka", 8075, "Microsoft")],
    ["203.0.113.41", cityRecord("Amsterdam", 8075, "Microsoft")],
    ["198.51.100.50", cityRecord("Tokyo", 20940, "Akamai")],
    ["203.0.113.80", cityRecord("Tokyo", 2906, "Netflix")],
    ["203.0.113.60", cityRecord("Tokyo", 32590, "Valve")],
    // ASN known, location unknown (documentation ASN range).
    [
      "198.51.100.200",
      { ...UNKNOWN_GEO, asn: 64500, organization: "Example Transit (mock)" },
    ],
    // 192.0.2.250 intentionally absent: fully unknown metadata.
  ]);
}
