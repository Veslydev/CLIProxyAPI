import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { RuntimeSecurity } from "./RuntimeSecurity";
import { isRuntimeInfo, type RuntimeInfo } from "./runtimeInfo";

const runtime: RuntimeInfo = {
  admin_auth: "separate bearer credential",
  admin_secret_source: "CONTROL_PLANE_ADMIN_SECRET",
  admin_secret_min_bytes: 32,
  secret_rotation: "update environment and restart",
  tls_enabled: false,
  database: "/data/state.sqlite",
  dashboard: "/app/dashboard.html",
  retention_days: 30,
  oauth_secrets_in_database: false,
  home_enabled: false,
};
describe("runtime security view", () => {
  it("shows actual deployment constraints without pretending to edit secrets", () => {
    const html = renderToStaticMarkup(<RuntimeSecurity runtime={runtime} />);
    expect(html).toContain("CONTROL_PLANE_ADMIN_SECRET");
    expect(html).toContain("terminate TLS");
    expect(html).toContain("not SQLite");
    expect(html).not.toContain("<input");
  });
  it("rejects malformed runtime payloads", () => {
    expect(isRuntimeInfo(runtime)).toBe(true);
    expect(isRuntimeInfo({ ...runtime, database: { unexpected: true } })).toBe(
      false,
    );
    expect(isRuntimeInfo({ ...runtime, retention_days: NaN })).toBe(false);
    expect(isRuntimeInfo(null)).toBe(false);
  });
});
