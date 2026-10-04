import { describe, expect, it } from "vitest";
import { edgeWidth, logNorm } from "../viz/scales";
import { formatBps, formatMeasurement, measurementTitle } from "./measurement";

describe("formatBps", () => {
  it("formats with bounded precision", () => {
    expect(formatBps(720_000_000)).toBe("720 Mb/s");
    expect(formatBps(1_234_567)).toBe("1.23 Mb/s");
    expect(formatBps(12_345_678)).toBe("12.3 Mb/s");
    expect(formatBps(2_000_000_000)).toBe("2 Gb/s");
    expect(formatBps(0)).toBe("0 b/s");
  });
});

describe("formatMeasurement", () => {
  it("marks sampled estimates as approximate but not counters", () => {
    expect(
      formatMeasurement({ value: 1e8, unit: "bps", measurement_kind: "sampled_estimate", window_seconds: 5 }),
    ).toBe("≈100 Mb/s");
    expect(formatMeasurement({ value: 1e8, unit: "bps", measurement_kind: "counter", interval_seconds: 5 })).toBe(
      "100 Mb/s",
    );
    expect(formatMeasurement(null)).toBe("—");
  });

  it("flags low-sample estimates", () => {
    expect(
      measurementTitle({ value: 1, unit: "bps", measurement_kind: "sampled_estimate", window_seconds: 5, sample_count: 2 }),
    ).toContain("low confidence");
  });
});

describe("scales", () => {
  it("is monotonic and bounded", () => {
    expect(logNorm(0)).toBe(0);
    expect(logNorm(1e12)).toBe(1);
    expect(logNorm(1e7)).toBeLessThan(logNorm(1e8));
    expect(edgeWidth(5e9)).toBe(8);
    expect(edgeWidth(5e5)).toBe(1);
    expect(edgeWidth(5e6)).toBeLessThan(edgeWidth(5e7));
  });
});
