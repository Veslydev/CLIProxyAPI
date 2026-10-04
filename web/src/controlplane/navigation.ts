import { useCallback, useState, useSyncExternalStore } from "react";

export const pages = [
  "Overview",
  "Monitoring",
  "Usage Analytics",
  "Accounts",
  "API Keys",
  "Pools",
  "Routing Settings",
  "Settings",
] as const;
export type Page = (typeof pages)[number];
const slugs = [
  "overview",
  "monitoring",
  "usage",
  "accounts",
  "keys",
  "pools",
  "routing",
  "settings",
];

export function pageFromHash(hash: string): Page {
  const slug = hash.replace(/^#\/?/, "").split("?")[0];
  const index = slugs.indexOf(slug);
  return index >= 0 ? pages[index] : "Overview";
}
export function pageHash(page: Page): string {
  return `#/${slugs[pages.indexOf(page)]}`;
}
const getSnapshot = () =>
  typeof window === "undefined" ? "" : window.location?.hash || "";
function subscribe(notify: () => void) {
  if (typeof window === "undefined" || !window.addEventListener)
    return () => {};
  window.addEventListener("hashchange", notify);
  window.addEventListener("popstate", notify);
  return () => {
    window.removeEventListener("hashchange", notify);
    window.removeEventListener("popstate", notify);
  };
}

// Hash routes keep the production bundle on /dashboard without proxy route rewrites.
// Only page names enter history; credentials and OAuth state never do.
export function useDashboardNavigation(): [Page, (page: Page) => void] {
  const hash = useSyncExternalStore(subscribe, getSnapshot, () => "");
  const [fallback, setFallback] = useState<Page>("Overview");
  const browser =
    typeof window !== "undefined" && Boolean(window.location && window.history);
  const navigate = useCallback((page: Page) => {
    setFallback(page);
    if (typeof window === "undefined" || !window.history || !window.location)
      return;
    const next = pageHash(page);
    if (window.location.hash === next) return;
    window.history.pushState(null, "", next);
    window.dispatchEvent(new Event("popstate"));
  }, []);
  return [browser ? pageFromHash(hash) : fallback, navigate];
}
