"use client";

import { ChevronLeft, ChevronRight, History, Pause, Play, Radio } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { HistoryTimelineResponse } from "@/contracts";
import { Button } from "@/components/ui/button";
import { SelectField } from "@/components/common/fields";
import { useData } from "@/lib/data/data-context";
import { formatBps } from "@/lib/format/measurement";
import { isoSeconds } from "@/lib/state/context";
import { useHistoryWindow } from "@/lib/state/use-history-window";
import { useInvestigation } from "@/lib/state/use-investigation";
import { useUiStore } from "@/lib/state/ui-store";
import { COLORS, withAlpha } from "@/lib/viz/colors";
import {
  RANGE_PRESETS,
  REPLAY_SPEEDS,
  WINDOW_SPANS,
  clampWindowEnd,
  replayStep,
  timelineGeometry,
  visibleRange,
  windowEndForClick,
  windowForDrag,
} from "@/lib/viz/timeline";

const CHART_HEIGHT = 44;
const REFRESH_MS = 10_000;

function fmtTime(ms: number, withDate: boolean): string {
  const d = new Date(ms);
  const time = d.toLocaleTimeString(undefined, { hour12: false });
  return withDate
    ? `${d.toLocaleDateString(undefined, { month: "2-digit", day: "2-digit" })} ${time}`
    : time;
}

function fmtSpan(seconds: number): string {
  if (seconds % 3600 === 0) return `${seconds / 3600} h`;
  if (seconds % 60 === 0) return `${seconds / 60} min`;
  return `${seconds} s`;
}

/**
 * History timeline (D-059): traffic over time, a selectable historical
 * window, step and replay. Only shown when the backend keeps history; the
 * views then query [at − span, at) instead of the live window.
 */
export function Timeline() {
  const { status } = useData();
  const { unavailable } = useHistoryWindow();
  const [, update] = useInvestigation();
  if (unavailable) {
    return (
      <div
        role="status"
        className="flex shrink-0 items-center gap-3 border-t border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-200"
      >
        <History className="size-3.5" />
        This link selects a historical window, but this backend keeps no history — showing live
        data.
        <Button size="xs" variant="outline" onClick={() => update({ at: null })}>
          Dismiss
        </Button>
      </div>
    );
  }
  if (!status?.history) return null;
  return <TimelineBar />;
}

