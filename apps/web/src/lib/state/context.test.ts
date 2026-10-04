import { describe, expect, it } from "vitest";
import {
  DEFAULT_CONTEXT,
  globeToHome,
  homeToGlobe,
  hrefFor,
  parseContext,
  serializeContext,
  withGrouping,
  type InvestigationContext,
} from "./context";

const parse = (qs: string) => parseContext(new URLSearchParams(qs));

describe("parseContext / serializeContext", () => {
  it("returns defaults for an empty query", () => {
    expect(parse("")).toEqual(DEFAULT_CONTEXT);
    expect(serializeContext(DEFAULT_CONTEXT).toString()).toBe("");
  });

  it("round-trips a full context", () => {
    const ctx: InvestigationContext = {
      src: "dev_pc01",
      dst: "ip:2001:db8:1::10",
      proto: "tcp",
      min: 1_000_000,
      top: 25,
      grouping: "ip",
      dir: "inbound",
      mode: "hybrid",
      scope: "external",
      sel: "e:dev_nas01~dev_pc01",
      vlan: 20,
      types: ["pc", "server"],
      inactive: true,
    };
    expect(parseContext(serializeContext(ctx))).toEqual(ctx);
  });

  it("falls back to defaults for invalid values instead of throwing", () => {
    const ctx = parse("g=planet&dir=up&min=-5&top=abc&proto=sctp&mode=x&vlan=1.5&types=pc,toaster");
    expect(ctx.grouping).toBe("asn");
    expect(ctx.dir).toBe("both");
    expect(ctx.min).toBe(0);
    expect(ctx.top).toBeNull();
    expect(ctx.proto).toBeNull();
    expect(ctx.mode).toBe("traffic");
    expect(ctx.vlan).toBeNull();
    expect(ctx.types).toEqual(["pc"]);
  });

  it("drops destination keys with an unknown grouping", () => {
    expect(parse("dst=planet:earth").dst).toBeNull();
  });

  it("derives grouping from the selected destination", () => {
    expect(parse("g=asn&dst=country:JP").grouping).toBe("country");
  });
});

describe("withGrouping", () => {
  it("clears a destination of a different grouping", () => {
    const ctx = { ...DEFAULT_CONTEXT, dst: "asn:13335" };
    expect(withGrouping(ctx, "country").dst).toBeNull();
    expect(withGrouping(ctx, "asn").dst).toBe("asn:13335");
  });
});

describe("cross-view transitions", () => {
  const globe: InvestigationContext = {
    ...DEFAULT_CONTEXT,
    src: "dev_phone01",
    dst: "asn:13335",
    proto: "tcp",
    min: 500_000,
    top: 50,
    dir: "inbound",
  };

  it("Globe → Home carries destination, source, protocol and thresholds", () => {
    const home = globeToHome(globe);
    expect(home.dst).toBe("asn:13335");
    expect(home.src).toBe("dev_phone01");
    expect(home.proto).toBe("tcp");
    expect(home.min).toBe(500_000);
    expect(home.top).toBe(50);
  });

  it("Open in Home Network sets the chosen destination", () => {
    expect(globeToHome({ ...globe, dst: null }, "country:US").dst).toBe("country:US");
  });

  it("Home → Globe with a device sets the source filter and drops a stale destination", () => {
    const g = homeToGlobe(globe, "dev_pc01");
    expect(g.src).toBe("dev_pc01");
    expect(g.dst).toBeNull();
    expect(g.proto).toBe("tcp");
  });

  it("Home → Globe for the same source keeps the destination", () => {
    expect(homeToGlobe(globe, "dev_phone01").dst).toBe("asn:13335");
    expect(homeToGlobe(globe).dst).toBe("asn:13335");
  });

  it("round trip Globe → Home → Globe preserves context", () => {
    const back = parseContext(new URLSearchParams(hrefFor("globe", homeToGlobe(globeToHome(globe))).split("?")[1]));
    expect(back).toEqual(globe);
  });

  it("builds shareable hrefs", () => {
    expect(hrefFor("home", globeToHome(DEFAULT_CONTEXT, "asn:15169"))).toBe("/home?dst=asn%3A15169");
    expect(hrefFor("globe", DEFAULT_CONTEXT)).toBe("/globe");
  });
});
