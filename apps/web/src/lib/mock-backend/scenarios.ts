/**
 * Mock scenarios (docs/MOCK_DATA.md §8). A scenario is a set of
 * conversations plus inventory/geo overrides. Both Globe and Home are derived
 * from the same conversations — never from per-view datasets.
 */
import type { Protocol } from "@/contracts";
import type { GeoRecord, Inventory } from "./domain";
import { baseGeoDb, CITIES, COUNTRY_NAMES, ORGS } from "./geodb";
import { defaultInventory } from "./inventory";
import { keyedRng } from "./rng";

export type RateShape = "steady" | "burst" | "periodic";

export interface RateProfile {
  /** Mean true rate while active, bits per second. */
  bps: number;
  shape?: RateShape;
  periodSeconds?: number;
  /** Fraction of the period that a burst/periodic flow is active. */
  duty?: number;
  /** Relative amplitude of smooth noise, 0..1. */
  jitter?: number;
}

export interface Conversation {
  id: string;
  client: string;
  server: string;
  protocol: Protocol;
  serverPort: number;
  clientPort: number;
  /** client → server */
  up: RateProfile;
  /** server → client */
  down: RateProfile;
  packetBytes?: { up: number; down: number };
}

export interface Scenario {
  name: ScenarioName;
  description: string;
  conversations: Conversation[];
  geo: Map<string, GeoRecord>;
  inventory: Inventory;
  /** Sim second after which the collector stops delivering data. */
  dataStopsAtTick: number | null;
}

export const SCENARIO_NAMES = [
  "default",
  "heavy-download",
  "internal-backup",
  "many-destinations",
  "unknown-metadata",
  "stale-collector",
] as const;
export type ScenarioName = (typeof SCENARIO_NAMES)[number];

export function isScenarioName(value: string): value is ScenarioName {
  return (SCENARIO_NAMES as readonly string[]).includes(value);
}

const M = 1_000_000;

/** Sim second at which mock providers start (so the first window is full). */
export const START_TICK = 30;

let clientPortCounter = 49152;
function conv(
  id: string,
  client: string,
  server: string,
  protocol: Protocol,
  serverPort: number,
  upMbps: number,
  downMbps: number,
  extra: Partial<Pick<Conversation, "up" | "down" | "packetBytes">> = {},
): Conversation {
  return {
    id,
    client,
    server,
    protocol,
    serverPort,
    clientPort: clientPortCounter++,
    up: { bps: upMbps * M, jitter: 0.25, ...extra.up },
    down: { bps: downMbps * M, jitter: 0.25, ...extra.down },
    packetBytes: extra.packetBytes,
  };
}

function baseConversations(): Conversation[] {
  clientPortCounter = 49152;
  return [
    // PC → NAS high internal traffic (SMB), plus an IPv6 rsync session.
    conv("pc-nas-smb", "10.20.0.10", "10.10.0.10", "tcp", 445, 420, 18),
    conv("pc-nas-ssh6", "fd00:20::10", "fd00:10::10", "tcp", 22, 22, 1.5),
    conv("pc-google", "10.20.0.10", "203.0.113.20", "udp", 443, 3, 55),
    conv("pc-cloudflare", "10.20.0.10", "198.51.100.10", "tcp", 443, 2, 22),
    // Phone → external moderate traffic (incl. IPv6).
    conv("phone-cloudflare6", "fd00:40::22", "2001:db8:1::10", "tcp", 443, 4, 30),
    conv("phone-google", "10.40.0.22", "203.0.113.21", "udp", 443, 1.5, 12),
    // Media server → external burst (offsite backup).
    conv("media-amazon", "10.10.0.30", "192.0.2.30", "tcp", 443, 260, 3, {
      up: { bps: 260 * M, shape: "burst", periodSeconds: 60, duty: 0.35, jitter: 0.2 },
    }),
    conv("laptop-media", "10.40.0.21", "10.10.0.30", "tcp", 32400, 1, 38),
    // IoT → small periodic external traffic.
    conv("thermostat-amazon", "10.30.0.40", "192.0.2.31", "tcp", 8883, 0.25, 0.12, {
      up: { bps: 0.25 * M, shape: "periodic", periodSeconds: 30, duty: 0.3, jitter: 0.3 },
      down: { bps: 0.12 * M, shape: "periodic", periodSeconds: 30, duty: 0.3, jitter: 0.3 },
      packetBytes: { up: 180, down: 140 },
    }),
    conv("camera-unknown-geo", "10.30.0.41", "198.51.100.200", "tcp", 443, 3.5, 0.3),
    conv("k8s-nas-nfs", "10.10.0.20", "10.10.0.10", "tcp", 2049, 70, 25),
    conv("k8s-microsoft", "10.10.0.20", "203.0.113.41", "tcp", 443, 2, 14),
    conv("laptop-microsoft", "10.40.0.21", "203.0.113.40", "tcp", 443, 4, 18),
    conv("laptop-cloudflare", "10.40.0.21", "198.51.100.11", "tcp", 443, 1, 7),
    conv("tablet-akamai", "10.40.0.23", "198.51.100.50", "tcp", 443, 1, 32),
    conv("tablet-netflix", "10.40.0.23", "203.0.113.80", "tcp", 443, 0.8, 18),
    // Internal endpoint with no device identity → fully unknown destination.
    conv("unresolved-unknown", "10.30.0.77", "192.0.2.250", "tcp", 8443, 1.2, 0.6),
  ];
}

