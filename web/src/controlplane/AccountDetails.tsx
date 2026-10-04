import { useEffect, useState } from "react";
import { Drawer } from "@/components/ui/Drawer";
import { SegmentedTabs } from "@/components/ui/SegmentedTabs";
import { Button } from "@/components/ui/Button";
import { QuotaWindowCard } from "@/features/accounts/components/QuotaWindowCard";
import { quotaWindowView } from "./quotaWindowView";
import type { Account, State } from "./api";

export function AccountDetails({
  account,
  state,
  busy,
  onClose,
  mutate,
  onStartOAuth,
}: {
  account: Account;
  state: State;
  busy: boolean;
  onClose: () => void;
  mutate: (path: string, method?: string, body?: unknown) => Promise<unknown>;
  onStartOAuth?: (provider: string, account?: string) => void;
}) {
  const [tab, setTab] = useState<"overview" | "quota" | "reauth">("overview");
  const [source, setSource] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [message, setMessage] = useState("");
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (typeof window === "undefined") return;
    const timer = window.setInterval(() => setNow(Date.now()), 5000);
    return () => window.clearInterval(timer);
  }, []);
  const replacements = state.accounts.filter(
    (a) =>
      a.provider === account.provider &&
      a.id !== account.id &&
      !a.replaced_by &&
      a.identity_evidence === "credential_reference" &&
      a.paused &&
      !a.warm_enabled &&
      !a.requests,
  );
  const schedules = state.schedules.filter((s) => s.account === account.id);
  return (
    <Drawer
      open
      title={`${account.label} · ${account.provider}`}
      onClose={onClose}
      width={820}
    >
      <div className="cp-app">
        <SegmentedTabs
          items={[
            { id: "overview", label: "Overview" },
            { id: "quota", label: "Quota windows" },
            { id: "reauth", label: "Re-auth continuity" },
          ]}
          activeTab={tab}
          onChange={setTab}
          ariaLabel="Account detail views"
          idBase="control-plane-account-details"
        />
        {tab === "overview" && (
          <>
            <dl>
              <dt>Logical account ID</dt>
              <dd>{account.id}</dd>
              <dt>Identity evidence</dt>
              <dd>{account.identity_evidence || "Unknown"}</dd>
              <dt>Workspace / plan</dt>
              <dd>
                {account.workspace || "Unknown"} / {account.plan || "Unknown"}
              </dd>
              <dt>Health</dt>
              <dd>{account.paused ? "Paused" : account.health}</dd>
              <dt>Requests / failures</dt>
              <dd>
                {account.requests ?? 0} / {account.failures ?? 0}
              </dd>
              <dt>Assigned pools</dt>
              <dd>
                {state.pools
                  .filter((p) => p.accounts.includes(account.id))
                  .map((p) => p.name)
                  .join(", ") || "None"}
              </dd>
            </dl>
            <p>Warm-up: {account.warm_enabled ? "Enabled" : "Disabled"}</p>
            {schedules.map((s) => (
              <p key={s.pool}>
                Pool {s.pool} · next {s.next} · last {s.last} ·{" "}
                {s.result || "No probe yet"} · {s.suspended || (s.effective ? "Next eligible policy" : "Retained pool policy")}
              </p>
            ))}
            <div className="cp-actions">
              <Button
                disabled={busy || !!account.replaced_by}
                onClick={() =>
                  void mutate(`/accounts/${account.id}`, "PUT", {
                    ...account,
                    paused: !account.paused,
                  })
                }
              >
                {account.paused ? "Resume account" : "Pause account"}
              </Button>
              <Button
                disabled={busy || !!account.replaced_by}
                onClick={() =>
                  void mutate(`/accounts/${account.id}`, "PUT", {
                    ...account,
                    warm_enabled: !account.warm_enabled,
                  })
                }
              >
                {account.warm_enabled ? "Disable warm-up" : "Enable warm-up"}
              </Button>
            </div>
          </>
        )}
        {tab === "quota" && (
          <>
            <Button
              disabled={busy || !!account.replaced_by}
              onClick={() => void mutate(`/accounts/${account.id}/quota`)}
            >
              Refresh native quota
            </Button>
            {state.quotas
              .filter((q) => q.account === account.id)
              .map((q) => (
                <QuotaWindowCard
                  key={q.key}
                  window={quotaWindowView(
                    q,
                    now,
                    state.settings.quota_fresh_seconds,
                  )}
                  variant="drawer"
                  locale="en"
                />
              ))}
            {!state.quotas.some((q) => q.account === account.id) && (
              <p>No provider quota evidence. Unknown is not unlimited.</p>
            )}
          </>
        )}
        {tab === "reauth" && (
          <>
            {onStartOAuth && ["codex", "claude"].includes(account.provider) && (
              <Button
                disabled={busy || !!account.replaced_by}
                onClick={() => onStartOAuth(account.provider, account.identity_evidence === "provider" ? account.id : undefined)}
              >
                Start provider re-auth
              </Button>
            )}
            <p>
              Normal Codex/Claude OAuth sign-in automatically keeps this logical
              account when provider subject/workspace matches. A different
              verified principal never inherits its scope.
              Targeted re-auth rejects a mismatched or missing native principal before credential persistence. Identity-less sign-in is separate and still needs the audited confirmation below.
            </p>
            <p>
              If an old credential has no stable provider identity, sign in
              again, pause the newly imported account, and verify that it is the
              same upstream account. Only then attach the unused replacement
              below. Existing pools, limits, health and continuation ownership
              remain on this logical account. Replacement bindings or usage are
              never merged.
            </p>
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                setMessage("");
                const result = await mutate(
                  `/accounts/${account.id}/reassociate`,
                  "POST",
                  { source, confirmation },
                );
                setMessage(
                  result
                    ? "Replacement attached; logical account metadata preserved."
                    : "Attachment rejected. Check confirmation and replacement eligibility.",
                );
                if (result) {
                  setSource("");
                  setConfirmation("");
                }
              }}
            >
              <label>
                Paused unused replacement
                <select
                  value={source}
                  onChange={(e) => setSource(e.target.value)}
                  required
                >
                  <option value="">Select verified replacement</option>
                  {replacements.map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.label} · {a.id}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Confirm existing logical account ID
                <input
                  value={confirmation}
                  onChange={(e) => setConfirmation(e.target.value)}
                  autoComplete="off"
                  required
                />
              </label>
              <Button
                type="submit"
                disabled={
                  busy ||
                  !source ||
                  confirmation !== account.id ||
                  !!account.replaced_by
                }
              >
                Attach replacement credential
              </Button>
              {!replacements.length && (
                <p>
                  No eligible replacement. Used, bound, warmed or
                  provider-identified accounts cannot be attached.
                </p>
              )}
              {message && <p role="status">{message}</p>}
            </form>
          </>
        )}
      </div>
    </Drawer>
  );
}
