import { useCallback, useEffect, useRef, useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import styles from "@/features/oauth/CodexReauthDialog.module.scss";
import { api } from "./api";
import { safeOAuthLink, type OAuthAttempt } from "./oauthSession";

export function OAuthSession({
  attempt,
  secret,
  onClose,
  onComplete,
}: {
  attempt: OAuthAttempt;
  secret: string;
  onClose: () => void;
  onComplete: () => void;
}) {
  const [status, setStatus] = useState<"waiting" | "success" | "error">(
    "waiting",
  );
  const [message, setMessage] = useState("");
  const [checks, setChecks] = useState(0);
  const [callbackURL, setCallbackURL] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const finalized = useRef(false);
  const mounted = useRef(false);
  const path = `?state=${encodeURIComponent(attempt.state)}`;

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, [secret, path]);

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      try {
        const result = await api<{ status?: string }>(
          secret,
          `/oauth/status${path}`,
          "GET",
          undefined,
          controller.signal,
        );
        if (controller.signal.aborted || finalized.current) return;
        if (result.status === "ok") {
          finalized.current = true;
          setStatus("success");
          setMessage(
            "Provider sign-in completed. Account identity and pool metadata are being refreshed.",
          );
          onComplete();
        } else if (result.status === "wait") {
          setStatus("waiting");
          setMessage(
            "Waiting for the provider callback. Status is checked automatically.",
          );
          timer = setTimeout(poll, 3000);
        } else {
          setStatus("error");
          setMessage(
            "Provider sign-in failed or the session expired. Close this session and start a new sign-in.",
          );
        }
      } catch {
        if (controller.signal.aborted || finalized.current) return;
        setStatus("error");
        setMessage(
          "Unable to check provider status. Retry the status check, or cancel this session.",
        );
      }
    };
    void poll();
    return () => {
      controller.abort();
      if (timer !== undefined) clearTimeout(timer);
    };
  }, [secret, path, checks, onComplete]);

  const close = useCallback(async () => {
    if (finalized.current) {
      onClose();
      return;
    }
    setCancelling(true);
    try {
      await api(secret, `/oauth/session${path}`, "DELETE");
      if (!mounted.current) return;
      finalized.current = true;
      onClose();
    } catch {
      if (mounted.current) {
        setStatus("error");
        setMessage(
          "Unable to cancel the provider session. Retry cancellation before starting another sign-in.",
        );
      }
    } finally {
      if (mounted.current) setCancelling(false);
    }
  }, [secret, path, onClose]);
  const requestClose = useCallback(() => {
    void close();
  }, [close]);

  const callback = async () => {
    let parsed: URL;
    try {
      parsed = new URL(callbackURL);
    } catch {
      setMessage("Paste the complete callback URL, not an authorization code.");
      return;
    }
    if (
      !["http:", "https:"].includes(parsed.protocol) ||
      parsed.searchParams.get("state") !== attempt.state ||
      (!parsed.searchParams.get("code") && !parsed.searchParams.get("error"))
    ) {
      setMessage(
        "The callback must contain this session's state and a code or provider error. A different session cannot be submitted.",
      );
      return;
    }
    setSubmitting(true);
    try {
      await api(secret, "/oauth/callback", "POST", {
        provider: attempt.provider,
        state: attempt.state,
        code: parsed.searchParams.get("code") || "",
        error: parsed.searchParams.get("error") || "",
      });
      if (!mounted.current) return;
      setCallbackURL("");
      if (finalized.current) return;
      setMessage(
        "Callback accepted. Waiting for the existing provider token exchange.",
      );
      setChecks((n) => n + 1);
    } catch {
      if (mounted.current)
        setMessage(
          "Callback submission failed. Verify the active session and retry.",
        );
    } finally {
      if (mounted.current) setSubmitting(false);
    }
  };

  return (
    <Modal
      open
      title={`${attempt.provider} · Add / re-auth`}
      onClose={requestClose}
      closeDisabled={cancelling || submitting}
      footer={
        <div className={styles.footer}>
          <Button disabled={cancelling || submitting} onClick={requestClose}>
            {status === "success"
              ? "Done"
              : cancelling
                ? "Cancelling…"
                : "Cancel sign-in"}
          </Button>
        </div>
      }
    >
      <div className={styles.dialogBody}>
        <p className={styles.hint}>
          Use the same provider account/workspace to preserve its logical
          identity and pools. A different principal is imported separately and
          never inherits the old scope.
        </p>
        <div className={styles.oauthPanel}>
          {safeOAuthLink(attempt.url) && status !== "success" && (
            <a
              className="btn btn-primary"
              href={attempt.url}
              target="_blank"
              rel="noopener noreferrer"
            >
              Open provider sign-in
            </a>
          )}
          <p
            role="status"
            className={`${styles.status} ${status === "success" ? styles.statusSuccess : status === "error" ? styles.statusError : styles.statusWaiting}`}
          >
            {message || "Checking provider sign-in status…"}
          </p>
          {status !== "success" && (
            <Button
              disabled={cancelling || submitting}
              onClick={() => setChecks((n) => n + 1)}
            >
              Check status
            </Button>
          )}
        </div>
        {status !== "success" && (
          <form
            className={styles.callbackSection}
            onSubmit={(e) => {
              e.preventDefault();
              void callback();
            }}
          >
            <Input
              label="Remote callback URL"
              value={callbackURL}
              onChange={(e) => setCallbackURL(e.target.value)}
              autoComplete="off"
              spellCheck={false}
              hint="If the provider redirects to localhost on your own computer, paste that URL here. It is submitted to the proxy, never fetched or stored in browser history."
            />
            <Button
              type="submit"
              disabled={!callbackURL || submitting || cancelling}
            >
              Submit callback
            </Button>
          </form>
        )}
      </div>
    </Modal>
  );
}
