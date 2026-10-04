/** Traffic semantics colours — keep in sync with globals.css @theme. */
export const COLORS = {
  outbound: "#f59e0b",
  inbound: "#22d3ee",
  internal: "#a78bfa",
  origin: "#34d399",
  stale: "#f87171",
  land: "rgba(148, 163, 184, 0.32)",
  globe: "#0b1426",
  selected: "#ffffff",
} as const;

/** "#rrggbb" + alpha → "rgba(...)" */
export function withAlpha(hex: string, alpha: number): string {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${Math.max(0, Math.min(1, alpha))})`;
}
