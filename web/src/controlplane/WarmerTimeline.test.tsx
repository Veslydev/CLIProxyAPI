import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { WarmerTimeline } from "./WarmerTimeline";
import { warmerPoints } from "./warmerPoints";
import type { Schedule } from "./api";

vi.mock("@/components/charts/EChartsView", () => ({
  EChartsView: ({ option }: { option: unknown }) => (
    <pre>{JSON.stringify(option)}</pre>
  ),
}));

const schedules: Schedule[] = [
  {
    account: "a",
    pool: "p",
    next: "2026-10-03T01:00:00Z",
    last: "0001-01-01T00:00:00Z",
    reason: "stagger",
    result: "",
    claimed: false,
  },
  {
    account: "b",
    pool: "p",
    next: "invalid",
    last: "2026-10-03T00:00:00Z",
    reason: "reset",
    result: "ok",
    claimed: false,
  },
];

describe("pool warmer timeline", () => {
  it("keeps account positions while excluding unset or invalid timestamps", () => {
    expect(warmerPoints(schedules, "next")).toEqual([
      [Date.parse(schedules[0].next), 0],
    ]);
    expect(warmerPoints(schedules, "last")).toEqual([
      [Date.parse(schedules[1].last), 1],
    ]);
  });
  it("renders the integrated chart and an explicit empty state", () => {
    const html = renderToStaticMarkup(
      <WarmerTimeline schedules={schedules} accounts={[]} />,
    );
    expect(html).toContain("Last probe");
    expect(html).toContain("Next probe");
    expect(
      renderToStaticMarkup(<WarmerTimeline schedules={[]} accounts={[]} />),
    ).toContain("No eligible warm-up schedules");
  });
  it("retains history but does not promise inactive, suspended or claimed probes", () => {
    const rows = [
      { ...schedules[0], active: false, last: schedules[1].last },
      { ...schedules[0], active: true, suspended: "unknown_primary_duration" },
      { ...schedules[0], active: true, claimed: true },
      { ...schedules[0], active: true, effective: false },
    ];
    expect(warmerPoints(rows, "next")).toEqual([
      [Date.parse(schedules[0].next), 3],
    ]);
    expect(warmerPoints(rows, "last")).toEqual([
      [Date.parse(schedules[1].last), 0],
    ]);
  });
});
