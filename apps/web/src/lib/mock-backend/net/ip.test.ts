import { describe, expect, it } from "vitest";
import { CidrClassifier, cidrContains, parseCidr, parseIp } from "./ip";

describe("parseIp", () => {
  it("parses IPv4", () => {
    expect(parseIp("10.20.0.10")).toEqual({ family: "ipv4", value: 0x0a14000an });
    expect(parseIp("255.255.255.255")?.value).toBe(0xffffffffn);
  });

  it("rejects malformed IPv4", () => {
    for (const bad of ["10.0.0", "10.0.0.256", "10.0.0.01", "a.b.c.d", "1.2.3.4.5", ""]) {
      expect(parseIp(bad), bad).toBeNull();
    }
  });

  it("parses IPv6 forms consistently", () => {
    const full = parseIp("2001:0db8:0000:0000:0000:0000:0000:0001");
    expect(full?.family).toBe("ipv6");
    expect(parseIp("2001:db8::1")).toEqual(full);
    expect(parseIp("::1")?.value).toBe(1n);
    expect(parseIp("::")?.value).toBe(0n);
    expect(parseIp("fe80::1%eth0")).toEqual(parseIp("fe80::1"));
  });

  it("parses IPv4-mapped IPv6", () => {
    expect(parseIp("::ffff:192.0.2.1")?.value).toBe(0xffffc0000201n);
  });

  it("rejects malformed IPv6", () => {
    for (const bad of ["2001:db8:::1", "1::2::3", "12345::", "1:2:3:4:5:6:7:8:9", "g::1"]) {
      expect(parseIp(bad), bad).toBeNull();
    }
  });
});

describe("CIDR", () => {
  it("normalises the network address", () => {
    expect(parseCidr("10.20.0.10/16")).toEqual({ family: "ipv4", network: 0x0a140000n, prefix: 16 });
  });

  it("matches IPv4 and IPv6 without cross-family matches", () => {
    const v4 = parseCidr("10.0.0.0/8")!;
    const v6 = parseCidr("fc00::/7")!;
    expect(cidrContains(v4, parseIp("10.255.1.1")!)).toBe(true);
    expect(cidrContains(v4, parseIp("11.0.0.1")!)).toBe(false);
    expect(cidrContains(v6, parseIp("fd00:20::10")!)).toBe(true);
    expect(cidrContains(v6, parseIp("2001:db8::1")!)).toBe(false);
    expect(cidrContains(v4, parseIp("::ffff:10.0.0.1")!)).toBe(false);
  });

  it("handles /0 and host prefixes", () => {
    expect(cidrContains(parseCidr("0.0.0.0/0")!, parseIp("203.0.113.9")!)).toBe(true);
    expect(cidrContains(parseCidr("203.0.113.9/32")!, parseIp("203.0.113.9")!)).toBe(true);
    expect(cidrContains(parseCidr("203.0.113.9/32")!, parseIp("203.0.113.10")!)).toBe(false);
  });

  it("rejects invalid prefixes", () => {
    expect(parseCidr("10.0.0.0/33")).toBeNull();
    expect(parseCidr("2001:db8::/129")).toBeNull();
    expect(parseCidr("10.0.0.0/x")).toBeNull();
  });
});

describe("CidrClassifier", () => {
  it("uses configured CIDRs, including owned public prefixes", () => {
    const c = new CidrClassifier(["192.168.0.0/16", "203.0.113.0/24", "2001:db8:42::/48"]);
    expect(c.isInternal("192.168.1.10")).toBe(true);
    expect(c.isInternal("203.0.113.77")).toBe(true); // operator-owned public prefix
    expect(c.isInternal("10.0.0.1")).toBe(false); // RFC1918 but not configured
    expect(c.isInternal("2001:db8:42::5")).toBe(true);
    expect(c.isInternal("2001:db8:43::5")).toBe(false);
  });

  it("reports unparseable addresses as unknown, not external", () => {
    expect(new CidrClassifier(["10.0.0.0/8"]).isInternal("not-an-ip")).toBeNull();
  });

  it("refuses invalid configuration", () => {
    expect(() => new CidrClassifier(["10.0.0.0/40"])).toThrow();
  });
});
