"use client";

import { PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useUiStore } from "@/lib/state/ui-store";
import { cn } from "@/lib/utils";

/**
 * View layout: summary strip, collapsible filters (left), dominant
 * visualization (center), collapsible inspector (right).
 */
export function Workspace({
  summary,
  filters,
  inspector,
  inspectorTitle,
  children,
}: {
  summary: React.ReactNode;
  filters: React.ReactNode;
  inspector: React.ReactNode;
  inspectorTitle: string;
  children: React.ReactNode;
}) {
  const { leftOpen, rightOpen, setLeftOpen, setRightOpen } = useUiStore();

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-2 border-b border-border/60 px-2 py-1.5">
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={() => setLeftOpen(!leftOpen)}
          aria-label={leftOpen ? "Hide filters" : "Show filters"}
          aria-expanded={leftOpen}
        >
          {leftOpen ? <PanelLeftClose /> : <PanelLeftOpen />}
        </Button>
        <div className="min-w-0 flex-1 overflow-x-auto">{summary}</div>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={() => setRightOpen(!rightOpen)}
          aria-label={rightOpen ? "Hide inspector" : "Show inspector"}
          aria-expanded={rightOpen}
        >
          {rightOpen ? <PanelRightClose /> : <PanelRightOpen />}
        </Button>
      </div>
      <div className="relative flex min-h-0 flex-1">
        <aside
          aria-label="Filters"
          className={cn(
            "shrink-0 overflow-x-hidden overflow-y-auto border-r border-border/60 bg-sidebar/80 transition-[width] duration-200",
            leftOpen ? "w-64" : "w-0 border-r-0",
            // Overlay instead of squeezing the canvas on narrow screens.
            "max-md:absolute max-md:inset-y-0 max-md:left-0 max-md:z-20",
          )}
        >
          {leftOpen && <div className="min-w-0 space-y-5 p-3">{filters}</div>}
        </aside>
        <main className="relative min-w-0 flex-1 overflow-hidden">{children}</main>
        <aside
          aria-label={inspectorTitle}
          className={cn(
            "shrink-0 overflow-x-hidden overflow-y-auto border-l border-border/60 bg-sidebar/80 transition-[width] duration-200",
            rightOpen ? "w-80" : "w-0 border-l-0",
            "max-md:absolute max-md:inset-y-0 max-md:right-0 max-md:z-20",
          )}
        >
          {rightOpen && <div className="min-w-0 p-3">{inspector}</div>}
        </aside>
      </div>
    </div>
  );
}

export function SummaryItem({
  label,
  children,
  accent,
}: {
  label: string;
  children: React.ReactNode;
  accent?: string;
}) {
  return (
    <div className="flex items-baseline gap-2 px-3 text-sm">
      <span className="text-xs text-muted-foreground" style={accent ? { color: accent } : undefined}>
        {label}
      </span>
      <span className="font-medium">{children}</span>
    </div>
  );
}

export function FilterSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">{title}</h3>
      {children}
    </section>
  );
}
