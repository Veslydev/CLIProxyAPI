import { useMemo, useSyncExternalStore } from "react";

const subscribe = (listener: () => void) => {
  window.addEventListener("popstate", listener);
  return () => window.removeEventListener("popstate", listener);
};
const snapshot = () => window.location.pathname + window.location.search;
export function navigate(path: string, replace = false) {
  const url = new URL(path, window.location.origin);
  if (url.origin !== window.location.origin) throw new Error("External navigation rejected");
  window.history[replace ? "replaceState" : "pushState"](null, "", url);
  window.dispatchEvent(new PopStateEvent("popstate"));
}
export function usePathname() { return useSyncExternalStore(subscribe, snapshot).split("?")[0]; }
export function useSearchParams() {
  const path = useSyncExternalStore(subscribe, snapshot);
  return useMemo(() => new URLSearchParams(path.split("?")[1] ?? ""), [path]);
}
const router = {
  push: (path: string, _options?: { scroll?: boolean }) => navigate(path),
  replace: (path: string, _options?: { scroll?: boolean }) => navigate(path, true),
  refresh: () => window.dispatchEvent(new Event("dashboard:refresh")),
  back: () => window.history.back(),
};
export function useRouter() { return router; }
