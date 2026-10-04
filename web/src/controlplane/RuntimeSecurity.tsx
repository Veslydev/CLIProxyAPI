import { Card } from "@/components/ui/Card";
import type { RuntimeInfo } from "./runtimeInfo";

export function RuntimeSecurity({ runtime }: { runtime: RuntimeInfo }) {
  return (
    <Card title="Authentication, security and deployment">
      <dl>
        <dt>Dashboard authentication</dt>
        <dd>{runtime.admin_auth}</dd>
        <dt>Admin credential source</dt>
        <dd>
          {runtime.admin_secret_source} · minimum{" "}
          {runtime.admin_secret_min_bytes} bytes
        </dd>
        <dt>Credential rotation</dt>
        <dd>{runtime.secret_rotation}</dd>
        <dt>Proxy TLS</dt>
        <dd>
          {runtime.tls_enabled
            ? "Enabled"
            : "Disabled — terminate TLS at a trusted reverse proxy before Internet exposure"}
        </dd>
        <dt>OAuth secret storage</dt>
        <dd>
          {runtime.oauth_secrets_in_database
            ? "Database"
            : "Existing provider auth files, not SQLite"}
        </dd>
        <dt>SQLite database</dt>
        <dd>{runtime.database}</dd>
        <dt>Dashboard bundle</dt>
        <dd>{runtime.dashboard}</dd>
        <dt>Deployment retention default</dt>
        <dd>{runtime.retention_days} days</dd>
        <dt>Home dispatch</dt>
        <dd>{runtime.home_enabled ? "Enabled" : "Disabled"}</dd>
      </dl>
      <p>
        Inference keys cannot administer the dashboard. Admin credentials stay
        in browser memory only; sign out clears the session. TLS and
        admin-secret changes are deployment operations, not browser-side edits.
      </p>
    </Card>
  );
}
