import { EChartsView } from "@/components/charts/EChartsView";
import type { Account, Schedule } from "./api";
import { warmerPoints } from "./warmerPoints";

export function WarmerTimeline({
  schedules,
  accounts,
}: {
  schedules: Schedule[];
  accounts: Account[];
}) {
  if (!schedules.length) return <p>No eligible warm-up schedules.</p>;
  return (
    <EChartsView
      ariaLabel="Pool warm-up schedule timeline"
      style={{ height: Math.max(220, schedules.length * 34) }}
      option={{
        tooltip: { trigger: "item" },
        legend: { data: ["Last probe", "Next probe"] },
        grid: { left: 150, right: 24, top: 40, bottom: 40 },
        xAxis: { type: "time" },
        yAxis: {
          type: "category",
          data: schedules.map(
            (s) => accounts.find((a) => a.id === s.account)?.label ?? s.account,
          ),
        },
        series: [
          {
            name: "Last probe",
            type: "line",
            lineStyle: { opacity: 0 },
            symbolSize: 10,
            symbol: "circle",
            data: warmerPoints(schedules, "last"),
          },
          {
            name: "Next probe",
            type: "line",
            lineStyle: { opacity: 0 },
            symbolSize: 10,
            symbol: "diamond",
            data: warmerPoints(schedules, "next"),
          },
        ],
      }}
    />
  );
}
