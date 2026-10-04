import { describe, expect, it } from "vitest";
import { counterRate } from "./counters";

describe("counterRate", () => {
  it("computes bps = delta_octets * 8 / delta_seconds", () => {
    const r = counterRate({ at: 100, octets: 1_000, width: 64 }, { at: 110, octets: 126_000, width: 64 });
    expect(r).toEqual({ ok: true, bps: 100_000, intervalSeconds: 10 });
  });

  it("has no rate for the first sample", () => {
    expect(counterRate(null, { at: 1, octets: 5, width: 64 })).toEqual({ ok: false, reason: "first_sample" });
  });

  it("rejects non-positive intervals", () => {
    const r = counterRate({ at: 10, octets: 1, width: 64 }, { at: 10, octets: 2, width: 64 });
    expect(r).toEqual({ ok: false, reason: "non_positive_interval" });
  });

  it("handles a single 32-bit wrap", () => {
    const prev = { at: 0, octets: 2 ** 32 - 1_000, width: 32 as const };
    const cur = { at: 1, octets: 24_000, width: 32 as const };
    const r = counterRate(prev, cur);
    expect(r).toEqual({ ok: true, bps: 25_000 * 8, intervalSeconds: 1 });
  });

  it("treats a 64-bit decrease as a reset", () => {
    const r = counterRate({ at: 0, octets: 5e12, width: 64 }, { at: 20, octets: 1_000, width: 64 });
    expect(r).toEqual({ ok: false, reason: "counter_reset" });
  });

  it("treats an implausible wrap (faster than link speed) as a reset", () => {
    const prev = { at: 0, octets: 3_000_000_000, width: 32 as const };
    const cur = { at: 1, octets: 2_000_000_000, width: 32 as const };
    // Wrap would imply ~26 Gbps on a 1 Gbps link.
    expect(counterRate(prev, cur, { ifSpeedBps: 1e9 })).toEqual({ ok: false, reason: "counter_reset" });
  });

  it("ignores stale previous samples", () => {
    const r = counterRate(
      { at: 0, octets: 0, width: 64 },
      { at: 600, octets: 1_000, width: 64 },
      { maxIntervalSeconds: 120 },
    );
    expect(r).toEqual({ ok: false, reason: "stale_previous" });
  });
});
