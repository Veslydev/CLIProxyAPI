export interface RuntimeInfo {
  admin_auth: string;
  admin_secret_source: string;
  admin_secret_min_bytes: number;
  secret_rotation: string;
  tls_enabled: boolean;
  database: string;
  dashboard: string;
  retention_days: number;
  oauth_secrets_in_database: boolean;
  home_enabled: boolean;
}

export function isRuntimeInfo(value: unknown): value is RuntimeInfo {
  if (!value || typeof value !== "object") return false;
  const row = value as Record<string, unknown>;
  return (
    [
      "admin_auth",
      "admin_secret_source",
      "secret_rotation",
      "database",
      "dashboard",
    ].every((k) => typeof row[k] === "string") &&
    ["tls_enabled", "oauth_secrets_in_database", "home_enabled"].every(
      (k) => typeof row[k] === "boolean",
    ) &&
    ["admin_secret_min_bytes", "retention_days"].every(
      (k) => typeof row[k] === "number" && Number.isFinite(row[k]),
    )
  );
}
