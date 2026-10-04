/**
 * Web Mercator math for the flat Globe View projection (D-062).
 *
 * World coordinates are unitless: the full 360° of longitude spans x ∈ [-0.5, 0.5)
 * and the map is square, so y spans the same range at ±MAX_LATITUDE. y grows
 * southwards to match screen coordinates. Longitudes are taken relative to a
 * central meridian (the origin's), so every destination lies within half a
 * world of the origin and arcs never cross the map edge.
 */

/** Latitude at which Web Mercator becomes square. */
export const MAX_LATITUDE = 85.05112878;

/** Wraps a longitude difference into [-180, 180). */
export function wrapLongitude(lng: number): number {
  return ((((lng + 180) % 360) + 360) % 360) - 180;
}

/** Longitude relative to `centralMeridian` → x ∈ [-0.5, 0.5). Not wrapped: polygons stay contiguous. */
export function mercatorX(lng: number, centralMeridian: number): number {
  return (lng - centralMeridian) / 360;
}

/** Latitude → y ∈ [-0.5, 0.5], clamped at ±MAX_LATITUDE. North is negative. */
export function mercatorY(lat: number): number {
  const phi = (Math.max(-MAX_LATITUDE, Math.min(MAX_LATITUDE, lat)) * Math.PI) / 180;
  return -Math.log(Math.tan(Math.PI / 4 + phi / 2)) / (2 * Math.PI);
}

/** A point projected relative to the central meridian, wrapped to the nearest copy. */
export function projectPoint(lat: number, lng: number, centralMeridian: number): [number, number] {
  return [wrapLongitude(lng - centralMeridian) / 360, mercatorY(lat)];
}

/** Inverse of mercatorY. */
export function latitudeOf(y: number): number {
  return (Math.atan(Math.sinh(-y * 2 * Math.PI)) * 180) / Math.PI;
}

/** The world copies (integer x offsets) that overlap the screen range [0, width). */
export function visibleCopies(tx: number, scale: number, width: number): number[] {
  // Copy k spans [tx + (k − ½)·scale, tx + (k + ½)·scale); touching an edge is not overlap.
  const first = Math.floor(-tx / scale - 0.5) + 1;
  const last = Math.ceil((width - tx) / scale + 0.5) - 1;
  const out: number[] = [];
  for (let k = first; k <= last; k++) out.push(k);
  return out;
}
