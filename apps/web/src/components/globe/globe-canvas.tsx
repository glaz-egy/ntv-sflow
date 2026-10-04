"use client";

import Globe, { type GlobeInstance } from "globe.gl";
import { polygonToCells } from "h3-js";
import { useEffect, useRef } from "react";
import { MeshPhongMaterial } from "three";
import { feature } from "topojson-client";
import type { Topology } from "topojson-specification";
import countriesTopo from "world-atlas/countries-110m.json";
import type { DirectionFilter, GlobeDestination, GlobeOrigin } from "@/contracts";
import { COLORS, withAlpha } from "@/lib/viz/colors";
import { arcStroke, markerRadius, trafficOpacity } from "@/lib/viz/scales";
import { angularDistance, arcLabel, destinationLabel, originLabel } from "./labels";

const HEX_RESOLUTION = 3;

interface PointDatum {
  key: string;
  lat: number;
  lng: number;
  radius: number;
  color: string;
  altitude: number;
  label: string;
  isOrigin: boolean;
}

interface ArcDatum {
  id: string;
  key: string;
  direction: "inbound" | "outbound";
  startLat: number;
  startLng: number;
  endLat: number;
  endLng: number;
  stroke: number;
  altitude: number;
  colors: [string, string];
  dashLength: number;
  dashGap: number;
  animateMs: number;
  label: string;
}

export interface GlobeCanvasProps {
  origin: GlobeOrigin;
  destinations: GlobeDestination[];
  selectedKey: string | null;
  direction: DirectionFilter;
  animate: boolean;
  onSelect: (key: string | null) => void;
}

