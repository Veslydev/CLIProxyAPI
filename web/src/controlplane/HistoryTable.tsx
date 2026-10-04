const columns: Record<string, string[]> = {
  quota: [
    "observed_at",
    "provider",
    "account",
    "key",
    "model",
    "used_percent",
    "remaining",
    "reset_at",
    "source",
  ],
  decisions: [
    "at",
    "account",
    "pool",
    "api_key",
    "model",
    "strategy",
    "reason",
    "remaining_percent",
    "in_flight",
  ],
  warm: ["at", "account", "reason", "result"],
  audit: ["at", "action", "target"],
};

function object(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function cell(value: unknown, field: string): string {
  if (value === undefined || value === null || value === "") return "—";
  if (["at", "observed_at", "reset_at"].includes(field)) {
    const date =
      typeof value === "number"
        ? new Date(value * 1000)
        : typeof value === "string"
          ? new Date(value)
          : undefined;
    if (date && Number.isFinite(date.getTime()))
      return date.getUTCFullYear() <= 1 ? "—" : date.toISOString();
  }
  if (typeof value === "number" && field.endsWith("percent"))
    return `${value.toFixed(1)}%`;
  return typeof value === "object" ? JSON.stringify(value) : String(value);
}

// Keep the full redacted evidence available without making JSON the primary UI.
export function HistoryTable({
  kind,
  rows,
}: {
  kind: string;
  rows: unknown[];
}) {
  const fields = columns[kind] ?? [];
  if (!rows.length) return <p>No matching history.</p>;
  return (
    <div className="cp-table">
      <table>
        <thead>
          <tr>
            {fields.map((field) => (
            <th key={field}>{field.replace(/_/g, " ")}</th>
            ))}
            <th>Evidence</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((value, index) => {
            const row = object(value);
            return (
              <tr key={index}>
                {fields.map((field) => (
                  <td key={field}>{cell(row[field], field)}</td>
                ))}
                <td>
                  <details>
                    <summary>Inspect</summary>
                    <pre className="cp-history">
                      {JSON.stringify(value, null, 2)}
                    </pre>
                  </details>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