function TimelineBar() {
  const { provider, status } = useData();
  const [ctx, update] = useInvestigation();
  const { timelineRange, setTimelineRange, replaying, setReplaying, replaySpeed, setReplaySpeed } =
    useUiStore();
  // Rendered only when status.history is set (see Timeline).
  const history = status!.history!;
  const earliest = history.earliest ? Date.parse(history.earliest) : null;
  const latest = history.latest ? Date.parse(history.latest) : null;
  const atMs = ctx.at ? Date.parse(ctx.at) : null;
  const isLive = atMs === null;

  // The server clock (the mock may run faster than wall time).
  const serverNow = Date.parse(status!.server_time);
  const nowBucket = Math.floor(serverNow / REFRESH_MS) * REFRESH_MS; // refetch at most every 10 s
  const range = useMemo(
    () => visibleRange(nowBucket, timelineRange, atMs),
    [nowBucket, timelineRange, atMs],
  );

  const chartRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(600);
  useEffect(() => {
    const el = chartRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) =>
      setWidth(Math.max(100, Math.round(e.contentRect.width))),
    );
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const maxPoints = Math.min(720, Math.max(20, Math.floor(width / 3)));

  const [timeline, setTimeline] = useState<HistoryTimelineResponse | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let cancelled = false;
    provider
      .getHistoryTimeline(
        new Date(range.start).toISOString(),
        new Date(range.end).toISOString(),
        maxPoints,
      )
      .then(
        (t) => {
          if (!cancelled) {
            setTimeline(t);
            setFailed(false);
          }
        },
        () => !cancelled && setFailed(true),
      );
    return () => {
      cancelled = true;
    };
  }, [provider, range, maxPoints]);

  const select = (end: number, span = ctx.span, keepReplay = false) => {
    if (!keepReplay) setReplaying(false);
    const at = clampWindowEnd(end, span, earliest, latest);
    if (at !== null) update({ at: isoSeconds(at), span });
  };

  // Replay: the window moves `speed` seconds per wall-clock second. The
  // interval depends only on the replay settings (status updates re-render
  // every second); current values are read through a ref.
  const carry = useRef(0);
  const cursor = useRef<number | null>(null);
  const current = useRef({ atMs, latest, update });
  useLayoutEffect(() => {
    current.current = { atMs, latest, update };
  });
  useEffect(() => {
    if (!replaying) return;
    cursor.current = null;
    carry.current = 0;
    const id = setInterval(() => {
      const { atMs, latest, update } = current.current;
      // Starting from live: wait until the start window is in the URL.
      const at = cursor.current ?? atMs;
      if (at === null || latest === null) return;
      const next = replayStep(at, replaySpeed, carry.current, latest);
      if (next === null) {
        setReplaying(false);
        return;
      }
      carry.current = next.carry;
      cursor.current = next.atMs;
      if (next.atMs !== at) update({ at: isoSeconds(next.atMs) });
    }, 1000);
    return () => clearInterval(id);
  }, [replaying, replaySpeed, setReplaying]);

  // Pointer: click selects a centred window, drag selects a range.
  const drag = useRef<{ x0: number; moved: boolean } | null>(null);
  const [dragX, setDragX] = useState<[number, number] | null>(null);
  const [hoverX, setHoverX] = useState<number | null>(null);
  const toMs = (px: number) => range.start + (px / width) * (range.end - range.start);
  const toPx = (ms: number) => ((ms - range.start) / (range.end - range.start)) * width;
  const localX = (e: React.PointerEvent) => {
    const r = e.currentTarget.getBoundingClientRect();
    return Math.max(0, Math.min(width, e.clientX - r.left));
  };

  const geometry = useMemo(
    () =>
      timeline
        ? timelineGeometry(timeline.points, timeline.step_seconds, range, width, CHART_HEIGHT)
        : null,
    [timeline, range, width],
  );
  const withDate = range.end - range.start > 24 * 3600 * 1000;
  const hovered = useMemo(() => {
    if (hoverX === null || !timeline) return null;
    const t = toMs(hoverX);
    return timeline.points.find((p) => {
      const s = Date.parse(p.start);
      return t >= s && t < s + timeline.step_seconds * 1000;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hoverX, timeline, range, width]);

  const selStart = atMs !== null ? atMs - ctx.span * 1000 : null;

  return (
    <section
      aria-label="History timeline"
      className="border-border/60 flex shrink-0 items-center gap-3 border-t px-3 py-1.5"
    >
      <div className="flex shrink-0 items-center gap-1.5">
        <Button
          size="xs"
          variant={isLive ? "default" : "outline"}
          onClick={() => {
            setReplaying(false);
            update({ at: null });
          }}
          aria-pressed={isLive}
          title="Show the live window"
        >
          <Radio />
          Live
        </Button>
        <div className="w-20 max-sm:hidden">
          <SelectField
            ariaLabel="Timeline range"
            value={String(timelineRange)}
            onChange={(v) => v && setTimelineRange(Number(v))}
            options={RANGE_PRESETS.map((p) => ({ value: String(p.seconds), label: p.label }))}
          />
        </div>
      </div>

      <div className="relative min-w-0 flex-1">
        <div
          ref={chartRef}
          className="relative h-11 cursor-crosshair touch-none select-none"
          role="slider"
          tabIndex={0}
          aria-label="Historical window end"
          aria-valuemin={earliest ?? undefined}
          aria-valuemax={latest ?? undefined}
          aria-valuenow={atMs ?? latest ?? undefined}
          aria-valuetext={atMs !== null ? `Window ending ${fmtTime(atMs, true)}` : "Live"}
          onKeyDown={(e) => {
            const base = atMs ?? latest;
            if (base === null) return;
            if (e.key === "ArrowLeft") select(base - ctx.span * 1000);
            if (e.key === "ArrowRight") select(base + ctx.span * 1000);
            if (e.key === "End") {
              setReplaying(false);
              update({ at: null });
            }
          }}
          onPointerDown={(e) => {
            e.currentTarget.setPointerCapture(e.pointerId);
            drag.current = { x0: localX(e), moved: false };
          }}
          onPointerMove={(e) => {
            const x = localX(e);
            setHoverX(x);
            if (drag.current && Math.abs(x - drag.current.x0) > 4) {
              drag.current.moved = true;
              setDragX([drag.current.x0, x]);
            }
          }}
          onPointerUp={(e) => {
            const d = drag.current;
            drag.current = null;
            setDragX(null);
            if (!d) return;
            const x = localX(e);
            if (d.moved) {
              const w = windowForDrag(toMs(d.x0), toMs(x));
              select(w.atMs, w.spanS);
            } else {
              select(windowEndForClick(toMs(x), ctx.span));
            }
            setReplaying(false);
          }}
          onPointerLeave={() => setHoverX(null)}
        >
          <svg width={width} height={CHART_HEIGHT} className="block" aria-hidden>
            <line
              x1={0}
              x2={width}
              y1={CHART_HEIGHT / 2}
              y2={CHART_HEIGHT / 2}
              stroke="currentColor"
              className="text-border"
            />
            {earliest !== null && toPx(earliest) > 0 && (
              <rect
                x={0}
                y={0}
                width={toPx(earliest)}
                height={CHART_HEIGHT}
                className="fill-muted/40"
              />
            )}
            {geometry && (
              <>
                <path d={geometry.inbound} fill={withAlpha(COLORS.inbound, 0.45)} />
                <path d={geometry.outbound} fill={withAlpha(COLORS.outbound, 0.45)} />
                <path d={geometry.wanDown} fill="none" stroke={COLORS.inbound} strokeWidth={1} />
                <path d={geometry.wanUp} fill="none" stroke={COLORS.outbound} strokeWidth={1} />
              </>
            )}
            {selStart !== null && atMs !== null && (
              <rect
                x={toPx(selStart)}
                y={0.5}
                width={Math.max(2, toPx(atMs) - toPx(selStart))}
                height={CHART_HEIGHT - 1}
                className="fill-white/15 stroke-white/80"
              />
            )}
            {dragX && (
              <rect
                x={Math.min(...dragX)}
                y={0}
                width={Math.abs(dragX[1] - dragX[0])}
                height={CHART_HEIGHT}
                className="fill-primary/25"
              />
            )}
            {hoverX !== null && (
              <line x1={hoverX} x2={hoverX} y1={0} y2={CHART_HEIGHT} className="stroke-white/50" />
            )}
          </svg>
          {hovered && hoverX !== null && (
            <div
              className="border-border/60 bg-popover tabular pointer-events-none absolute bottom-full z-30 mb-1 rounded-md border px-2 py-1 text-[11px] whitespace-nowrap shadow-md"
              style={{ left: Math.min(Math.max(0, hoverX - 80), width - 170) }}
            >
              <div className="text-muted-foreground">
                {fmtTime(Date.parse(hovered.start), withDate)}
              </div>
              {hovered.has_data ? (
                <>
                  <div style={{ color: COLORS.inbound }}>
                    ↓ ≈{formatBps(hovered.inbound?.value ?? 0)}
                    {hovered.wan_download && ` · ctr ${formatBps(hovered.wan_download.value)}`}
                  </div>
                  <div style={{ color: COLORS.outbound }}>
                    ↑ ≈{formatBps(hovered.outbound?.value ?? 0)}
                    {hovered.wan_upload && ` · ctr ${formatBps(hovered.wan_upload.value)}`}
                  </div>
                </>
              ) : (
                <div className="text-muted-foreground">No stored data</div>
              )}
            </div>
          )}
        </div>
        <div className="text-muted-foreground tabular flex justify-between text-[10px]">
          <span>{fmtTime(range.start, withDate)}</span>
          <span className="max-md:hidden">
            {failed
              ? "History unavailable"
              : `↓ download / ↑ upload · ≈ sampled${geometry?.maxBps ? ` · max ≈${formatBps(geometry.maxBps)}` : ""} · line = counter`}
          </span>
          <span>{fmtTime(range.end, withDate)}</span>
        </div>
      </div>

      <div className="flex shrink-0 items-center gap-1.5">
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label="Previous window"
          disabled={latest === null}
          onClick={() => select((atMs ?? latest!) - ctx.span * 1000)}
        >
          <ChevronLeft />
        </Button>
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label="Next window"
          disabled={atMs === null}
          onClick={() => select(atMs! + ctx.span * 1000)}
        >
          <ChevronRight />
        </Button>
        <Button
          size="xs"
          variant={replaying ? "default" : "outline"}
          aria-pressed={replaying}
          disabled={latest === null}
          onClick={() => {
            if (!replaying && atMs === null && latest !== null)
              select(latest - timelineRange * 500); // start mid-range
            carry.current = 0;
            setReplaying(!replaying);
          }}
          title="Replay: the window moves forward in time"
        >
          {replaying ? <Pause /> : <Play />}
          Replay
        </Button>
        <div className="w-16 max-sm:hidden">
          <SelectField
            ariaLabel="Replay speed"
            value={String(replaySpeed)}
            onChange={(v) => v && setReplaySpeed(Number(v))}
            options={REPLAY_SPEEDS.map((s) => ({ value: String(s), label: `${s}×` }))}
          />
        </div>
        <div className="w-20 max-sm:hidden">
          <SelectField
            ariaLabel="Window length"
            value={String(ctx.span)}
            onChange={(v) => {
              if (!v) return;
              const span = Number(v);
              // Live: show the newest stored window of that length.
              select(atMs ?? latest ?? serverNow, span);
            }}
            options={[
              ...WINDOW_SPANS.map((w) => ({ value: String(w.seconds), label: w.label })),
              ...(WINDOW_SPANS.some((w) => w.seconds === ctx.span)
                ? []
                : [{ value: String(ctx.span), label: fmtSpan(ctx.span) }]),
            ]}
          />
        </div>
      </div>
    </section>
  );
}

/**
 * "14:30:00–14:35:00 (5 min)" for the status bar; null when live. Shows the
 * part of the selected window that has stored data (the API averages over
 * that part only).
 */
export function useHistoryLabel(): string | null {
  const { range } = useHistoryWindow();
  const { status } = useData();
  if (!range) return null;
  const h = status?.history;
  let s = Date.parse(range.start);
  let e = Date.parse(range.end);
  if (h?.earliest && Date.parse(h.earliest) > s && Date.parse(h.earliest) < e)
    s = Date.parse(h.earliest);
  if (h?.latest && Date.parse(h.latest) < e && Date.parse(h.latest) > s) e = Date.parse(h.latest);
  const today = status ? new Date(Date.parse(status.server_time)).toDateString() : "";
  const withDate = new Date(s).toDateString() !== today;
  const partial = e - s < Date.parse(range.end) - Date.parse(range.start);
  return `${fmtTime(s, withDate)}–${fmtTime(e, false)} (${fmtSpan(Math.round((e - s) / 1000))}${partial ? " with data" : ""})`;
}
