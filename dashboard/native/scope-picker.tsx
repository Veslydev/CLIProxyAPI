import { useEffect, useState } from "react";
import { useTranslations } from "./intl";
import { readNative, type State, type Binding } from "./api";

export function ScopePicker({ value, onChange }: { value: Record<string, Binding>; onChange: (next: Record<string, Binding>) => void }) {
  const [state, setState] = useState<State>();
  const [error, setError] = useState(false);
  const t = useTranslations("native");
  useEffect(() => {
    const controller = new AbortController();
    void readNative<State>("/state", { signal: controller.signal }).then((s) => { if (!controller.signal.aborted) setState(s); }).catch(() => { if (!controller.signal.aborted) setError(true); });
    return () => controller.abort();
  }, []);
  const toggle = (provider: string, kind: "accounts" | "pools", id: string, checked: boolean) => {
    const binding = value[provider] ?? { accounts: [], pools: [] };
    onChange({ ...value, [provider]: { ...binding, [kind]: checked ? [...binding[kind], id] : binding[kind].filter((item) => item !== id) } });
  };
  return <fieldset className="space-y-2 rounded-md border border-[var(--surface-border)] p-3">
    <legend className="text-sm font-semibold">{t("scope")}</legend>
    <p className="text-xs text-[var(--text-muted)]">{t("scopeHint")}</p>
    {!state && <p role={error ? "alert" : "status"}>{t(error ? "error" : "loading")}</p>}
    {state && state.accounts.length + state.pools.length === 0 && <p className="text-sm">{t("noAccounts")}</p>}
    {state?.accounts.filter((a) => !a.replaced_by).map((a) => <label key={a.id} className="flex gap-2 text-sm"><input type="checkbox" checked={value[a.provider]?.accounts.includes(a.id) ?? false} onChange={(e) => toggle(a.provider, "accounts", a.id, e.target.checked)} />{a.provider}: {a.label || a.id}</label>)}
    {state?.pools.map((p) => <label key={p.id} className="flex gap-2 text-sm"><input type="checkbox" checked={value[p.provider]?.pools.includes(p.id) ?? false} onChange={(e) => toggle(p.provider, "pools", p.id, e.target.checked)} />{p.provider}: {p.name}</label>)}
  </fieldset>;
}
