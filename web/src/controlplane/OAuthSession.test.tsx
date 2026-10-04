import { act, StrictMode, type ReactNode } from "react";
import { create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { OAuthSession } from "./OAuthSession";
import { safeOAuthLink } from "./oauthSession";
import { api } from "./api";

vi.mock("./api", () => ({ api: vi.fn() }));
vi.mock("@/components/ui/Modal", () => ({
  Modal: ({ children, footer }: { children: ReactNode; footer: ReactNode }) => (
    <section>
      {children}
      {footer}
    </section>
  ),
}));
const attempt = {
  provider: "claude",
  state: "fixture-oauth-state",
  url: "https://claude.ai/oauth/authorize?state=fixture-oauth-state",
};
let view: ReactTestRenderer | undefined;
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  vi.useFakeTimers();
  vi.mocked(api).mockReset();
  vi.mocked(api).mockResolvedValue({ status: "wait" });
});
afterEach(async () => {
  if (view) await act(async () => view?.unmount());
  view = undefined;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("adapted provider OAuth dialog", () => {
  it("does not overlap slow polls, then refreshes once after completion", async () => {
    let finish!: (result: { status: string }) => void;
    vi.mocked(api).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const complete = vi.fn(),
      close = vi.fn();
    await act(async () => {
      view = create(
        <OAuthSession
          attempt={attempt}
          secret="fixture-admin"
          onClose={close}
          onComplete={complete}
        />,
      );
    });
    const stableClose = view!.root.findByType(Modal).props.onClose;
    await act(async () => vi.advanceTimersByTimeAsync(6000));
    expect(api).toHaveBeenCalledTimes(1);
    await act(async () => finish({ status: "wait" }));
    expect(view!.root.findByType(Modal).props.onClose).toBe(stableClose);
    vi.mocked(api).mockResolvedValueOnce({ status: "ok" });
    await act(async () => vi.advanceTimersByTimeAsync(3000));
    expect(complete).toHaveBeenCalledTimes(1);
    await act(async () => vi.advanceTimersByTimeAsync(9000));
    expect(api).toHaveBeenCalledTimes(2);
    expect(view!.root.findAllByType("a")).toHaveLength(0);
  });
  it("rejects another session's callback and submits only the active provider/state/code", async () => {
    await act(async () => {
      view = create(
        <OAuthSession
          attempt={attempt}
          secret="fixture-admin"
          onClose={() => {}}
          onComplete={() => {}}
        />,
      );
    });
    const input = () => view!.root.findByType(Input);
    const form = () => view!.root.findByType("form");
    await act(async () =>
      input().props.onChange({
        target: {
          value:
            "http://localhost:54545/callback?state=other&code=fixture-code",
        },
      }),
    );
    await act(async () => form().props.onSubmit({ preventDefault() {} }));
    expect(
      vi.mocked(api).mock.calls.filter((c) => c[2] === "POST"),
    ).toHaveLength(0);
    await act(async () =>
      input().props.onChange({
        target: {
          value:
            "http://localhost:54545/callback?state=fixture-oauth-state&code=fixture-code",
        },
      }),
    );
    await act(async () => form().props.onSubmit({ preventDefault() {} }));
    expect(api).toHaveBeenCalledWith(
      "fixture-admin",
      "/oauth/callback",
      "POST",
      {
        provider: "claude",
        state: attempt.state,
        code: "fixture-code",
        error: "",
      },
    );
    expect(input().props.value).toBe("");
  });
  it("cancels through the separate admin endpoint before closing", async () => {
    const close = vi.fn();
    await act(async () => {
      view = create(
        <OAuthSession
          attempt={attempt}
          secret="fixture-admin"
          onClose={close}
          onComplete={() => {}}
        />,
      );
    });
    await act(async () =>
      view!.root
        .findAllByType(Button)
        .find((b) => b.props.children === "Cancel sign-in")!
        .props.onClick(),
    );
    expect(api).toHaveBeenCalledWith(
      "fixture-admin",
      "/oauth/session?state=fixture-oauth-state",
      "DELETE",
    );
    expect(close).toHaveBeenCalledTimes(1);
  });
  it("ignores a late poll after unmount and does not cancel on StrictMode effect replay", async () => {
    const complete = vi.fn();
    let finish!: (result: { status: string }) => void;
    vi.mocked(api).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    await act(async () => {
      view = create(
        <StrictMode>
          <OAuthSession
            attempt={attempt}
            secret="fixture-admin"
            onClose={() => {}}
            onComplete={complete}
          />
        </StrictMode>,
      );
    });
    expect(vi.mocked(api).mock.calls.every((c) => c[2] === "GET")).toBe(true);
    await act(async () => view!.unmount());
    view = undefined;
    await act(async () => finish({ status: "ok" }));
    expect(complete).not.toHaveBeenCalled();
    expect(
      vi.mocked(api).mock.calls.every((c) => (c[4] as AbortSignal).aborted),
    ).toBe(true);
  });
  it("does not render executable or credential-bearing provider links", () => {
    expect(safeOAuthLink("javascript:alert(1)")).toBe(false);
    expect(safeOAuthLink("https://user:password@example.com/auth")).toBe(false);
    expect(safeOAuthLink(attempt.url)).toBe(true);
  });
});
