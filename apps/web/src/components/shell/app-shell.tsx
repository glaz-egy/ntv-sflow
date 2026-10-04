"use client";

import Link from "next/link";
import { Globe2, Network, Pause, Play, Settings2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import { API_BASE_URL, DATA_TRANSPORT, useData, useLiveState, type LiveState } from "@/lib/data/data-context";
import { SCENARIO_NAMES, type ScenarioName } from "@/lib/mock-backend/scenarios";
import { globeToHome, homeToGlobe, hrefFor } from "@/lib/state/context";
import { useCurrentView, useInvestigation } from "@/lib/state/use-investigation";
import { useUiStore, type MotionPreference } from "@/lib/state/ui-store";
import { cn } from "@/lib/utils";
import { OptionGroup, SelectField } from "@/components/common/fields";
import { useHistoryWindow } from "@/lib/state/use-history-window";
import { Timeline, useHistoryLabel } from "./timeline";

export function AppShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-dvh flex-col">
      <Header />
      {children}
      <Timeline />
      <StatusBar />
    </div>
  );
}

function Header() {
  const view = useCurrentView();
  const [ctx] = useInvestigation();
  const tabs = [
    { view: "globe" as const, label: "Globe", icon: Globe2, href: hrefFor("globe", homeToGlobe(ctx)) },
    { view: "home" as const, label: "Home Network", icon: Network, href: hrefFor("home", globeToHome(ctx)) },
  ];

  return (
    <header className="flex h-12 shrink-0 items-center gap-4 border-b border-border/60 px-3">
      <div className="flex items-center gap-2">
        <span className="grid size-6 place-items-center rounded-md bg-primary/15 text-[10px] font-bold text-primary">
          NTV
        </span>
        <span className="hidden text-sm font-semibold sm:inline">Network Traffic Visualizer</span>
      </div>
      <nav aria-label="Views" className="flex rounded-lg bg-muted/60 p-0.5">
        {tabs.map((t) => (
          <Link
            key={t.view}
            href={t.href}
            aria-current={view === t.view ? "page" : undefined}
            className={cn(
              "flex items-center gap-1.5 rounded-md px-3 py-1 text-sm transition-colors",
              view === t.view
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <t.icon className="size-4" />
            {t.label}
          </Link>
        ))}
      </nav>
      <div className="flex-1" />
      <LiveBadge />
      <SettingsMenu />
    </header>
  );
}

const BADGE: Record<LiveState, { label: string; className: string; dot: string }> = {
  live: { label: "Live", className: "text-emerald-300", dot: "bg-emerald-400 animate-pulse" },
  paused: { label: "Paused", className: "text-amber-300", dot: "bg-amber-400" },
  stale: { label: "Stale", className: "text-red-300", dot: "bg-red-400" },
  connecting: { label: "Connecting", className: "text-muted-foreground", dot: "bg-slate-400" },
  waiting: { label: "Waiting for sFlow", className: "text-sky-300", dot: "bg-sky-400 animate-pulse" },
};

function LiveBadge() {
  const { status } = useData();
  const state = useLiveState();
  const { range } = useHistoryWindow();
  // A historical window is never shown as "Live".
  const b = range ? { label: "History", className: "text-amber-300", dot: "bg-amber-400" } : BADGE[state];
  return (
    <div
      role="status"
      aria-live="polite"
      className="flex items-center gap-2 rounded-full border border-border/60 px-2.5 py-0.5 text-xs"
    >
      {status?.mode === "mock" && (
        <span className="rounded bg-violet-500/20 px-1.5 font-semibold tracking-wider text-violet-200">
          MOCK
        </span>
      )}
      {status?.mode === "live" && (
        <span
          title="Data from the sFlow collector"
          className="rounded bg-sky-500/20 px-1.5 font-semibold tracking-wider text-sky-200"
        >
          SFLOW
        </span>
      )}
      <span className={cn("size-1.5 rounded-full", b.dot)} aria-hidden />
      <span className={cn("font-medium", b.className)}>{b.label}</span>
    </div>
  );
}

function SettingsMenu() {
  const { particles, setParticles, motion, setMotion, mock, setScenario, setSeed } = useUiStore();
  const { scenarioDescription, status } = useData();
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="Settings">
          <Settings2 />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72 space-y-4">
        <div className="flex items-center justify-between">
          <Label htmlFor="particles">Traffic animation</Label>
          <Switch id="particles" checked={particles} onCheckedChange={setParticles} />
        </div>
        <div className="space-y-1.5">
          <Label>Motion</Label>
          <OptionGroup<MotionPreference>
            value={motion}
            onChange={setMotion}
            options={[
              { value: "system", label: "System" },
              { value: "reduce", label: "Reduce" },
              { value: "full", label: "Full" },
            ]}
          />
        </div>
        {DATA_TRANSPORT === "in-browser" ? (
          <>
            <div className="space-y-1.5 border-t border-border/60 pt-3">
              <Label>Mock scenario</Label>
              <SelectField<ScenarioName>
                value={mock.scenario}
                onChange={(s) => s && setScenario(s)}
                options={SCENARIO_NAMES.map((s) => ({ value: s, label: s }))}
              />
              {scenarioDescription && <p className="text-xs text-muted-foreground">{scenarioDescription}</p>}
            </div>
            <div className="flex items-center justify-between">
              <Label>Seed</Label>
              <div className="flex items-center gap-2">
                <span className="tabular text-sm">{mock.seed}</span>
                <Button size="xs" variant="outline" onClick={() => setSeed(mock.seed + 1)}>
                  Next
                </Button>
              </div>
            </div>
          </>
        ) : (
          <div className="space-y-1 border-t border-border/60 pt-3 text-xs">
            <Label>Data source</Label>
            <p className="font-mono break-all text-muted-foreground">{API_BASE_URL}</p>
            {status?.mock && (
              <p className="text-muted-foreground">
                Server mock: {status.mock.scenario} · seed {status.mock.seed} (set on the API server)
              </p>
            )}
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}

function StatusBar() {
  const { status, receivedAt } = useData();
  const { paused, setPaused } = useUiStore();
  const state = useLiveState();
  const historyLabel = useHistoryLabel();
  const fmt = (iso: string | null | undefined) =>
    iso ? new Date(iso).toLocaleTimeString(undefined, { hour12: false }) : "—";

  return (
    <footer className="flex h-9 shrink-0 items-center gap-4 border-t border-border/60 px-3 text-xs text-muted-foreground">
      {historyLabel ? (
        <span className="tabular font-medium text-amber-200">History window {historyLabel}</span>
      ) : (
        <>
          <Button
            size="xs"
            variant={paused ? "default" : "outline"}
            onClick={() => setPaused(!paused)}
            aria-pressed={paused}
          >
            {paused ? <Play /> : <Pause />}
            {paused ? "Resume" : "Pause"}
          </Button>
          <span className="tabular">
            Window {fmt(status?.last_aggregate_at && new Date(Date.parse(status.last_aggregate_at) - status.window_seconds * 1000).toISOString())}
            –{fmt(status?.last_aggregate_at)} ({status?.window_seconds ?? "?"} s)
          </span>
        </>
      )}
      <span className="tabular max-sm:hidden">
        Collector: {status?.collector.status ?? "—"} · last data {fmt(status?.collector.last_datagram_at)}
      </span>
      {state === "stale" && !historyLabel && (
        <span className="font-medium text-red-300">Data is stale — showing last received window</span>
      )}
      <div className="flex-1" />
      <span className="max-lg:hidden">≈ = sampled estimate · ctr = interface counter</span>
      {status?.mock && (
        <span className="tabular max-md:hidden">
          {status.mock.scenario} · seed {status.mock.seed} · {status.mock.speed}×
        </span>
      )}
      <span className="sr-only">Last update received {receivedAt ? new Date(receivedAt).toISOString() : "never"}</span>
    </footer>
  );
}
