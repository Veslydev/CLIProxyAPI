import { act } from "react";
import { create } from "react-test-renderer";
import { afterEach, describe, expect, it, vi } from "vitest";
import { pageFromHash, pageHash, useDashboardNavigation } from "./navigation";

afterEach(() => vi.unstubAllGlobals());
describe("dashboard browser history", () => {
  it("validates direct page links", () => {
    expect(pageFromHash("#/accounts")).toBe("Accounts");
    expect(pageFromHash("#/settings?ignored=1")).toBe("Settings");
    expect(pageFromHash("#/untrusted-route")).toBe("Overview");
    expect(pageHash("API Keys")).toBe("#/keys");
  });
  it("handles back/forward events and removes listeners on unmount", async () => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    const listeners = new Map<string, Set<() => void>>();
    const location = { hash: "#/accounts" };
    const pushState = vi.fn((_state: unknown, _title: string, hash: string) => {
      location.hash = hash;
    });
    vi.stubGlobal("window", {
      location,
      history: { pushState },
      addEventListener: (type: string, fn: () => void) => {
        const set = listeners.get(type) || new Set();
        set.add(fn);
        listeners.set(type, set);
      },
      removeEventListener: (type: string, fn: () => void) =>
        listeners.get(type)?.delete(fn),
      dispatchEvent: (e: Event) => listeners.get(e.type)?.forEach((fn) => fn()),
    });
    function Probe() {
      const [page, navigate] = useDashboardNavigation();
      return <button onClick={() => navigate("Monitoring")}>{page}</button>;
    }
    let view!: ReturnType<typeof create>;
    await act(async () => {
      view = create(<Probe />);
    });
    expect(view.root.findByType("button").props.children).toBe("Accounts");
    await act(async () => view.root.findByType("button").props.onClick());
    expect(pushState).toHaveBeenCalledWith(null, "", "#/monitoring");
    expect(view.root.findByType("button").props.children).toBe("Monitoring");
    await act(async () => {
      location.hash = "#/accounts";
      listeners.get("popstate")?.forEach((fn) => fn());
    });
    expect(view.root.findByType("button").props.children).toBe("Accounts");
    await act(async () => view.unmount());
    expect([...listeners.values()].every((set) => set.size === 0)).toBe(true);
  });
});
