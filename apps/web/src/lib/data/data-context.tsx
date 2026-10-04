"use client";

import {
  createContext,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import type { StatusResponse } from "@/contracts";
import { useUiStore } from "@/lib/state/ui-store";
import { HttpDataProvider } from "./http-provider";
import { MockDataProvider } from "./mock-provider";
import type { TrafficDataProvider } from "./provider";

/**
 * NEXT_PUBLIC_DATA_MODE: "mock" (default) runs the mock in the browser;
 * "api" talks to the Go API at NEXT_PUBLIC_API_BASE_URL (D-040).
 */
export const DATA_TRANSPORT = process.env.NEXT_PUBLIC_DATA_MODE === "api" ? "api" : "in-browser";
export const API_BASE_URL = process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8080";

interface DataContextValue {
  provider: TrafficDataProvider;
  /** Increments on every displayed window. Frozen while paused. */
  version: number;
  status: StatusResponse | null;
  /** Wall-clock ms when the last update was received from the provider. */
  receivedAt: number | null;
  scenarioDescription: string | null;
}

const DataContext = createContext<DataContextValue | null>(null);

/** Client-side staleness: no update for this many intervals. */
const STALE_INTERVALS = 3;

export function DataRoot({ children }: { children: React.ReactNode }) {
  const mock = useUiStore((s) => s.mock);
  const paused = useUiStore((s) => s.paused);

  const provider = useMemo<TrafficDataProvider>(
    () => (DATA_TRANSPORT === "api" ? new HttpDataProvider(API_BASE_URL) : new MockDataProvider(mock)),
    // The mock config only matters for the in-browser provider.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [DATA_TRANSPORT === "api" ? null : mock],
  );
  const [latest, setLatest] = useState(0);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [receivedAt, setReceivedAt] = useState<number | null>(null);

  // Pause freezes the displayed window; resume jumps to the latest one.
  const [prevPaused, setPrevPaused] = useState(paused);
  const [pausedVersion, setPausedVersion] = useState(latest);
  if (paused !== prevPaused) {
    setPrevPaused(paused);
    if (paused) setPausedVersion(latest);
  }
  const version = paused ? pausedVersion : latest;

  useEffect(() => {
    let cancelled = false;
    provider.getStatus().then(
      (s) => {
        if (cancelled) return;
        setStatus(s);
        setReceivedAt(Date.now());
        setLatest((v) => v + 1);
      },
      () => {
        // API unreachable: stay "Connecting"; the WebSocket keeps retrying.
      },
    );
    const unsubscribe = provider.subscribe((u) => {
      setStatus(u.status);
      setReceivedAt(Date.now());
      setLatest((v) => v + 1);
    });
    return () => {
      cancelled = true;
      unsubscribe();
      provider.dispose();
    };
  }, [provider]);

  const value = useMemo<DataContextValue>(
    () => ({
      provider,
      version,
      status,
      receivedAt,
      scenarioDescription: provider instanceof MockDataProvider ? provider.scenarioDescription : null,
    }),
    [provider, version, status, receivedAt],
  );
  // The app is live, client-side data; render it only after hydration so the
  // server HTML never disagrees with client clocks/status.
  const hydrated = useSyncExternalStore(
    noopSubscribe,
    () => true,
    () => false,
  );
  return (
    <DataContext.Provider value={value}>
      {hydrated ? children : <div className="h-dvh bg-background" />}
    </DataContext.Provider>
  );
}

const noopSubscribe = () => () => {};

export function useData(): DataContextValue {
  const ctx = useContext(DataContext);
  if (!ctx) throw new Error("useData must be used inside <DataRoot>");
  return ctx;
}

export type LiveState = "live" | "paused" | "stale" | "connecting" | "waiting";

/** Combined live/paused/stale state for status UI. */
export function useLiveState(): LiveState {
  const { status, receivedAt } = useData();
  const paused = useUiStore((s) => s.paused);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  if (!status || receivedAt === null) return "connecting";
  // Live mode before the first sFlow datagram: nothing is stale yet.
  if (status.collector.status === "disconnected") return "waiting";
  const transportStale = now - receivedAt > STALE_INTERVALS * status.update_interval_seconds * 1000;
  if (!status.live || status.collector.status === "stale" || transportStale) return "stale";
  return paused ? "paused" : "live";
}

/**
 * Re-runs `fetcher` whenever a new window is displayed or deps change.
 * Keeps the previous data while refreshing (no flicker between ticks).
 * With `live: false` (a historical window) it runs only when deps change.
 */
export function useLiveQuery<T>(
  fetcher: (provider: TrafficDataProvider) => Promise<T>,
  deps: ReadonlyArray<string | number | boolean | null | undefined>,
  options: { live?: boolean } = {},
): { data: T | undefined; loading: boolean; error: boolean } {
  const { provider, version } = useData();
  const live = options.live ?? true;
  const key = JSON.stringify([live ? version : "history", ...deps]);
  const [result, setResult] = useState<{ key: string; data: T | undefined; error?: boolean } | null>(null);
  const fetcherRef = useRef(fetcher);

  useLayoutEffect(() => {
    fetcherRef.current = fetcher;
  });

  useEffect(() => {
    let cancelled = false;
    fetcherRef.current(provider).then(
      (data) => {
        if (!cancelled) setResult({ key, data });
      },
      () => {
        if (!cancelled) setResult((r) => ({ key, data: r?.data, error: true }));
      },
    );
    return () => {
      cancelled = true;
    };
  }, [provider, key]);

  // Previous data is kept while a newer request is in flight (no flicker).
  return { data: result?.data, loading: result?.key !== key, error: result?.key === key && Boolean(result.error) };
}
