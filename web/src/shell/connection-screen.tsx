import { useState, type FormEvent } from "react";
import { Button } from "@/shared/ui/button";
import { webText } from "../i18n";
import {
  requestSession,
  SessionError,
  type WebSession,
} from "../platform/session";

export function ConnectionScreen({
  onConnected,
}: {
  onConnected: (session: WebSession) => void;
}) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(webText("offline"));
  async function connect(event: FormEvent) {
    event.preventDefault();
    setPending(true);
    setError("");
    try {
      onConnected(await requestSession());
    } catch (reason) {
      setError(
        reason instanceof SessionError
          ? webText(
              reason.status === 429
                ? "limited"
                : reason.status === 403
                  ? "denied"
                  : reason.status === 409
                    ? "incompatible"
                    : "offline",
            )
          : webText("offline"),
      );
      setPending(false);
    }
  }
  return (
    <main className="bg-background text-foreground flex min-h-dvh items-center justify-center p-6">
      <form
        className="bg-surface-panel flex w-full max-w-md flex-col gap-4 rounded-xl p-6"
        onSubmit={(event) => {
          void connect(event);
        }}
      >
        <h1 className="type-page-title font-semibold">{webText("title")}</h1>
        <p className="type-body text-content-muted">{webText("description")}</p>
        {error ? (
          <p
            id="connection-error"
            className="type-body text-destructive"
            role="alert"
          >
            {error}
          </p>
        ) : null}
        <Button type="submit" size="lg" disabled={pending}>
          {webText(pending ? "connecting" : "retry")}
        </Button>
      </form>
    </main>
  );
}