export default function GlobeCanvas(props: GlobeCanvasProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const globeRef = useRef<GlobeInstance | null>(null);
  const pointsRef = useRef(new Map<string, PointDatum>());
  const arcsRef = useRef(new Map<string, ArcDatum>());
  const onSelectRef = useRef(props.onSelect);
  useEffect(() => {
    onSelectRef.current = props.onSelect;
  });
  const clickedRef = useRef(false);

  // Mount once.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const topo = countriesTopo as unknown as Topology;
    const countries = feature(topo, topo.objects.countries!) as unknown as {
      features: Array<{ geometry: { type: string; coordinates: number[][][] | number[][][][] } }>;
    };
    // A few dataset polygons fail H3 hex conversion; draw those as flat land
    // instead of dropping them.
    const hexable = (f: (typeof countries.features)[number]) => {
      const polys = (f.geometry.type === "Polygon" ? [f.geometry.coordinates] : f.geometry.coordinates) as number[][][][];
      try {
        for (const p of polys) polygonToCells(p, HEX_RESOLUTION, true);
        return true;
      } catch {
        return false;
      }
    };
    const hexLand = countries.features.filter(hexable);
    const flatLand = countries.features.filter((f) => !hexLand.includes(f));

    const globe = new Globe(el, { animateIn: false })
      .backgroundColor("rgba(0,0,0,0)")
      .showAtmosphere(true)
      .atmosphereColor("#3b82f6")
      .atmosphereAltitude(0.16)
      .globeMaterial(new MeshPhongMaterial({ color: COLORS.globe, emissive: "#050a14", shininess: 4 }))
      .hexPolygonsData(hexLand)
      .hexPolygonResolution(HEX_RESOLUTION)
      .hexPolygonMargin(0.4)
      .hexPolygonUseDots(true)
      .hexPolygonColor(() => COLORS.land)
      .polygonsData(flatLand)
      .polygonCapColor(() => "rgba(148, 163, 184, 0.12)")
      .polygonSideColor(() => "rgba(0,0,0,0)")
      .polygonStrokeColor(() => COLORS.land)
      .polygonAltitude(0.002)
      .pointLat("lat")
      .pointLng("lng")
      .pointRadius("radius")
      .pointColor("color")
      .pointAltitude("altitude")
      .pointResolution(16)
      .pointsTransitionDuration(0)
      .pointLabel((d) => (d as PointDatum).label)
      .arcStartLat("startLat")
      .arcStartLng("startLng")
      .arcEndLat("endLat")
      .arcEndLng("endLng")
      .arcStroke("stroke")
      .arcAltitude("altitude")
      .arcColor("colors")
      .arcDashLength("dashLength")
      .arcDashGap("dashGap")
      .arcDashAnimateTime("animateMs")
      .arcsTransitionDuration(0)
      .arcLabel((d) => (d as ArcDatum).label)
      .ringColor(() => (t: number) => withAlpha(COLORS.selected, 1 - t))
      .ringMaxRadius(3.5)
      .ringPropagationSpeed(2)
      .ringRepeatPeriod(1400)
      .onPointClick((d) => {
        clickedRef.current = true;
        const p = d as PointDatum;
        onSelectRef.current(p.isOrigin ? null : p.key);
      })
      .onArcClick((d) => {
        clickedRef.current = true;
        onSelectRef.current((d as ArcDatum).key);
      })
      .onGlobeClick(() => onSelectRef.current(null))
      .pointOfView({ lat: props.origin.latitude - 10, lng: props.origin.longitude - 30, altitude: 2.3 }, 0);

    const controls = globe.controls();
    controls.autoRotateSpeed = 0.25;
    controls.minDistance = 130;
    controls.maxDistance = 600;
    globeRef.current = globe;

    const resize = () => globe.width(el.clientWidth).height(el.clientHeight);
    resize();
    const ro = new ResizeObserver(resize);
    ro.observe(el);
    return () => {
      ro.disconnect();
      globe._destructor();
      globeRef.current = null;
      el.replaceChildren();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Update data in place: datum objects are stable per key so three-globe
  // updates existing meshes rather than recreating them each tick.
  const { origin, destinations, selectedKey, direction, animate } = props;
  useEffect(() => {
    const globe = globeRef.current;
    if (!globe) return;

    const located = destinations.filter((d) => d.location !== null);
    const points = pointsRef.current;
    const arcs = arcsRef.current;
    const seenPoints = new Set<string>(["__origin__"]);
    const seenArcs = new Set<string>();
    const anySelected = selectedKey !== null && located.some((d) => d.key === selectedKey);

    const originPoint: PointDatum = points.get("__origin__") ?? ({ key: "__origin__" } as PointDatum);
    Object.assign(originPoint, {
      lat: origin.latitude,
      lng: origin.longitude,
      radius: 0.45,
      color: COLORS.origin,
      altitude: 0.012,
      isOrigin: true,
      label: originLabel(origin),
    });
    points.set("__origin__", originPoint);

    for (const d of located) {
      const loc = d.location!;
      const total = d.inbound_bps.value + d.outbound_bps.value;
      const selected = d.key === selectedKey;
      const dim = anySelected && !selected;
      const dominantIn = d.inbound_bps.value >= d.outbound_bps.value;
      const base = dominantIn ? COLORS.inbound : COLORS.outbound;
      const p = points.get(d.key) ?? ({ key: d.key } as PointDatum);
      Object.assign(p, {
        lat: loc.latitude,
        lng: loc.longitude,
        radius: markerRadius(total) * (selected ? 1.25 : 1),
        color: selected ? COLORS.selected : withAlpha(base, dim ? 0.25 : trafficOpacity(total)),
        altitude: selected ? 0.02 : 0.01,
        isOrigin: false,
        label: destinationLabel(d),
      });
      points.set(d.key, p);
      seenPoints.add(d.key);

      const dist = angularDistance(origin.latitude, origin.longitude, loc.latitude, loc.longitude);
      for (const dir of ["outbound", "inbound"] as const) {
        if (direction !== "both" && direction !== dir) continue;
        const m = dir === "outbound" ? d.outbound_bps : d.inbound_bps;
        if (m.value <= 0) continue;
        const id = `${d.key}|${dir}`;
        const color = dir === "outbound" ? COLORS.outbound : COLORS.inbound;
        const alpha = selected ? 1 : dim ? 0.08 : trafficOpacity(m.value);
        const a = arcs.get(id) ?? ({ id, key: d.key, direction: dir } as ArcDatum);
        const fromOrigin = dir === "outbound";
        Object.assign(a, {
          startLat: fromOrigin ? origin.latitude : loc.latitude,
          startLng: fromOrigin ? origin.longitude : loc.longitude,
          endLat: fromOrigin ? loc.latitude : origin.latitude,
          endLng: fromOrigin ? loc.longitude : origin.longitude,
          stroke: arcStroke(m.value) * (selected ? 1.4 : 1),
          // Separate heights keep inbound/outbound arcs distinguishable.
          altitude: Math.max(0.04, dist * (dir === "outbound" ? 0.2 : 0.3)),
          // Gradient source→destination is a static direction cue that
          // survives reduced motion.
          colors: [withAlpha(color, alpha * 0.15), withAlpha(color, alpha)],
          dashLength: animate ? 0.4 : 1,
          dashGap: animate ? 0.2 : 0,
          animateMs: animate ? 3000 : 0,
          label: arcLabel(d, dir, m),
        });
        arcs.set(id, a);
        seenArcs.add(id);
      }
    }
    for (const k of [...points.keys()]) if (!seenPoints.has(k)) points.delete(k);
    for (const k of [...arcs.keys()]) if (!seenArcs.has(k)) arcs.delete(k);

    const selected = located.find((d) => d.key === selectedKey);
    globe
      .pointsData([...points.values()])
      .arcsData([...arcs.values()])
      .ringsData(
        animate && selected?.location ? [{ lat: selected.location.latitude, lng: selected.location.longitude }] : [],
      );
    globe.controls().autoRotate = animate && !anySelected;
  }, [origin, destinations, selectedKey, direction, animate]);

  // Bring a newly selected destination into view (unless it was clicked on the globe).
  const located = destinations.find((d) => d.key === selectedKey)?.location;
  const lat = located?.latitude;
  const lng = located?.longitude;
  useEffect(() => {
    const globe = globeRef.current;
    if (!globe || lat === undefined || lng === undefined) return;
    if (clickedRef.current) {
      clickedRef.current = false;
      return;
    }
    globe.pointOfView({ lat, lng, altitude: globe.pointOfView().altitude }, animate ? 900 : 0);
  }, [selectedKey, lat, lng, animate]);

  return <div ref={containerRef} className="absolute inset-0" aria-hidden />;
}
