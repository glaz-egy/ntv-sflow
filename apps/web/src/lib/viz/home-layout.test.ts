import { describe, expect, it } from "vitest";
import { MockBackend } from "@/lib/mock-backend/server";
import { START_TICK } from "@/lib/mock-backend/scenarios";
import { computeHomeLayout } from "./home-layout";

function backend(tick: number) {
  const b = new MockBackend({ seed: 42, scenario: "default", speed: 1, epochMs: 0 });
  b.setTick(tick);
  return b;
}

describe("computeHomeLayout", () => {
  it("does not move nodes when only traffic values change", () => {
    const a = computeHomeLayout(backend(START_TICK).getTopology().nodes);
    const b = computeHomeLayout(backend(START_TICK + 17).getTopology().nodes);
    expect([...b.entries()]).toEqual([...a.entries()]);
  });

  it("is independent of input order", () => {
    const nodes = backend(START_TICK).getTopology().nodes;
    expect(computeHomeLayout([...nodes].reverse())).toEqual(computeHomeLayout(nodes));
  });

  it("places the Internet above infrastructure above endpoints", () => {
    const layout = computeHomeLayout(backend(START_TICK).getTopology().nodes);
    expect(layout.get("internet")!.y).toBeLessThan(layout.get("dev_router")!.y);
    expect(layout.get("dev_router")!.y).toBeLessThan(layout.get("dev_core_sw")!.y);
    expect(layout.get("dev_core_sw")!.y).toBeLessThan(layout.get("dev_pc01")!.y);
  });

  it("does not move existing nodes when transient nodes appear", () => {
    const base = backend(START_TICK).getTopology().nodes;
    const before = computeHomeLayout(base);
    const extra = backend(START_TICK).getHomeTraffic({ destination_key: "asn:13335" }).nodes;
    const after = computeHomeLayout([...base, ...extra]);
    expect(extra.some((n) => n.kind === "unresolved_endpoint")).toBe(true);
    expect(extra.some((n) => n.kind === "external_destination")).toBe(true);
    for (const [id, p] of before) expect(after.get(id), id).toEqual(p);
  });

  it("gives every node a unique position", () => {
    const b = backend(START_TICK);
    const layout = computeHomeLayout([
      ...b.getTopology().nodes,
      ...b.getHomeTraffic({ include_inactive: true, destination_key: "asn:13335" }).nodes,
    ]);
    const keys = [...layout.values()].map((p) => `${p.x},${p.y}`);
    expect(new Set(keys).size).toBe(keys.length);
  });
});
