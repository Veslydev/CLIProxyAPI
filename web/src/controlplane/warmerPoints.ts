import type { Schedule } from "./api";

export function warmerPoints(schedules: Schedule[], field: "next" | "last") {
  return schedules.flatMap((schedule, index) => {
    if (
      field === "next" &&
      (schedule.active === false || schedule.suspended || schedule.claimed)
    )
      return [];
    const at = Date.parse(schedule[field]);
    return Number.isFinite(at) && at > 0 ? [[at, index]] : [];
  });
}
