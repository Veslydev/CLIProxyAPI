import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { HistoryTable } from "./HistoryTable";

describe("history evidence tables", () => {
  it("renders audit timestamps and escaped evidence", () => {
    const html = renderToStaticMarkup(
      <HistoryTable
        kind="audit"
        rows={[{ at: 1790985600, action: "pool.update", target: "<script>" }]}
      />,
    );
    expect(html).toContain("2026-10-03T00:00:00.000Z");
    expect(html).toContain("pool.update");
    expect(html).toContain("&lt;script&gt;");
    expect(html).not.toContain("<script>");
    expect(html).toContain("<details>");
  });
  it("shows quota percentages and hides unset resets", () => {
    const html = renderToStaticMarkup(
      <HistoryTable
        kind="quota"
        rows={[
          {
            used_percent: 42.25,
            reset_at: "0001-01-01T00:00:00Z",
            source: "native",
          },
        ]}
      />,
    );
    expect(html).toContain("42.3%");
    expect(html).toContain("<td>—</td>");
    expect(html).toContain("native");
  });
  it("handles empty and malformed rows without crashing", () => {
    expect(
      renderToStaticMarkup(<HistoryTable kind="warm" rows={[]} />),
    ).toContain("No matching history");
    expect(
      renderToStaticMarkup(
        <HistoryTable kind="decisions" rows={[null, [1], "legacy"]} />,
      ),
    ).toContain("legacy");
  });
});
