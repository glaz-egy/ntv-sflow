/**
 * Bounded, log-like scales so one huge flow cannot dominate the scene
 * (GLOBE_VIEW.md §7, HOME_NETWORK_VIEW.md §16).
 */

const MBPS = 1_000_000;

/** Maps bps onto [0, 1] on a log scale between `floor` and `ceil`. */
export function logNorm(bps: number, floor = 100_000, ceil = 2_000 * MBPS): number {
  if (!(bps > floor)) return 0;
  if (bps >= ceil) return 1;
  return Math.log(bps / floor) / Math.log(ceil / floor);
}

/** Home edge stroke width in px, matching the documented bands. */
export function edgeWidth(bps: number): number {
  if (bps < 1 * MBPS) return 1;
  if (bps < 10 * MBPS) return 1.5 + logNorm(bps, MBPS, 10 * MBPS) * 1;
  if (bps < 100 * MBPS) return 2.5 + logNorm(bps, 10 * MBPS, 100 * MBPS) * 2;
  if (bps < 1000 * MBPS) return 4.5 + logNorm(bps, 100 * MBPS, 1000 * MBPS) * 3;
  return 8; // capped
}

/** Globe arc stroke (globe units). */
export function arcStroke(bps: number): number {
  return 0.15 + logNorm(bps) * 0.85;
}

/** Globe marker radius (degrees). */
export function markerRadius(bps: number): number {
  return 0.25 + logNorm(bps) * 0.9;
}

/** De-emphasise low traffic. */
export function trafficOpacity(bps: number): number {
  return 0.35 + logNorm(bps) * 0.65;
}
