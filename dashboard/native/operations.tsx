import { useTranslations } from "./intl";
import { Button } from "../src/components/ui/button";

export function Operations() {
  const t = useTranslations("native");
  return <section className="space-y-4 rounded-lg border border-[var(--surface-border)] bg-[var(--surface-base)] p-4">
    <h1 className="text-xl font-semibold text-[var(--text-primary)]">{t("operations")}</h1>
    <p className="text-sm text-[var(--text-muted)]">{t("operationsHint")}</p>
    <a href="/control-plane" target="_blank" rel="noopener noreferrer"><Button>{t("openOperations")}</Button></a>
  </section>;
}
export function NativeSettings() {
  const t = useTranslations("native");
  return <div className="space-y-4">
    <section className="rounded-lg border border-[var(--surface-border)] bg-[var(--surface-base)] p-4">
      <h1 className="text-xl font-semibold text-[var(--text-primary)]">{t("settings")}</h1>
      <p className="mt-2 text-sm text-[var(--text-muted)]">{t("mode")}</p>
      <p className="mt-2 text-sm text-[var(--text-muted)]">{t("rotate")}</p>
    </section>
    <Operations />
  </div>;
}
