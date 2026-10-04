/**
 * View-local UI state that does not belong in the shareable URL.
 * Investigation context (filters/selection) lives in the URL — see context.ts.
 */
import { create } from "zustand";
import { isScenarioName, type ScenarioName } from "@/lib/mock-backend/scenarios";

export type MotionPreference = "system" | "reduce" | "full";

function envNumber(value: string | undefined, fallback: number): number {
  const n = Number(value);
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

const envScenario = process.env.NEXT_PUBLIC_MOCK_SCENARIO ?? "default";

export const INITIAL_MOCK_CONFIG = {
  seed: envNumber(process.env.NEXT_PUBLIC_MOCK_SEED, 42),
  scenario: (isScenarioName(envScenario) ? envScenario : "default") as ScenarioName,
  speed: envNumber(process.env.NEXT_PUBLIC_MOCK_SPEED, 1),
};

interface UiState {
  leftOpen: boolean;
  rightOpen: boolean;
  paused: boolean;
  particles: boolean;
  motion: MotionPreference;
  mock: typeof INITIAL_MOCK_CONFIG;
  /** Timeline: visible range (seconds) and replay (D-059). */
  timelineRange: number;
  replaying: boolean;
  replaySpeed: number;
  setTimelineRange: (seconds: number) => void;
  setReplaying: (on: boolean) => void;
  setReplaySpeed: (speed: number) => void;
  setLeftOpen: (open: boolean) => void;
  setRightOpen: (open: boolean) => void;
  setPaused: (paused: boolean) => void;
  setParticles: (on: boolean) => void;
  setMotion: (m: MotionPreference) => void;
  setScenario: (s: ScenarioName) => void;
  setSeed: (seed: number) => void;
}

export const useUiStore = create<UiState>((set) => ({
  leftOpen: true,
  rightOpen: true,
  paused: false,
  particles: true,
  motion: "system",
  mock: INITIAL_MOCK_CONFIG,
  timelineRange: 3600,
  replaying: false,
  replaySpeed: 1,
  setTimelineRange: (timelineRange) => set({ timelineRange }),
  setReplaying: (replaying) => set({ replaying }),
  setReplaySpeed: (replaySpeed) => set({ replaySpeed }),
  setLeftOpen: (leftOpen) => set({ leftOpen }),
  setRightOpen: (rightOpen) => set({ rightOpen }),
  setPaused: (paused) => set({ paused }),
  setParticles: (particles) => set({ particles }),
  setMotion: (motion) => set({ motion }),
  setScenario: (scenario) => set((s) => ({ mock: { ...s.mock, scenario } })),
  setSeed: (seed) => set((s) => ({ mock: { ...s.mock, seed } })),
}));
