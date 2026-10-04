"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useMemo } from "react";
import { hrefFor, parseContext, type InvestigationContext, type ViewName } from "./context";

export function useCurrentView(): ViewName {
  return usePathname()?.startsWith("/home") ? "home" : "globe";
}

/** Investigation context backed by the URL query string. */
export function useInvestigation(): [
  InvestigationContext,
  (next: Partial<InvestigationContext> | ((c: InvestigationContext) => InvestigationContext)) => void,
] {
  const params = useSearchParams();
  const router = useRouter();
  const view = useCurrentView();
  const ctx = useMemo(() => parseContext(params), [params]);

  const update = useCallback(
    (next: Partial<InvestigationContext> | ((c: InvestigationContext) => InvestigationContext)) => {
      const resolved = typeof next === "function" ? next(ctx) : { ...ctx, ...next };
      router.replace(hrefFor(view, resolved), { scroll: false });
    },
    [ctx, router, view],
  );

  return [ctx, update];
}

export function useReducedMotionPreference(pref: "system" | "reduce" | "full"): boolean {
  const system =
    typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  return pref === "reduce" || (pref === "system" && Boolean(system));
}
