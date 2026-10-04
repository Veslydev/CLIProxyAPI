import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { dashboardFetch, hasSession, readNative, signOut } from "./api";
import { emptyState } from "./fixtures.test-support";

const fetchMock = vi.fn<typeof fetch>();
const response = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status });
async function login() {
  fetchMock.mockResolvedValueOnce(response(emptyState()));
  expect((await dashboardFetch("/api/auth/login", { method: "POST", body: JSON.stringify({ password: "fixture-admin-secret" }) })).status).toBe(200);
}
beforeEach(() => {
  vi.stubGlobal("window", { fetch: fetchMock, location: { origin: "https://fixture.invalid" }, dispatchEvent: vi.fn() });
  fetchMock.mockReset();
});
afterEach(() => { signOut(); vi.unstubAllGlobals(); });

describe("native dashboard API boundary", () => {
  it("rejects unauthenticated operations, external origins and unavailable external-stack writes", async () => {
    expect((await dashboardFetch("/api/user/api-keys")).status).toBe(401);
    expect((await dashboardFetch("https://other.invalid/api/user/api-keys")).status).toBe(400);
    await login(); fetchMock.mockClear();
    expect((await dashboardFetch("/api/restart", { method: "POST" })).status).toBe(501);
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it("never creates an unscoped key or returns plaintext from listing", async () => {
    await login(); fetchMock.mockClear();
    expect((await dashboardFetch("/api/user/api-keys", { method: "POST", body: JSON.stringify({ name: "bad" }) })).status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
    fetchMock.mockResolvedValueOnce(response({ key: { id: "k" }, secret: "fixture-once" }));
    const result = await dashboardFetch("/api/user/api-keys", { method: "POST", body: JSON.stringify({ name: "good", bindings: { codex: { accounts: ["a"], pools: [] } } }) });
    expect(await result.json()).toEqual({ id: "k", key: "fixture-once" });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/control-plane/keys");
    expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer fixture-admin-secret");
    expect(String(url)).not.toContain("fixture-admin-secret");
    fetchMock.mockResolvedValueOnce(response(emptyState()));
    expect(await (await dashboardFetch("/api/user/api-keys")).json()).toEqual({ apiKeys: [] });
  });
  it("discards a delayed state body after sign-out", async () => {
    await login();
    let release!: (value: unknown) => void;
    fetchMock.mockResolvedValueOnce({ ok: true, json: () => new Promise((resolve) => { release = resolve; }) } as Response);
    const pending = readNative("/state");
    await vi.waitFor(() => expect(release).toBeTypeOf("function"));
    signOut(); release(emptyState());
    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    expect(hasSession()).toBe(false);
  });
  it("cancels a late OAuth session acquired after sign-out using its captured credential", async () => {
    await login();
    let release!: (value: Response) => void;
    fetchMock.mockImplementationOnce(() => new Promise((resolve) => { release = resolve; }));
    const pending = dashboardFetch("/api/management/antigravity-auth-url?is_webui=true");
    await vi.waitFor(() => expect(release).toBeTypeOf("function"));
    signOut();
    fetchMock.mockResolvedValueOnce(response({ cancelled: true }));
    release(response({ state: "late-fixture", url: "https://accounts.google.com/auth" }));
    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    expect(fetchMock.mock.calls.at(-1)?.[0]).toBe("/api/control-plane/oauth/session?state=late-fixture");
    expect(new Headers(fetchMock.mock.calls.at(-1)?.[1]?.headers).get("Authorization")).toBe("Bearer fixture-admin-secret");
  });
  it("rejects unsafe native OAuth links and cancels the waiter", async () => {
    await login();
    fetchMock.mockResolvedValueOnce(response({ state: "unsafe", url: "javascript:alert(1)" }));
    fetchMock.mockResolvedValueOnce(response({ cancelled: true }));
    expect((await dashboardFetch("/api/management/codex-auth-url")).status).toBe(400);
    expect(fetchMock.mock.calls.at(-1)?.[1]?.method).toBe("DELETE");
  });
  it("cancels the waiter even when its launch URL cannot be parsed", async () => {
    await login();
    fetchMock.mockResolvedValueOnce(response({ state: "malformed", url: "not a URL" }));
    fetchMock.mockResolvedValueOnce(response({ cancelled: true }));
    expect((await dashboardFetch("/api/management/codex-auth-url")).status).toBe(400);
    expect(fetchMock.mock.calls.at(-1)?.[0]).toBe("/api/control-plane/oauth/session?state=malformed");
  });
  it("returns disabled logging as an explicit unavailable state without fetching a log file", async () => {
    await login(); fetchMock.mockClear();
    fetchMock.mockResolvedValueOnce(response({ "logging-to-file": false }));
    const data = await (await dashboardFetch("/api/management/logs?limit=200")).json();
    expect(data).toMatchObject({ disabled: true, lines: [] });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
  it("maps native device flow without a fabricated ownership-claim write", async () => {
    await login();
    fetchMock.mockResolvedValueOnce(response({ state: "device", url: "https://auth.example.invalid/device", user_code: "fixture-code", flow: "device" }));
    const data = await (await dashboardFetch("/api/management/xai-auth-url")).json();
    expect(data.method).toBe("device_code");
    fetchMock.mockResolvedValueOnce(response({ status: "ok" }));
    expect((await dashboardFetch("/api/management/get-auth-status?state=device")).status).toBe(200);
    expect(fetchMock.mock.calls.at(-1)?.[0]).toBe("/api/control-plane/oauth/status?state=device");
  });
  it("rejects a credential import whose type differs from its selected provider", async () => {
    await login(); fetchMock.mockClear();
    const result = await dashboardFetch("/api/providers/oauth/import", { method: "POST", body: JSON.stringify({ provider: "codex", fileName: "fixture.json", fileContent: JSON.stringify({ type: "claude" }) }) });
    expect(result.status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it("sends validated credential imports to native multipart upload", async () => {
    await login(); fetchMock.mockClear();
    fetchMock.mockResolvedValueOnce(response({ status: "ok" }));
    const result = await dashboardFetch("/api/providers/oauth/import", { method: "POST", body: JSON.stringify({ provider: "codex", fileName: "fixture.json", fileContent: JSON.stringify({ type: "codex", token: "fixture-only" }) }) });
    expect(result.status).toBe(200);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/control-plane/native/credentials");
    expect(init?.body).toBeInstanceOf(FormData);
    const file = (init?.body as FormData).get("file") as File;
    expect(file.name).toBe("fixture.json");
    expect(JSON.parse(await file.text()).type).toBe("codex");
    expect(new Headers(init?.headers).has("Content-Type")).toBe(false);
  });
});
