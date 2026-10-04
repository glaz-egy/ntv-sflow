/**
 * IPv4/IPv6 parsing and CIDR matching using BigInt.
 * Reference implementation of internal/external classification (D-008):
 * internal networks are configured CIDRs, never hard-coded RFC1918.
 */

export type Family = "ipv4" | "ipv6";

export interface ParsedIp {
  family: Family;
  value: bigint;
}

export interface Cidr {
  family: Family;
  network: bigint;
  prefix: number;
}

const IPV4_BITS = 32;
const IPV6_BITS = 128;

function parseIpv4(text: string): bigint | null {
  const parts = text.split(".");
  if (parts.length !== 4) return null;
  let value = 0n;
  for (const part of parts) {
    if (!/^\d{1,3}$/.test(part)) return null;
    if (part.length > 1 && part.startsWith("0")) return null;
    const n = Number(part);
    if (n > 255) return null;
    value = (value << 8n) | BigInt(n);
  }
  return value;
}

function parseIpv6(text: string): bigint | null {
  let input = text;
  const zone = input.indexOf("%");
  if (zone >= 0) input = input.slice(0, zone);
  if (input.length === 0) return null;

  // Embedded IPv4 tail (e.g. ::ffff:192.0.2.1) → rewrite as two hex groups.
  const lastColon = input.lastIndexOf(":");
  if (input.includes(".", lastColon)) {
    const v4 = parseIpv4(input.slice(lastColon + 1));
    if (v4 === null) return null;
    const hi = (v4 >> 16n).toString(16);
    const lo = (v4 & 0xffffn).toString(16);
    input = `${input.slice(0, lastColon + 1)}${hi}:${lo}`;
  }

  const doubleColon = input.indexOf("::");
  if (doubleColon !== input.lastIndexOf("::")) return null;

  const parseGroups = (s: string): number[] | null => {
    if (s === "") return [];
    const groups = s.split(":");
    const out: number[] = [];
    for (const g of groups) {
      if (!/^[0-9a-fA-F]{1,4}$/.test(g)) return null;
      out.push(parseInt(g, 16));
    }
    return out;
  };

  let groups: number[];
  const totalGroups = 8;
  if (doubleColon >= 0) {
    const head = parseGroups(input.slice(0, doubleColon));
    const tail = parseGroups(input.slice(doubleColon + 2));
    if (!head || !tail) return null;
    const missing = totalGroups - head.length - tail.length;
    if (missing < 1) return null;
    groups = [...head, ...new Array<number>(missing).fill(0), ...tail];
  } else {
    const all = parseGroups(input);
    if (!all || all.length !== totalGroups) return null;
    groups = all;
  }
  if (groups.length !== 8) return null;

  let value = 0n;
  for (const g of groups) value = (value << 16n) | BigInt(g);
  return value;
}

export function parseIp(text: string): ParsedIp | null {
  const trimmed = text.trim();
  if (trimmed.includes(":")) {
    const v = parseIpv6(trimmed);
    return v === null ? null : { family: "ipv6", value: v };
  }
  const v = parseIpv4(trimmed);
  return v === null ? null : { family: "ipv4", value: v };
}

function bits(family: Family): number {
  return family === "ipv4" ? IPV4_BITS : IPV6_BITS;
}

function mask(family: Family, prefix: number): bigint {
  const total = bits(family);
  if (prefix === 0) return 0n;
  const all = (1n << BigInt(total)) - 1n;
  return (all >> BigInt(total - prefix)) << BigInt(total - prefix);
}

export function parseCidr(text: string): Cidr | null {
  const [addr, prefixText, ...rest] = text.trim().split("/");
  if (rest.length > 0 || addr === undefined) return null;
  const ip = parseIp(addr);
  if (!ip) return null;
  const max = bits(ip.family);
  let prefix = max;
  if (prefixText !== undefined) {
    if (!/^\d{1,3}$/.test(prefixText)) return null;
    prefix = Number(prefixText);
    if (prefix > max) return null;
  }
  return { family: ip.family, network: ip.value & mask(ip.family, prefix), prefix };
}

export function cidrContains(cidr: Cidr, ip: ParsedIp): boolean {
  if (cidr.family !== ip.family) return false;
  return (ip.value & mask(cidr.family, cidr.prefix)) === cidr.network;
}

/**
 * Classifies addresses as internal/external from configured CIDRs.
 * Unparseable addresses are reported as `null` (unknown), never guessed.
 */
export class CidrClassifier {
  private readonly cidrs: Cidr[];

  constructor(cidrTexts: string[]) {
    this.cidrs = cidrTexts.map((t) => {
      const c = parseCidr(t);
      if (!c) throw new Error(`invalid CIDR in internal_cidrs: ${t}`);
      return c;
    });
  }

  isInternal(address: string): boolean | null {
    const ip = parseIp(address);
    if (!ip) return null;
    return this.cidrs.some((c) => cidrContains(c, ip));
  }
}
