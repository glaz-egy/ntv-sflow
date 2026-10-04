/**
 * Pure helpers for the history timeline (D-059): which range to show, how a
 * click/drag/replay step maps to a historical window, and the chart paths.
 * Times are epoch milliseconds; spans are seconds.
 */
import type { HistoryTimelinePoint } from "@/contracts";

export const RANGE_PRESETS = [
  { seconds: 15 * 60, label: "15 min" },
  { seconds: 3600, label: "1 h" },
  { seconds: 6 * 3600, label: "6 h" },
  { seconds: 24 * 3600, label: "24 h" },
  { seconds: 7 * 24 * 3600, label: "7 d" },
] as const;

export const WINDOW_SPANS = [
  { seconds: 60, label: "1 min" },
  { seconds: 300, label: "5 min" },
  { seconds: 900, label: "15 min" },
  { seconds: 3600, label: "1 h" },
  { seconds: 6 * 3600, label: "6 h" },
] as const;

export const REPLAY_SPEEDS = [0.5, 1, 2, 4] as const;

const SECOND = 1000;

/**
 * The visible range: the last `preset` seconds, unless the selected window
 * lies before it — then the range is centred on the selection (never past now).
 */
export function visibleRange(
  nowMs: number,
  presetS: number,
  atMs: number | null,
): { start: number; end: number } {
  const len = presetS * SECOND;
  if (atMs === null || atMs >= nowMs - len) return { start: nowMs - len, end: nowMs };
  const end = Math.min(nowMs, atMs + len / 2);
  return { start: end - len, end };
}

/**
 * Clamps a window end so that [at − span, at) stays inside stored data
 * (whole seconds). Returns null when nothing is stored.
 */
export function clampWindowEnd(
  atMs: number,
  spanS: number,
  earliestMs: number | null,
  latestMs: number | null,
): number | null {
  if (earliestMs === null || latestMs === null) return null;
  const lo = Math.min(latestMs, earliestMs + spanS * SECOND);
  const v = Math.max(lo, Math.min(latestMs, atMs));
  return Math.floor(v / SECOND) * SECOND;
}

/** A click selects the window centred on the clicked time. */
export function windowEndForClick(tMs: number, spanS: number): number {
  return tMs + (spanS * SECOND) / 2;
}

/** A drag from a to b selects exactly that range (at least 1 s). */
export function windowForDrag(aMs: number, bMs: number): { atMs: number; spanS: number } {
  const lo = Math.min(aMs, bMs);
  const hi = Math.max(aMs, bMs);
  return {
    atMs: Math.round(hi / SECOND) * SECOND,
    spanS: Math.max(1, Math.round((hi - lo) / SECOND)),
  };
}

/**
 * One replay tick (called once per wall-clock second): the window moves by
 * `speed` seconds. Fractional speeds accumulate in `carry` (seconds).
 * Returns null when the replay reaches the newest stored data.
 */
export function replayStep(
  atMs: number,
  speed: number,
  carry: number,
  latestMs: number,
): { atMs: number; carry: number } | null {
  const total = carry + speed;
  const whole = Math.floor(total);
  const next = atMs + whole * SECOND;
  if (next >= latestMs) return null;
  return { atMs: next, carry: total - whole };
}

export interface TimelineGeometry {
  /** Download (external → internal) area above the midline. */
  inbound: string;
  /** Upload (internal → external) area below the midline. */
  outbound: string;
  /** Boundary-counter lines (exact-ish), when present. */
  wanDown: string;
  wanUp: string;
  maxBps: number;
}

/**
 * Mirrored areas: download up, upload down, sharing one linear scale.
 * Buckets without data break the paths (gaps stay visible, never interpolated).
 */
export function timelineGeometry(
  points: HistoryTimelinePoint[],
  stepS: number,
  range: { start: number; end: number },
  width: number,
  height: number,
): TimelineGeometry {
  const mid = height / 2;
  const span = Math.max(1, range.end - range.start);
  const x = (ms: number) => ((ms - range.start) / span) * width;
  let maxBps = 0;
  for (const p of points) {
    maxBps = Math.max(
      maxBps,
      p.inbound?.value ?? 0,
      p.outbound?.value ?? 0,
      p.wan_download?.value ?? 0,
      p.wan_upload?.value ?? 0,
    );
  }
  const scale = maxBps > 0 ? (mid - 1) / maxBps : 0;

  const area = (value: (p: HistoryTimelinePoint) => number | null, dir: 1 | -1) => {
    let d = "";
    let run: Array<[number, number]> = [];
    const flush = () => {
      if (run.length > 0) {
        d += `M${run[0][0].toFixed(1)},${mid}`;
        for (const [px, py] of run) d += `L${px.toFixed(1)},${py.toFixed(1)}`;
        d += `L${run[run.length - 1][0].toFixed(1)},${mid}Z`;
      }
      run = [];
    };
    for (const p of points) {
      const v = value(p);
      if (v === null) {
        flush();
        continue;
      }
      const t = Date.parse(p.start);
      const y = mid - dir * v * scale;
      run.push([x(t), y], [x(t + stepS * SECOND), y]);
    }
    flush();
    return d;
  };
  const line = (value: (p: HistoryTimelinePoint) => number | null, dir: 1 | -1) => {
    let d = "";
    let open = false;
    for (const p of points) {
      const v = value(p);
      if (v === null) {
        open = false;
        continue;
      }
      const t = Date.parse(p.start);
      const y = (mid - dir * v * scale).toFixed(1);
      d += `${open ? "L" : "M"}${x(t).toFixed(1)},${y}L${x(t + stepS * SECOND).toFixed(1)},${y}`;
      open = true;
    }
    return d;
  };
  return {
    inbound: area((p) => (p.has_data ? (p.inbound?.value ?? 0) : null), 1),
    outbound: area((p) => (p.has_data ? (p.outbound?.value ?? 0) : null), -1),
    wanDown: line((p) => p.wan_download?.value ?? null, 1),
    wanUp: line((p) => p.wan_upload?.value ?? null, -1),
    maxBps,
  };
}
