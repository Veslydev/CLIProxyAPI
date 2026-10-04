import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { AccountProviderTabs } from "@/features/accounts/components/AccountProviderTabs";
import { AccountMetricsGrid } from "@/features/accounts/components/AccountMetricsGrid";
import { PaginationControls } from "@/features/monitoring/components/MonitoringShared";
import { Input } from "@/components/ui/Input";
import { Select } from "@/components/ui/Select";
import { Button } from "@/components/ui/Button";
import styles from "@/features/accounts/AccountsPage.module.scss";
import {
  accountMetrics,
  emptyAccountFilters,
  filterAccountWorkspace,
  type AccountFilters,
} from "./accountWorkspace";
import type { Account, State } from "./api";

export function AccountWorkspace({
  state,
  provider,
  onProviderChange,
  resolvedTheme,
  children,
}: {
  state: State;
  provider: string;
  onProviderChange: (provider: string) => void;
  resolvedTheme: "light" | "dark";
  children: (rows: Account[]) => ReactNode;
}) {
  const { t } = useTranslation();
  const [filters, setFilters] = useState(emptyAccountFilters);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (typeof window === "undefined") return;
    const timer = window.setInterval(() => setNow(Date.now()), 5000);
    return () => window.clearInterval(timer);
  }, []);
  const edit = (next: AccountFilters) => {
    setFilters(next);
    setPage(1);
  };
  const accounts = state.accounts.filter(
    (a) => provider === "all" || a.provider === provider,
  );
  const rows = filterAccountWorkspace(state, provider, filters, now);
  const totalPages = Math.max(1, Math.ceil(rows.length / pageSize));
  const currentPage = Math.min(page, totalPages);
  const start = (currentPage - 1) * pageSize;
  const options = (values: string[], label: string) => [
    { value: "", label },
    ...Array.from(new Set(values.filter(Boolean)))
      .sort()
      .map((value) => ({ value, label: value })),
  ];
  return (
    <div className={styles.container}>
      <AccountMetricsGrid metrics={accountMetrics(accounts, state, now)} />
      <AccountProviderTabs
        rows={state.accounts}
        value={provider}
        onChange={(value) => {
          onProviderChange(value);
          setPage(1);
        }}
        resolvedTheme={resolvedTheme}
      />
      <div className={styles.toolbar}>
        <Input
          label="Search accounts"
          className={styles.toolbarSearchInput}
          value={filters.search}
          onChange={(e) => edit({ ...filters, search: e.target.value })}
          placeholder="Name, logical ID, workspace or plan"
        />
        <Select
          ariaLabel="Account status"
          value={filters.health}
          onChange={(health) => edit({ ...filters, health })}
          options={[
            { value: "all", label: "All status" },
            { value: "available", label: "Available" },
            { value: "disabled", label: "Paused / disabled" },
            { value: "problem", label: "Health problem" },
            { value: "low", label: "Low quota" },
            { value: "exhausted", label: "Exhausted" },
            { value: "unconfirmed", label: "Unknown / stale quota" },
            { value: "replaced", label: "Replaced imports" },
          ]}
          triggerClassName={styles.toolbarSelectTrigger}
        />
        <Select
          ariaLabel="Account plan"
          value={filters.plan}
          onChange={(plan) => edit({ ...filters, plan })}
          options={options(
            accounts.map((a) => a.plan || ""),
            "All plans",
          )}
          triggerClassName={styles.toolbarSelectTrigger}
        />
        <Select
          ariaLabel="Account pool"
          value={filters.pool}
          onChange={(pool) => edit({ ...filters, pool })}
          options={[
            { value: "", label: "All pools" },
            ...state.pools
              .filter((p) => provider === "all" || p.provider === provider)
              .map((p) => ({ value: p.id, label: p.name })),
          ]}
          triggerClassName={styles.toolbarSelectTrigger}
        />
        <Select
          ariaLabel="Account sort"
          value={filters.sort}
          onChange={(sort) => edit({ ...filters, sort })}
          options={[
            { value: "name", label: "Name" },
            { value: "quota", label: "Lowest quota first" },
            { value: "reset", label: "Nearest reset" },
            { value: "recent", label: "Recent traffic" },
            { value: "requests", label: "Most requests" },
          ]}
          triggerClassName={styles.toolbarSelectTrigger}
        />
        <Button variant="secondary" onClick={() => edit(emptyAccountFilters)}>
          Clear filters
        </Button>
      </div>
      {children(rows.slice(start, start + pageSize))}
      <PaginationControls
        count={rows.length}
        currentPage={currentPage}
        totalPages={totalPages}
        startItem={rows.length ? start + 1 : 0}
        endItem={Math.min(start + pageSize, rows.length)}
        pageSize={pageSize}
        pageSizeOptions={[25, 50, 100]}
        onPageChange={setPage}
        onPageSizeChange={(size) => {
          setPageSize(size);
          setPage(1);
        }}
        t={t}
      />
    </div>
  );
}
