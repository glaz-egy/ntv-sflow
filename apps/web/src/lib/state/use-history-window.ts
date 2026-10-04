"use client";

import { useData } from "@/lib/data/data-context";
import { historicalRange } from "./context";
import { useInvestigation } from "./use-investigation";

export interface HistoryWindow {
  /** Historical window to query, or null for live data. */
  range: { start: string; end: string } | null;
  /** The URL asks for a historical window but this backend keeps no history. */
  unavailable: boolean;
}

/**
 * The historical window selected in the URL (`at`/`span`, D-059). Views pass
 * it to every query; with no history on the backend they fall back to live
 * data and the shell says so — history is never faked from live data.
 */
export function useHistoryWindow(): HistoryWindow {
  const [ctx] = useInvestigation();
  const { status } = useData();
  const requested = historicalRange(ctx);
  if (!requested) return { range: null, unavailable: false };
  // Before the first status we cannot know; ask for history (the in-browser
  // mock rejects it, so nothing wrong is shown in the meantime).
  if (status && status.history === null) return { range: null, unavailable: true };
  return { range: requested, unavailable: false };
}
