/**
 * Deterministic, stateless randomness keyed by (seed, ...parts).
 * Same inputs always produce the same output — mock scenarios are
 * reproducible per (seed, scenario, tick).
 *
 * Portability: mirrored by Go `internal/mock/rng.go`. Only integer ops and
 * correctly-rounded float ops (+ − × ÷ √) are used, except Math.exp in
 * poisson(), so the two implementations agree (D-039).
 */

/** FNV-1a 32-bit hash over a string. */
export function hashString(text: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

/** mulberry32 PRNG. Returns a generator of floats in [0, 1). */
export function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** Numbers in keys must be integers so the key text is identical in Go. */
export function keyedRng(seed: number, ...parts: Array<string | number>): () => number {
  return mulberry32(hashString(`${seed}|${parts.join("|")}`));
}

/** Uniform float in [0, 1) for a key. */
export function keyedUniform(seed: number, ...parts: Array<string | number>): number {
  return keyedRng(seed, ...parts)();
}

/** Approximately standard-normal (Irwin–Hall, 12 uniforms). Arithmetic only. */
export function gaussian(rng: () => number): number {
  let sum = 0;
  for (let i = 0; i < 12; i++) sum += rng();
  return sum - 6;
}

/** Poisson-distributed integer with mean `lambda`. */
export function poisson(rng: () => number, lambda: number): number {
  if (lambda <= 0) return 0;
  if (lambda < 40) {
    const limit = Math.exp(-lambda);
    let k = 0;
    let p = rng();
    while (p > limit) {
      k++;
      p *= rng();
    }
    return k;
  }
  return Math.max(0, Math.floor(lambda + Math.sqrt(lambda) * gaussian(rng) + 0.5));
}

/**
 * Smooth value noise in [-1, 1] over integer time, keyed by `key`.
 * Knots every `period` seconds, smoothstep-interpolated between them.
 */
export function smoothNoise(seed: number, key: string, t: number, period = 8): number {
  const x = t / period;
  const i = Math.floor(x);
  const f = x - i;
  const a = keyedUniform(seed, key, i) * 2 - 1;
  const b = keyedUniform(seed, key, i + 1) * 2 - 1;
  const w = f * f * (3 - 2 * f);
  return a * (1 - w) + b * w;
}
