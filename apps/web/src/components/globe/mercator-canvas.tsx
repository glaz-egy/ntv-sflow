"use client";

import { useEffect, useRef } from "react";
import { feature } from "topojson-client";
import type { Topology } from "topojson-specification";
import countriesTopo from "world-atlas/countries-110m.json";
import { COLORS, withAlpha } from "@/lib/viz/colors";
import { mercatorX, mercatorY, projectPoint, visibleCopies } from "@/lib/viz/mercator";
import { arcStroke, markerRadius, trafficOpacity } from "@/lib/viz/scales";
import type { GlobeCanvasProps } from "./globe-canvas";
import { arcLabel, destinationLabel, originLabel } from "./labels";

/**
 * Flat Web Mercator alternative to the 3D globe (D-062). Same props and visual
 * encoding as GlobeCanvas; drawn with Canvas 2D. The map is centred on the
 * origin's meridian and wraps horizontally, so every arc takes the short way.
 */

const MAX_ZOOM = 32;
const DASH_PERIOD_MS = 3000;
const RING_PERIOD_MS = 1400;

interface Marker {
  key: string;
  x: number;
  y: number;
  radius: number;
  color: string;
  selected: boolean;
  isOrigin: boolean;
  label: string;
}

interface Arc {
  key: string;
  x0: number;
  y0: number;
  x1: number;
  y1: number;
  /** Bulge as a fraction of the chord; inbound/outbound differ so they stay distinguishable. */
  bend: number;
  width: number;
  color: string;
  alpha: number;
  label: string;
}

interface Scene {
  markers: Marker[];
  arcs: Arc[];
  ring: [number, number] | null;
  animate: boolean;
}

interface View {
  w: number;
  h: number;
  dpr: number;
  /** Pixels per world width. */
  s: number;
  /** Screen position of the world origin (the origin's meridian, the equator). */
  tx: number;
  ty: number;
  tween: {
    fromTx: number;
    fromTy: number;
    toTx: number;
    toTy: number;
    t0: number;
    ms: number;
  } | null;
}

/** Wraps tx to the copy nearest the centre (except mid-tween) and keeps the poles in view. */
function clamp(v: View) {
  if (!v.tween) v.tx = v.w / 2 + ((((v.tx - v.w / 2 + v.s / 2) % v.s) + v.s) % v.s) - v.s / 2;
  v.ty = v.s >= v.h ? Math.min(v.s / 2, Math.max(v.h - v.s / 2, v.ty)) : v.h / 2;
}

type Feature = { geometry: { type: string; coordinates: number[][][] | number[][][][] } };

function landPath(meridian: number): Path2D {
  const topo = countriesTopo as unknown as Topology;
  const { features } = feature(topo, topo.objects.countries!) as unknown as { features: Feature[] };
  const path = new Path2D();
  for (const f of features) {
    const polys = (f.geometry.type === "Polygon" ? [f.geometry.coordinates] : f.geometry.coordinates) as number[][][][];
    for (const rings of polys)
      for (const ring of rings) {
        // Unwrap so rings crossing the antimeridian (Russia, Fiji, Antarctica)
        // stay contiguous instead of spanning the whole map; copies cover the overflow.
        let prev: number | null = null;
        ring.forEach(([rawLng, lat], i) => {
          let lng = rawLng!;
          if (prev !== null) lng += 360 * Math.round((prev - lng) / 360);
          prev = lng;
          const x = mercatorX(lng, meridian);
          const y = mercatorY(lat!);
          if (i === 0) path.moveTo(x, y);
          else path.lineTo(x, y);
        });
        path.closePath();
      }
  }
  return path;
}

function graticulePath(meridian: number): Path2D {
  const path = new Path2D();
  for (let lng = -180; lng < 180; lng += 30) {
    const x = mercatorX(lng, meridian);
    // Shift each meridian into this copy's [-0.5, 0.5) range.
    const xs = x - Math.floor(x + 0.5);
    path.moveTo(xs, -0.5);
    path.lineTo(xs, 0.5);
  }
  for (let lat = -60; lat <= 60; lat += 30) {
    const y = mercatorY(lat);
    path.moveTo(-0.5, y);
    path.lineTo(0.5, y);
  }
  return path;
}

