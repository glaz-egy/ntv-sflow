import type { Measurement } from "@/contracts";

const UNITS = ["b/s", "kb/s", "Mb/s", "Gb/s", "Tb/s"];

/** 720000000 → "720 Mb/s". Two-to-three significant digits; never fake precision. */
export function formatBps(bps: number): string {
  if (!Number.isFinite(bps) || bps <= 0) return "0 b/s";
  let v = bps;
  let i = 0;
  while (v >= 1000 && i < UNITS.length - 1) {
    v /= 1000;
    i++;
  }
  const digits = v >= 100 ? 0 : v >= 10 ? 1 : 2;
  const fixed = v.toFixed(digits);
  const trimmed = fixed.includes(".") ? fixed.replace(/0+$/, "").replace(/\.$/, "") : fixed;
  return `${trimmed} ${UNITS[i]}`;
}

/** Sampled estimates are always prefixed with "≈" (D-005). */
export function formatMeasurement(m: Measurement | null | undefined): string {
  if (!m) return "—";
  const text = formatBps(m.value);
  return m.measurement_kind === "sampled_estimate" ? `≈${text}` : text;
}

export function measurementTitle(m: Measurement | null | undefined): string {
  if (!m) return "No data";
  if (m.measurement_kind === "counter") {
    return `Interface counter delta over ${m.interval_seconds ?? "?"} s`;
  }
  const samples = m.sample_count;
  return (
    `Sampled estimate over a ${m.window_seconds ?? "?"} s window` +
    (samples !== undefined ? ` from ${samples} packet sample${samples === 1 ? "" : "s"}` : "") +
    (samples !== undefined && samples < 10 ? " — low confidence" : "")
  );
}

export function sum(...ms: Array<Measurement | null | undefined>): number {
  return ms.reduce((s, m) => s + (m?.value ?? 0), 0);
}
