"use client";

import dynamic from "next/dynamic";

const DashboardMiniCharts = dynamic<Record<string, never>>(
  () => import("@/components/dashboard-mini-charts").then(mod => ({ default: mod.DashboardMiniCharts })),
  { ssr: false, loading: () => <div className="h-40 animate-pulse rounded-lg bg-[var(--surface-muted)]" /> }
);

export function LazyDashboardMiniCharts() {
  return <DashboardMiniCharts />;
}
