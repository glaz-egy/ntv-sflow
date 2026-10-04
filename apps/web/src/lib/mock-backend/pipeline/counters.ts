/**
 * Interface counter → rate math (docs/SFLOW.md §7).
 *   bps = delta_octets * 8 / delta_seconds
 * Reference for the Go `internal/counters` package.
 */

export interface CounterReading {
  /** Seconds (any epoch, consistent per interface). */
  at: number;
  octets: number;
  /** 32 for ifInOctets/ifOutOctets (wrap at 2^32); 64 for HC counters. */
  width: 32 | 64;
}

export type CounterRate =
  | { ok: true; bps: number; intervalSeconds: number }
  | {
      ok: false;
      reason: "first_sample" | "non_positive_interval" | "stale_previous" | "counter_reset";
    };

const TWO_POW_32 = 2 ** 32;

export interface CounterRateOptions {
  /** Previous readings older than this are not used for a rate. */
  maxIntervalSeconds?: number;
  /** Link speed; deltas implying more than this (×1.1) are treated as resets. */
  ifSpeedBps?: number | null;
}

export function counterRate(
  previous: CounterReading | null,
  current: CounterReading,
  options: CounterRateOptions = {},
): CounterRate {
  if (!previous) return { ok: false, reason: "first_sample" };
  const interval = current.at - previous.at;
  if (interval <= 0) return { ok: false, reason: "non_positive_interval" };
  if (options.maxIntervalSeconds !== undefined && interval > options.maxIntervalSeconds) {
    return { ok: false, reason: "stale_previous" };
  }

  let delta = current.octets - previous.octets;
  if (delta < 0) {
    // A 32-bit counter may legitimately wrap once. 64-bit counters do not wrap
    // in practice, so a decrease means the counter (or device) was reset.
    if (current.width === 32 && previous.octets < TWO_POW_32) {
      delta += TWO_POW_32;
    } else {
      return { ok: false, reason: "counter_reset" };
    }
  }

  const bps = (delta * 8) / interval;
  if (options.ifSpeedBps && bps > options.ifSpeedBps * 1.1) {
    // Implausible (e.g. a reset that happened to look like a wrap).
    return { ok: false, reason: "counter_reset" };
  }
  return { ok: true, bps, intervalSeconds: interval };
}
