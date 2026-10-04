import { describe, expect, it } from "vitest";
import type { HistoryTimelinePoint } from "@/contracts";
import {
  clampWindowEnd,
  replayStep,
  timelineGeometry,
  visibleRange,
  windowEndForClick,
  windowForDrag,
} from "./timeline";

const T = Date.parse("2026-10-05T12:00:00Z");
const s = (n: number) => n * 1000;

describe("visibleRange", () => {
  it("shows the last preset while the selection is inside it", () => {
    expect(visibleRange(T, 3600, null)).toEqual({ start: T - s(3600), end: T });
    expect(visibleRange(T, 3600, T - s(600))).toEqual({ start: T - s(3600), end: T });
  });
  it("centres an older selection without going past now", () => {
    expect(visibleRange(T, 3600, T - s(7200))).toEqual({ start: T - s(9000), end: T - s(5400) });
  });
});

describe("window selection", () => {
  it("keeps the window inside stored data, in whole seconds", () => {
    const earliest = T - s(3600);
    expect(clampWindowEnd(T + s(10), 300, earliest, T)).toBe(T);
    expect(clampWindowEnd(earliest + s(10), 300, earliest, T)).toBe(earliest + s(300));
    expect(clampWindowEnd(T - s(100) + 400, 300, earliest, T)).toBe(T - s(100));
    expect(clampWindowEnd(T, 300, null, null)).toBeNull();
    // span longer than the data: the window ends at the newest data
    expect(clampWindowEnd(T - s(50), 7200, earliest, T)).toBe(T);
  });
  it("centres a click and selects a dragged range", () => {
    expect(windowEndForClick(T, 300)).toBe(T + s(150));
    expect(windowForDrag(T, T - s(90.4))).toEqual({ atMs: T, spanS: 90 });
    expect(windowForDrag(T, T + 100).spanS).toBe(1);
  });
});

describe("replayStep", () => {
  it("advances by speed seconds per tick and accumulates fractions", () => {
    expect(replayStep(T, 2, 0, T + s(60))).toEqual({ atMs: T + s(2), carry: 0 });
    const a = replayStep(T, 0.5, 0, T + s(60))!;
    expect(a).toEqual({ atMs: T, carry: 0.5 });
    expect(replayStep(a.atMs, 0.5, a.carry, T + s(60))).toEqual({ atMs: T + s(1), carry: 0 });
  });
  it("stops at the newest stored data", () => {
    expect(replayStep(T, 4, 0, T + s(3))).toBeNull();
  });
});

const point = (
  sec: number,
  down: number | null,
  up: number | null,
  wan?: number,
): HistoryTimelinePoint => {
  const m = (v: number | null) =>
    v === null
      ? null
      : { value: v, unit: "bps" as const, measurement_kind: "sampled_estimate" as const };
  return {
    start: new Date(T + s(sec)).toISOString(),
    has_data: down !== null,
    inbound: m(down),
    outbound: m(up),
    internal: m(0),
    wan_download:
      wan === undefined ? null : { value: wan, unit: "bps", measurement_kind: "counter" },
    wan_upload:
      wan === undefined ? null : { value: wan / 2, unit: "bps", measurement_kind: "counter" },
  };
};

describe("timelineGeometry", () => {
  it("mirrors download/upload on one scale and breaks paths at gaps", () => {
    const pts = [point(0, 100, 50), point(10, null, null), point(20, 50, 100, 200)];
    const g = timelineGeometry(pts, 10, { start: T, end: T + s(30) }, 300, 41);
    expect(g.maxBps).toBe(200);
    // two separate download segments (gap in between)
    expect(g.inbound.match(/M/g)?.length).toBe(2);
    // midline 20.5, scale 19.5/200: download goes up, upload down
    expect(g.inbound).toContain("L0.0,10.8"); // 100 b/s → 20.5 − 9.75
    expect(g.outbound).toContain("L0.0,25.4"); // 50 b/s → 20.5 + 4.875
    // counter line only where counters exist; the maximum touches the top
    expect(g.wanDown).toBe("M200.0,1.0L300.0,1.0");
  });
  it("is empty without data", () => {
    const g = timelineGeometry([], 10, { start: T, end: T + s(30) }, 300, 40);
    expect(g).toEqual({ inbound: "", outbound: "", wanDown: "", wanUp: "", maxBps: 0 });
  });
});
