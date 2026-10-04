import { Suspense } from "react";
import { AppShell } from "@/components/shell/app-shell";
import { DataRoot } from "@/lib/data/data-context";

/**
 * Shared by /globe and /home so the data provider (and its live clock)
 * persists across view switches.
 */
export default function ViewsLayout({ children }: { children: React.ReactNode }) {
  return (
    <DataRoot>
      <Suspense fallback={<div className="h-dvh bg-background" />}>
        <AppShell>{children}</AppShell>
      </Suspense>
    </DataRoot>
  );
}