function boost(conversations: Conversation[], id: string, upMbps: number, downMbps: number) {
  const c = conversations.find((x) => x.id === id);
  if (!c) throw new Error(`unknown conversation ${id}`);
  c.up = { ...c.up, bps: upMbps * M };
  c.down = { ...c.down, bps: downMbps * M };
}

function manyDestinations(seed: number): { conversations: Conversation[]; geo: Map<string, GeoRecord> } {
  const rng = keyedRng(seed, "many-destinations");
  const clients = ["10.20.0.10", "10.40.0.21", "10.40.0.22", "10.40.0.23", "10.10.0.20", "10.10.0.30"];
  const conversations: Conversation[] = [];
  const geo = new Map<string, GeoRecord>();
  for (let i = 0; i < 140; i++) {
    const ip =
      i < 100
        ? `${i % 2 === 0 ? "198.51.100" : "203.0.113"}.${100 + Math.floor(i / 2)}`
        : `2001:db8:ff::${(i - 99).toString(16)}`;
    const city = CITIES[Math.floor(rng() * CITIES.length)]!;
    const org = ORGS[Math.floor(rng() * ORGS.length)]!;
    geo.set(ip, {
      countryCode: city.countryCode,
      countryName: COUNTRY_NAMES[city.countryCode] ?? null,
      city: city.name,
      latitude: city.latitude,
      longitude: city.longitude,
      asn: org.asn,
      organization: org.organization,
    });
    const client = clients[Math.floor(rng() * clients.length)]!;
    const clientIp = ip.includes(":") ? "fd00:20::10" : client;
    // 0.3–20 Mbps download, skewed towards small flows (arithmetic only, D-039).
    const u = rng();
    const down = 0.3 + 19.7 * u * u * u;
    conversations.push(conv(`many-${i}`, clientIp, ip, rng() < 0.7 ? "tcp" : "udp", 443, down / 8, down));
  }
  return { conversations, geo };
}

export function buildScenario(name: ScenarioName, seed: number): Scenario {
  const conversations = baseConversations();
  const geo = baseGeoDb();
  const inventory = defaultInventory();
  let dataStopsAtTick: number | null = null;
  let description = "Balanced household traffic.";

  switch (name) {
    case "default":
      break;
    case "heavy-download":
      description = "pc-01 downloads a large game update from a single CDN.";
      conversations.push(conv("pc-valve", "10.20.0.10", "203.0.113.60", "tcp", 443, 6, 620));
      break;
    case "internal-backup":
      description = "Backups to nas-01 dominate the internal view.";
      boost(conversations, "pc-nas-smb", 880, 25);
      boost(conversations, "k8s-nas-nfs", 300, 40);
      conversations.push(conv("media-nas-backup", "10.10.0.30", "10.10.0.10", "tcp", 873, 410, 6));
      break;
    case "many-destinations": {
      description = "Many small destinations to exercise grouping and top-N.";
      const extra = manyDestinations(seed);
      conversations.push(...extra.conversations);
      for (const [ip, rec] of extra.geo) geo.set(ip, rec);
      break;
    }
    case "unknown-metadata": {
      description = "Missing geo, unresolved devices and partial topology.";
      const strip = (ip: string) => {
        const r = geo.get(ip);
        if (r) geo.set(ip, { ...r, latitude: null, longitude: null, city: null });
      };
      strip("203.0.113.41");
      strip("198.51.100.11");
      geo.delete("203.0.113.80");
      inventory.devices = inventory.devices.filter(
        (d) => d.id !== "dev_laptop01" && d.id !== "dev_tablet01",
      );
      inventory.topology = inventory.topology.filter(
        (l) => l.evidence !== "wlc" && l.b !== "dev_media",
      );
      break;
    }
    case "stale-collector":
      description = "Collector stops delivering data 20 s after start.";
      dataStopsAtTick = START_TICK + 20;
      break;
  }

  return { name, description, conversations, geo, inventory, dataStopsAtTick };
}