/** Quadratic curve bowed towards the top of the map. */
function arcGeometry(a: Arc, ox: number, s: number, tx: number, ty: number) {
  const x0 = tx + (a.x0 + ox) * s;
  const y0 = ty + a.y0 * s;
  const x1 = tx + (a.x1 + ox) * s;
  const y1 = ty + a.y1 * s;
  const dx = x1 - x0;
  const dy = y1 - y0;
  const len = Math.hypot(dx, dy) || 1;
  let nx = -dy / len;
  let ny = dx / len;
  if (ny > 0) {
    nx = -nx;
    ny = -ny;
  }
  const cx = (x0 + x1) / 2 + nx * len * a.bend;
  const cy = (y0 + y1) / 2 + ny * len * a.bend;
  const path = new Path2D();
  path.moveTo(x0, y0);
  path.quadraticCurveTo(cx, cy, x1, y1);
  // Arc length of a shallow quadratic ≈ (2·control polygon + chord) / 3.
  const length = (2 * (Math.hypot(cx - x0, cy - y0) + Math.hypot(x1 - cx, y1 - cy)) + len) / 3;
  return { path, x0, y0, x1, y1, length };
}

export default function MercatorCanvas(props: GlobeCanvasProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);
  const sceneRef = useRef<Scene>({ markers: [], arcs: [], ring: null, animate: false });
  const viewRef = useRef<View | null>(null);
  const meridianRef = useRef(props.origin.longitude);
  const originLatRef = useRef(props.origin.latitude);
  const landRef = useRef<{ meridian: number; land: Path2D; graticule: Path2D } | null>(null);
  const hitArcsRef = useRef<Array<{ path: Path2D; arc: Arc }>>([]);
  const requestDrawRef = useRef<() => void>(() => {});
  const onSelectRef = useRef(props.onSelect);
  useEffect(() => {
    onSelectRef.current = props.onSelect;
  });
  const clickedRef = useRef(false);

  // Mount once: canvas, render loop and interaction.
  useEffect(() => {
    const el = containerRef.current;
    const canvas = canvasRef.current;
    const tip = tooltipRef.current;
    const ctx = canvas?.getContext("2d");
    if (!el || !canvas || !tip || !ctx) return;

    let frame = 0;

    const draw = (now: number) => {
      frame = 0;
      const v = viewRef.current;
      if (!v || v.w === 0) return;
      if (v.tween) {
        const t = Math.min(1, (now - v.tween.t0) / v.tween.ms);
        const e = t < 0.5 ? 2 * t * t : 1 - (-2 * t + 2) ** 2 / 2;
        v.tx = v.tween.fromTx + (v.tween.toTx - v.tween.fromTx) * e;
        v.ty = v.tween.fromTy + (v.tween.toTy - v.tween.fromTy) * e;
        if (t >= 1) v.tween = null;
        clamp(v);
      }
      const { w, h, dpr, s, tx, ty } = v;
      const scene = sceneRef.current;
      const meridian = meridianRef.current;
      if (landRef.current?.meridian !== meridian) {
        landRef.current = {
          meridian,
          land: landPath(meridian),
          graticule: graticulePath(meridian),
        };
      }
      const { land, graticule } = landRef.current;
      const copies = visibleCopies(tx, s, w);

      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);

      // Ocean and graticule for every copy first, then land on top, so land
      // spilling past a copy's edge is not painted over by the next ocean.
      for (const k of copies) {
        ctx.setTransform(dpr * s, 0, 0, dpr * s, dpr * (tx + k * s), dpr * ty);
        ctx.fillStyle = COLORS.globe;
        ctx.fillRect(-0.5, -0.5, 1, 1);
        ctx.lineWidth = 1 / s;
        ctx.strokeStyle = "rgba(148, 163, 184, 0.07)";
        ctx.stroke(graticule);
      }
      // Land x is relative to the meridian but unwrapped, so it can extend up to
      // a world beyond its copy: draw the neighbouring copies too.
      for (let k = copies[0]! - 1; k <= copies[copies.length - 1]! + 1; k++) {
        ctx.setTransform(dpr * s, 0, 0, dpr * s, dpr * (tx + k * s), dpr * ty);
        ctx.fillStyle = "rgba(148, 163, 184, 0.12)";
        ctx.fill(land);
        ctx.lineWidth = 0.75 / s;
        ctx.strokeStyle = COLORS.land;
        ctx.stroke(land);
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

      const hitArcs: Array<{ path: Path2D; arc: Arc }> = [];
      ctx.lineCap = "round";
      for (const k of copies) {
        for (const a of scene.arcs) {
          const g = arcGeometry(a, k, s, tx, ty);
          hitArcs.push({ path: g.path, arc: a });
          const grad = ctx.createLinearGradient(g.x0, g.y0, g.x1, g.y1);
          grad.addColorStop(0, withAlpha(a.color, a.alpha * 0.15));
          grad.addColorStop(1, withAlpha(a.color, a.alpha));
          ctx.strokeStyle = grad;
          ctx.lineWidth = a.width;
          if (scene.animate) {
            ctx.setLineDash([g.length * 0.4, g.length * 0.2]);
            ctx.lineDashOffset = -((now % DASH_PERIOD_MS) / DASH_PERIOD_MS) * g.length;
          } else {
            ctx.setLineDash([]);
          }
          ctx.stroke(g.path);
        }
      }
      ctx.setLineDash([]);
      hitArcsRef.current = hitArcs;

      for (const k of copies) {
        for (const m of scene.markers) {
          const x = tx + (m.x + k) * s;
          const y = ty + m.y * s;
          if (x < -20 || x > w + 20 || y < -20 || y > h + 20) continue;
          ctx.beginPath();
          ctx.arc(x, y, m.radius, 0, Math.PI * 2);
          ctx.fillStyle = m.color;
          ctx.fill();
          if (m.selected || m.isOrigin) {
            ctx.lineWidth = 1.5;
            ctx.strokeStyle = m.isOrigin ? "rgba(0,0,0,0.5)" : COLORS.selected;
            ctx.stroke();
          }
        }
        if (scene.ring && scene.animate) {
          const p = (now % RING_PERIOD_MS) / RING_PERIOD_MS;
          ctx.beginPath();
          ctx.arc(tx + (scene.ring[0] + k) * s, ty + scene.ring[1] * s, 6 + p * 18, 0, Math.PI * 2);
          ctx.lineWidth = 1.5;
          ctx.strokeStyle = withAlpha(COLORS.selected, 1 - p);
          ctx.stroke();
        }
      }

      if (scene.animate || v.tween) frame = requestAnimationFrame(draw);
    };

    const requestDraw = () => {
      if (!frame) frame = requestAnimationFrame(draw);
    };
    requestDrawRef.current = requestDraw;

    const resize = () => {
      const w = el.clientWidth;
      const h = el.clientHeight;
      const dpr = window.devicePixelRatio || 1;
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      canvas.style.width = `${w}px`;
      canvas.style.height = `${h}px`;
      const prev = viewRef.current;
      if (!prev || prev.w === 0) {
        // Fit the width, origin's meridian centred, leaning towards the origin's latitude.
        const s = w;
        const oy = mercatorY(originLatRef.current);
        viewRef.current = { w, h, dpr, s, tx: w / 2, ty: h / 2 - oy * s * 0.5, tween: null };
      } else {
        // Keep the zoom relative to the width and the centre point fixed.
        const s = Math.max(Math.min(w, h), (prev.s * w) / prev.w);
        const cx = (prev.w / 2 - prev.tx) / prev.s;
        const cy = (prev.h / 2 - prev.ty) / prev.s;
        Object.assign(prev, { tx: w / 2 - cx * s, ty: h / 2 - cy * s, w, h, dpr, s });
      }
      clamp(viewRef.current!);
      requestDraw();
    };
    resize();
    const ro = new ResizeObserver(resize);
    ro.observe(el);

    const toLocal = (e: { clientX: number; clientY: number }) => {
      const r = canvas.getBoundingClientRect();
      return [e.clientX - r.left, e.clientY - r.top] as const;
    };

    const hitTest = (x: number, y: number): { marker?: Marker; arc?: Arc } => {
      const v = viewRef.current!;
      let best: Marker | undefined;
      let bestD = Infinity;
      for (const k of visibleCopies(v.tx, v.s, v.w)) {
        for (const m of sceneRef.current.markers) {
          const d = Math.hypot(v.tx + (m.x + k) * v.s - x, v.ty + m.y * v.s - y);
          if (d <= m.radius + 4 && d < bestD) {
            best = m;
            bestD = d;
          }
        }
      }
      if (best) return { marker: best };
      ctx.setTransform(v.dpr, 0, 0, v.dpr, 0, 0);
      ctx.lineWidth = 8;
      // Last drawn is on top.
      for (let i = hitArcsRef.current.length - 1; i >= 0; i--) {
        const h = hitArcsRef.current[i]!;
        if (ctx.isPointInStroke(h.path, x * v.dpr, y * v.dpr)) return { arc: h.arc };
      }
      return {};
    };

    let drag: { x: number; y: number; moved: boolean } | null = null;

    const onPointerDown = (e: PointerEvent) => {
      if (e.button !== 0) return;
      canvas.setPointerCapture(e.pointerId);
      drag = { x: e.clientX, y: e.clientY, moved: false };
    };
    const onPointerMove = (e: PointerEvent) => {
      const v = viewRef.current;
      if (!v) return;
      if (drag) {
        const dx = e.clientX - drag.x;
        const dy = e.clientY - drag.y;
        if (!drag.moved && Math.hypot(dx, dy) < 4) return;
        drag.moved = true;
        drag.x = e.clientX;
        drag.y = e.clientY;
        v.tween = null;
        v.tx += dx;
        v.ty += dy;
        clamp(v);
        tip.style.display = "none";
        canvas.style.cursor = "grabbing";
        requestDraw();
        return;
      }
      const [x, y] = toLocal(e);
      const hit = hitTest(x, y);
      const label = hit.marker?.label ?? hit.arc?.label;
      canvas.style.cursor = label ? "pointer" : "grab";
      if (label) {
        tip.innerHTML = label;
        tip.style.display = "block";
        const left = Math.min(x + 14, v.w - tip.offsetWidth - 4);
        const top = y + 14 + tip.offsetHeight > v.h ? y - 14 - tip.offsetHeight : y + 14;
        tip.style.transform = `translate(${Math.max(4, left)}px, ${Math.max(4, top)}px)`;
      } else {
        tip.style.display = "none";
      }
    };
    const onPointerUp = (e: PointerEvent) => {
      if (!drag) return;
      const wasDrag = drag.moved;
      drag = null;
      canvas.style.cursor = "grab";
      if (wasDrag) return;
      const [x, y] = toLocal(e);
      const hit = hitTest(x, y);
      if (hit.marker) {
        clickedRef.current = !hit.marker.isOrigin;
        onSelectRef.current(hit.marker.isOrigin ? null : hit.marker.key);
      } else if (hit.arc) {
        clickedRef.current = true;
        onSelectRef.current(hit.arc.key);
      } else {
        onSelectRef.current(null);
      }
    };
    const onLeave = () => {
      tip.style.display = "none";
    };
    const onWheel = (e: WheelEvent) => {
      const v = viewRef.current;
      if (!v) return;
      e.preventDefault();
      const [x, y] = toLocal(e);
      const minS = Math.min(v.w, v.h);
      const s = Math.min(minS * MAX_ZOOM, Math.max(minS, v.s * Math.exp(-e.deltaY * 0.0015)));
      v.tween = null;
      v.tx = x - ((x - v.tx) * s) / v.s;
      v.ty = y - ((y - v.ty) * s) / v.s;
      v.s = s;
      clamp(v);
      requestDraw();
    };

    canvas.style.cursor = "grab";
    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerup", onPointerUp);
    canvas.addEventListener("pointercancel", onPointerUp);
    canvas.addEventListener("pointerleave", onLeave);
    canvas.addEventListener("wheel", onWheel, { passive: false });
    return () => {
      ro.disconnect();
      cancelAnimationFrame(frame);
      requestDrawRef.current = () => {};
      canvas.removeEventListener("pointerdown", onPointerDown);
      canvas.removeEventListener("pointermove", onPointerMove);
      canvas.removeEventListener("pointerup", onPointerUp);
      canvas.removeEventListener("pointercancel", onPointerUp);
      canvas.removeEventListener("pointerleave", onLeave);
      canvas.removeEventListener("wheel", onWheel);
    };
  }, []);

  // Rebuild the scene in world coordinates; the render loop picks it up.
  const { origin, destinations, selectedKey, direction, animate } = props;
  useEffect(() => {
    const meridian = origin.longitude;
    meridianRef.current = meridian;
    const located = destinations.filter((d) => d.location !== null);
    const anySelected = selectedKey !== null && located.some((d) => d.key === selectedKey);
    const [ox, oy] = projectPoint(origin.latitude, origin.longitude, meridian);

    const markers: Marker[] = [];
    const arcs: Arc[] = [];
    let ring: [number, number] | null = null;
    for (const d of located) {
      const loc = d.location!;
      const [x, y] = projectPoint(loc.latitude, loc.longitude, meridian);
      const total = d.inbound_bps.value + d.outbound_bps.value;
      const selected = d.key === selectedKey;
      const dim = anySelected && !selected;
      const base = d.inbound_bps.value >= d.outbound_bps.value ? COLORS.inbound : COLORS.outbound;
      markers.push({
        key: d.key,
        x,
        y,
        radius: (2 + markerRadius(total) * 4) * (selected ? 1.25 : 1),
        color: selected ? COLORS.selected : withAlpha(base, dim ? 0.25 : trafficOpacity(total)),
        selected,
        isOrigin: false,
        label: destinationLabel(d),
      });
      if (selected) ring = [x, y];

      for (const dir of ["outbound", "inbound"] as const) {
        if (direction !== "both" && direction !== dir) continue;
        const m = dir === "outbound" ? d.outbound_bps : d.inbound_bps;
        if (m.value <= 0) continue;
        const fromOrigin = dir === "outbound";
        arcs.push({
          key: d.key,
          x0: fromOrigin ? ox : x,
          y0: fromOrigin ? oy : y,
          x1: fromOrigin ? x : ox,
          y1: fromOrigin ? y : oy,
          bend: dir === "outbound" ? 0.15 : 0.25,
          width: (0.75 + arcStroke(m.value) * 2.25) * (selected ? 1.4 : 1),
          color: dir === "outbound" ? COLORS.outbound : COLORS.inbound,
          alpha: selected ? 1 : dim ? 0.08 : trafficOpacity(m.value),
          label: arcLabel(d, dir, m),
        });
      }
    }
    // Selected items last so they draw on top.
    markers.sort((a, b) => Number(a.selected) - Number(b.selected));
    arcs.sort((a, b) => Number(a.key === selectedKey) - Number(b.key === selectedKey));
    markers.push({
      key: "__origin__",
      x: ox,
      y: oy,
      radius: 5,
      color: COLORS.origin,
      selected: false,
      isOrigin: true,
      label: originLabel(origin),
    });
    sceneRef.current = { markers, arcs, ring, animate };
    requestDrawRef.current();
  }, [origin, destinations, selectedKey, direction, animate]);

  // Bring a newly selected destination into view (unless it was clicked on the map).
  const located = destinations.find((d) => d.key === selectedKey)?.location;
  const lat = located?.latitude;
  const lng = located?.longitude;
  useEffect(() => {
    const v = viewRef.current;
    if (!v || lat === undefined || lng === undefined) return;
    if (clickedRef.current) {
      clickedRef.current = false;
      return;
    }
    const [x, y] = projectPoint(lat, lng, meridianRef.current);
    // Pan to the copy nearest the current view so the map never spins a whole world.
    const k = Math.round((v.w / 2 - v.tx) / v.s - x);
    const toTx = v.w / 2 - (x + k) * v.s;
    const toTy = v.h / 2 - y * v.s;
    if (animate) {
      v.tween = { fromTx: v.tx, fromTy: v.ty, toTx, toTy, t0: performance.now(), ms: 900 };
    } else {
      v.tx = toTx;
      v.ty = toTy;
      clamp(v);
    }
    requestDrawRef.current();
  }, [selectedKey, lat, lng, animate]);

  return (
    <div ref={containerRef} className="absolute inset-0 overflow-hidden">
      <canvas ref={canvasRef} className="absolute inset-0 touch-none" aria-hidden />
      <div
        ref={tooltipRef}
        className="pointer-events-none absolute top-0 left-0 z-10 max-w-xs"
        style={{ display: "none" }}
      />
    </div>
  );
}
