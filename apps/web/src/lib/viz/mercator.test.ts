import { describe, expect, it } from "vitest";
import { latitudeOf, MAX_LATITUDE, mercatorX, mercatorY, projectPoint, visibleCopies, wrapLongitude } from "./mercator";

describe("mercator", () => {
  it("wraps longitudes into [-180, 180)", () => {
    expect(wrapLongitude(0)).toBe(0);
    expect(wrapLongitude(180)).toBe(-180);
    expect(wrapLongitude(-180)).toBe(-180);
    expect(wrapLongitude(190)).toBe(-170);
    expect(wrapLongitude(-190)).toBe(170);
    expect(wrapLongitude(720 + 45)).toBe(45);
  });

  it("maps the equator to 0 and the square-map latitude to ±0.5", () => {
    expect(mercatorY(0)).toBeCloseTo(0, 12);
    expect(mercatorY(MAX_LATITUDE)).toBeCloseTo(-0.5, 6);
    expect(mercatorY(-MAX_LATITUDE)).toBeCloseTo(0.5, 6);
    // Poles are clamped rather than infinite.
    expect(mercatorY(90)).toBeCloseTo(-0.5, 6);
    expect(mercatorY(-90)).toBeCloseTo(0.5, 6);
  });

  it("inverts y back to latitude", () => {
    for (const lat of [-80, -35.7, 0, 35.68, 60]) expect(latitudeOf(mercatorY(lat))).toBeCloseTo(lat, 9);
  });

  it("places points relative to the central meridian on the nearest copy", () => {
    // Tokyo-centred map: San Francisco is east across the Pacific, not west across Eurasia.
    const [x] = projectPoint(37.77, -122.42, 139.69);
    expect(x).toBeCloseTo((360 - 122.42 - 139.69) / 360, 9);
    expect(x).toBeGreaterThan(0);
    expect(projectPoint(10, 139.69, 139.69)[0]).toBe(0);
  });

  it("keeps polygon x unwrapped so shapes stay contiguous", () => {
    expect(mercatorX(180, 140)).toBeCloseTo(40 / 360, 12);
    expect(mercatorX(-180, 140)).toBeCloseTo(-320 / 360, 12);
  });

  it("lists the world copies that cover the screen", () => {
    // World centred in a viewport exactly one world wide.
    expect(visibleCopies(500, 1000, 1000)).toEqual([0]);
    // Panned right by a quarter world: the left gap is filled by copy -1.
    expect(visibleCopies(750, 1000, 1000)).toEqual([-1, 0]);
    // Zoomed out: the viewport is wider than one world.
    expect(visibleCopies(1000, 500, 2000)).toEqual([-2, -1, 0, 1, 2]);
  });
});
