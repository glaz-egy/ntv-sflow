"use client";

import type { Measurement } from "@/contracts";
import { formatMeasurement, measurementTitle } from "@/lib/format/measurement";
import { cn } from "@/lib/utils";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

/**
 * Renders a rate with its provenance. Sampled estimates show "≈" and an
 * "est" tag; counter values show a "ctr" tag. Details on hover/focus.
 */
export function Rate({
  m,
  className,
  showKind = true,
}: {
  m: Measurement | null | undefined;
  className?: string;
  showKind?: boolean;
}) {
  const lowConfidence = m?.measurement_kind === "sampled_estimate" && (m.sample_count ?? 99) < 10;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          tabIndex={0}
          className={cn("tabular inline-flex items-baseline gap-1 whitespace-nowrap outline-none", className)}
        >
          <span className={cn(lowConfidence && "opacity-70")}>{formatMeasurement(m)}</span>
          {showKind && m && (
            <span
              className={cn(
                "rounded px-1 text-[9px] font-medium tracking-wide uppercase",
                m.measurement_kind === "counter"
                  ? "bg-emerald-500/15 text-emerald-300"
                  : "bg-slate-500/20 text-slate-300",
              )}
            >
              {m.measurement_kind === "counter" ? "ctr" : "est"}
            </span>
          )}
        </span>
      </TooltipTrigger>
      <TooltipContent side="top">{measurementTitle(m)}</TooltipContent>
    </Tooltip>
  );
}
