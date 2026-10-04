import { API_ENDPOINTS as E } from "../src/lib/api-endpoints";
import type { Account, APIKey, Binding, State } from "../../web/src/controlplane/api";
import { quotaProjection, usageProjection, type AnalyticsPage } from "./projections";

// A browser session is memory-only. Credentials never enter URLs, cookies,
// localStorage, SWR keys, or the imported dashboard's PostgreSQL auth model.
let credential = "";
let epoch = 0;
let lifetime = new AbortController();
const sessions = new Map<string, string>();
export const hasSession = () => credential !== "";
export const sessionVersion = () => credential ? epoch + 1 : -epoch - 1;
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
const signalFor = (signal?: AbortSignal | null) => signal ? AbortSignal.any([signal, lifetime.signal]) : lifetime.signal;

export async function nativeRequest(path: string, init: RequestInit = {}) {
  const captured = epoch;
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${credential}`);
  if (typeof init.body === "string") headers.set("Content-Type", "application/json");
  const response = await window.fetch(`/api/control-plane${path}`, { ...init, headers, signal: signalFor(init.signal), cache: "no-store" });
  if (captured !== epoch) throw new DOMException("Session ended", "AbortError");
  return response;
}
export async function readNative<T>(path: string, init?: RequestInit): Promise<T> {
  const captured = epoch;
  const response = await nativeRequest(path, init);
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  const data = await response.json() as T;
  if (captured !== epoch) throw new DOMException("Session ended", "AbortError");
  return data;
}
export async function cancelOAuth(state?: string) {
  const targets = state ? [[state, sessions.get(state)]] : [...sessions];
  await Promise.all(targets.map(async ([id, secret]) => {
    if (!secret || !id) return;
    sessions.delete(id);
    // This deliberately uses the captured credential after local sign-out.
    await window.fetch(`/api/control-plane/oauth/session?state=${encodeURIComponent(id)}`, { method: "DELETE", headers: { Authorization: `Bearer ${secret}` }, keepalive: true }).catch(() => undefined);
  }));
}
export function signOut() {
  void cancelOAuth();
  lifetime.abort(); lifetime = new AbortController(); credential = ""; epoch++;
  window.dispatchEvent(new Event("dashboard:session"));
}
const bodyObject = (init: RequestInit): Record<string, unknown> => {
  const value: unknown = typeof init.body === "string" ? JSON.parse(init.body) : {};
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid body");
  return value as Record<string, unknown>;
};
const encode = encodeURIComponent;
const send = (path: string, method: string, body?: unknown, signal?: AbortSignal | null) => nativeRequest(path, { method, signal, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });

export async function dashboardFetch(input: string, init: RequestInit = {}): Promise<Response> {
  const captured = epoch;
  const response = await translate(input, init);
  if (captured !== epoch && input !== E.AUTH.LOGIN && input !== E.AUTH.LOGOUT) throw new DOMException("Session ended", "AbortError");
  return response;
}
async function translate(input: string, init: RequestInit): Promise<Response> {
  const url = new URL(input, window.location.origin);
  if (url.origin !== window.location.origin || !url.pathname.startsWith("/api/")) return json({ error: "External API rejected" }, 400);
  const path = url.pathname;
  const method = init.method?.toUpperCase() ?? "GET";
  const captured = epoch;
  if (path === E.SETUP.BASE && method === "GET") return json({ setupRequired: false });
  if (path === E.AUTH.LOGIN && method === "POST") {
    const body = bodyObject(init);
    const secret = typeof body.password === "string" ? body.password : "";
    const response = await window.fetch("/api/control-plane/state", { headers: { Authorization: `Bearer ${secret}` }, signal: signalFor(init.signal), cache: "no-store" });
    if (captured !== epoch) throw new DOMException("Session ended", "AbortError");
    if (!response.ok) return json({ error: "Invalid admin credential" }, response.status);
    await response.json();
    if (captured !== epoch) throw new DOMException("Session ended", "AbortError");
    lifetime.abort(); lifetime = new AbortController(); epoch++;
    credential = secret;
    window.dispatchEvent(new Event("dashboard:session"));
    return json({ ok: true });
  }
  if (path === E.AUTH.LOGOUT && method === "POST") { signOut(); return json({ ok: true }); }
  if (!credential) return json({ error: "Admin authentication required" }, 401);
  const state = () => readNative<State>("/state", { signal: init.signal });
  if (path.startsWith("/api/control-plane/")) return nativeRequest((path + url.search).slice("/api/control-plane".length), init);
  if (path === E.AUTH.ME && method === "GET") {
    await state(); return json({ id: `admin-${epoch}`, username: "admin", isAdmin: true });
  }
  if (path === "/api/set-locale" && method === "POST") return json({ ok: true });
  if (path === E.PROXY.OAUTH_SETTINGS && method === "GET") return json({ incognitoBrowser: true });
  if (path === E.PROXY.STATUS && method === "GET") {
    const response = await nativeRequest("/runtime", init);
    return response.ok ? json({ running: true }) : response;
  }
  if (path === E.HEALTH && method === "GET") {
    await state(); return json({ status: "ok", database: "connected", proxy: "connected" });
  }
  if (path === E.PROVIDERS.KEYS) return send(`/native/provider-keys${url.search}`, method, method === "POST" ? bodyObject(init) : undefined, init.signal);
  if (path.startsWith(`${E.PROVIDERS.KEYS}/`) && method === "DELETE") return send(`/native/provider-keys/${encode(path.slice(E.PROVIDERS.KEYS.length + 1))}${url.search}`, method);
  if (path === E.PROVIDERS.OAUTH && method === "GET") {
    const data = await state();
    return json({ accounts: data.accounts.filter((a) => !a.replaced_by).map((a) => ({ id: a.id, authId: null, accountName: a.label || a.id, accountEmail: null, provider: a.provider, ownerUsername: null, ownerUserId: null, isOwn: false, status: a.paused ? "disabled" : a.health === "missing" ? "error" : "active", statusMessage: a.health, unavailable: a.health === "missing", quotaGroups: [], canDelete: false, canClaim: false })) });
  }
  if (path.startsWith(`${E.PROVIDERS.OAUTH}/`) && method === "PATCH") {
    const id = decodeURIComponent(path.slice(E.PROVIDERS.OAUTH.length + 1));
    const account = (await state()).accounts.find((a) => a.id === id);
    if (!account) return json({ error: "Unknown account" }, 404);
    return send(`/accounts/${encode(id)}`, "PUT", { ...account, paused: bodyObject(init).disabled === true }, init.signal);
  }
  if (path === E.PROVIDERS.OAUTH_IMPORT && method === "POST") {
    const body = bodyObject(init);
    if (typeof body.fileName !== "string" || typeof body.fileContent !== "string" || typeof body.provider !== "string") return json({ error: "Invalid credential import" }, 400);
    const content: unknown = JSON.parse(body.fileContent);
    if (!content || typeof content !== "object" || Array.isArray(content) || !("type" in content) || content.type !== body.provider) return json({ error: "Credential type must match selected provider" }, 400);
    const form = new FormData();
    form.set("file", new Blob([body.fileContent], { type: "application/json" }), body.fileName);
    return nativeRequest("/native/credentials", { method: "POST", body: form, signal: init.signal });
  }
  const launch = path.match(/^\/api\/management\/([a-z0-9-]+)-auth-url$/);
  if (launch && method === "GET") {
    const provider = launch[1] === "anthropic" ? "claude" : launch[1] === "github" ? "copilot" : launch[1];
    url.searchParams.set("provider", provider);
    const secret = credential;
    // Keep the acquisition response readable after sign-out so a late backend
    // state can be cancelled. Aborting only the HTTP request strands the waiter.
    const response = await window.fetch(`/api/control-plane/oauth/start?${url.searchParams}`, { headers: { Authorization: `Bearer ${secret}` }, signal: AbortSignal.timeout(45000), cache: "no-store" });
    if (!response.ok) return response;
    const data = await response.json() as { state?: string; url?: string; flow?: string; user_code?: string; method?: string };
    if (data.state) sessions.set(data.state, secret);
    if (captured !== epoch) {
      if (data.state) void cancelOAuth(data.state);
      throw new DOMException("Session ended", "AbortError");
    }
    if (data.url) {
      let safe = false;
      try {
        const link = new URL(data.url);
        safe = link.protocol === "https:" && !link.username && !link.password;
      } catch { /* Malformed links still require waiter cancellation. */ }
      if (!safe) {
        if (data.state) await cancelOAuth(data.state);
        return json({ error: "Unsafe OAuth URL rejected" }, 400);
      }
    }
    return json({ ...data, method: data.flow === "device" ? "device_code" : data.method });
  }
  if (path === "/api/management/get-auth-status" && method === "GET") {
    const response = await nativeRequest(`/oauth/status${url.search}`, init);
    if (!response.ok) return response;
    const data = await response.json() as { status?: string };
    if (data.status === "ok" || data.status === "error") sessions.delete(url.searchParams.get("state") ?? "");
    return json(data);
  }
  if (path === E.MANAGEMENT.OAUTH_CALLBACK && method === "POST") {
    const body = bodyObject(init);
    // Device flows save through the native waiter; there is no PostgreSQL ownership claim.
    if (!body.callbackUrl) return nativeRequest(`/oauth/status?state=${encode(String(body.state ?? ""))}`, { signal: init.signal });
    return send("/oauth/callback", "POST", { provider: body.provider, redirect_url: body.callbackUrl }, init.signal);
  }
  if (path === E.USER.API_KEYS && method === "GET") {
    const data = await state();
    return json({ apiKeys: data.keys.filter((k) => !k.revoked).map((k) => ({ id: k.id, name: k.name, keyPreview: k.prefix + "…", createdAt: "", lastUsedAt: validDate(k.last_used) })) });
  }
  if (path === E.USER.API_KEYS && method === "POST") {
    const body = bodyObject(init);
    if (!body.bindings || typeof body.bindings !== "object" || !Object.values(body.bindings).some((b: unknown) => b && typeof b === "object" && (("accounts" in b && Array.isArray(b.accounts) && b.accounts.length) || ("pools" in b && Array.isArray(b.pools) && b.pools.length)))) return json({ error: "Select an explicit account or pool scope" }, 400);
    const response = await send("/keys", "POST", { name: body.name, bindings: body.bindings }, init.signal);
    if (!response.ok) return response;
    const data = await response.json() as { key: APIKey; secret: string };
    return json({ id: data.key.id, key: data.secret });
  }
  if (path === E.USER.API_KEYS && method === "DELETE") return send(`/keys/${encode(url.searchParams.get("id") ?? "")}`, "DELETE", undefined, init.signal);
  if (path === E.QUOTA.BASE && method === "GET") {
    if (url.searchParams.has("bust")) {
      const data = await state();
      for (const account of data.accounts.filter((a) => !a.replaced_by && !a.paused)) {
        const response = await send(`/accounts/${encode(account.id)}/quota`, "POST", undefined, init.signal);
        // Unsupported quota adapters remain unknown, not fabricated capacity.
        if (!response.ok && response.status >= 500) return response;
      }
    }
    return json(quotaProjection(await state()));
  }
  if (path === "/api/usage/history" && method === "GET") {
    const since = url.searchParams.get("from") ?? "";
    const to = url.searchParams.get("to") ?? "";
    const end = to ? new Date(`${to}T00:00:00Z`) : null;
    if (end && !Number.isFinite(end.getTime())) return json({ error: "Invalid range" }, 400);
    end?.setUTCDate(end.getUTCDate() + 1);
    const query = new URLSearchParams({ since, until: end?.toISOString() ?? "", limit: "100" });
    const [analytics, data] = await Promise.all([readNative<AnalyticsPage>(`/analytics/page?${query}`, { signal: init.signal }), state()]);
    return json({ data: usageProjection(analytics, data), isAdmin: true });
  }
  if (path === E.MANAGEMENT.USAGE && method === "GET") {
    const data = await readNative<AnalyticsPage>("/analytics/page?limit=1", { signal: init.signal });
    return json({ usage: { total_requests: data.totals.requests, total_tokens: data.totals.tokens, success_count: data.totals.requests - data.totals.failures, failure_count: data.totals.failures, apis: {} } });
  }
  if (path === E.MANAGEMENT.LOGS && method === "GET") {
    const config = await readNative<{ "logging-to-file": boolean }>("/native/logging-to-file", { signal: init.signal });
    if (!config["logging-to-file"]) return json({ lines: [], "line-count": 0, "latest-timestamp": 0, disabled: true });
    return nativeRequest(`/native/logs${url.search}`, init);
  }
  if (path === E.MANAGEMENT.LOGGING_TO_FILE && ["GET", "PATCH"].includes(method)) return send("/native/logging-to-file", method === "PATCH" ? "PUT" : "GET", method === "PATCH" ? bodyObject(init) : undefined, init.signal);
  return json({ error: "This external-stack feature is not available in Go/SQLite mode. Use the native Operations controls." }, 501);
}

export const validDate = (value: string) => value && !value.startsWith("0001-") ? value : null;
export type { Account, Binding, State };
