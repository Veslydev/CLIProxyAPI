"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { CopyBlock } from "@/components/copy-block";
import { LazyDashboardMiniCharts } from "@/components/lazy-dashboard-mini-charts";
import { readNative, type State } from "@native/api";

// Keep the upstream Quick Start cards and integration layout. Only its server
// data loader changes: no Prisma users, plaintext stored client keys, or config
// subscription database is created by this native mode.
export default function QuickStartPage() {
  const t = useTranslations("native");
  const nav = useTranslations("dashboard");
  const [state, setState] = useState<State>();
  const [error, setError] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    void readNative<State>("/state", { signal: controller.signal }).then((s) => { if (!controller.signal.aborted) setState(s); }).catch(() => { if (!controller.signal.aborted) setError(true); });
    return () => controller.abort();
  }, []);
  const statusCards = [
    { label: t("service"), value: state ? t("online") : error ? t("offline") : t("loading"), tone: state ? "text-emerald-600" : "text-rose-600", icon: "●", iconTone: "text-emerald-700" },
    { label: t("providers"), value: state ? new Set(state.accounts.filter((a) => !a.paused && !a.replaced_by && a.health !== "missing").map((a) => a.provider)).size : "—", tone: "text-[var(--text-primary)]", icon: "◆", iconTone: "text-blue-600" },
    { label: t("keys"), value: state?.keys.filter((k) => !k.revoked).length ?? "—", tone: "text-[var(--text-primary)]", icon: "♟", iconTone: "text-amber-700" },
    { label: t("proxy"), value: window.location.origin, tone: "text-[var(--text-primary)]", icon: "◈", iconTone: "text-[var(--text-secondary)]", truncate: true },
  ];
  return <div className="space-y-4">
    <section className="rounded-lg border border-[var(--surface-border)] bg-[var(--surface-base)] p-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div><h1 className="text-xl font-semibold tracking-tight text-[var(--text-primary)]">{t("quickStart")}</h1><p className="mt-1 text-sm text-[var(--text-muted)]">{t("quickHint")}</p></div>
        <div className="flex flex-wrap gap-2">{[["providers", "navProviders"], ["api-keys", "navApiKeys"], ["settings", "navSettings"]].map(([path, label]) => <Link key={path} href={`/dashboard/${path}`} className="rounded-md border border-[var(--surface-border)]/80 bg-[var(--surface-muted)]/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-[var(--text-primary)] transition-colors hover:bg-[var(--surface-hover)]/80">{nav(label)}</Link>)}</div>
      </div>
    </section>
    {error && <p role="alert" className="text-sm text-rose-600">{t("error")}</p>}
    <section id="overview" className="scroll-mt-24"><div className="grid grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-2">
      {statusCards.map((card) => <div key={card.label} className="glass-card rounded-md border border-[var(--surface-border)] px-2.5 py-2 transition-colors hover:border-[var(--surface-border)]">
        <div className="flex items-center justify-between"><div className="text-[11px] font-semibold uppercase tracking-[0.12em] text-[var(--text-muted)]">{card.label}</div><span className={`text-xs ${card.iconTone}`} aria-hidden="true">{card.icon}</span></div>
        <div className={`mt-0.5 text-xs font-semibold ${card.tone} ${card.truncate ? "truncate" : ""}`} title={String(card.value)}>{card.value}</div>
      </div>)}
    </div></section>
    <LazyDashboardMiniCharts />
    <section id="integrations" className="scroll-mt-24"><details className="group/details rounded-lg border border-[var(--surface-border)] bg-[var(--surface-base)]"><summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3"><div><p className="text-sm font-semibold text-[var(--text-primary)]">{t("integration")}</p><p className="text-xs text-[var(--text-muted)]">{t("integrationHint")}</p></div></summary><div className="border-t border-[var(--surface-border)] px-4 py-3"><CopyBlock code={`export ANTHROPIC_BASE_URL="${window.location.origin}"\nexport ANTHROPIC_AUTH_TOKEN="your-api-key"\nclaude`} /></div></details></section>
  </div>;
}
