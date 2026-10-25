import { FitAddon } from "@xterm/addon-fit";
import { Terminal as Xterm } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

import {
  attachTerminal,
  detachTerminal,
  resizeTerminal,
  startTerminal,
  subscribeTerminalExit,
  subscribeTerminalOutput,
  subscribeTerminalSnapshot,
  writeTerminal,
} from "@/shared/platform";
import { logger } from "@/shared/diagnostics/logger";

type ChatTerminalPanelProps = {
  chatId?: string;
  onSessionBound: (sessionId: string) => void;
  ownerKey: string;
  sessionId?: string;
  workingDirectory: string;
};

const MAX_PENDING_OUTPUT_BYTES = 256 * 1024;

function terminalErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  return fallback;
}

/** The DOM emulator may detach; Main keeps the process and recovery screen. */
export function ChatTerminalPanel({
  chatId,
  onSessionBound,
  ownerKey,
  sessionId,
  workingDirectory,
}: ChatTerminalPanelProps) {
  const { t } = useTranslation("chat");
  const containerRef = useRef<HTMLDivElement>(null);
  const bindingRef = useRef(onSessionBound);
  const initialSessionRef = useRef(sessionId);
  useEffect(() => {
    bindingRef.current = onSessionBound;
  }, [onSessionBound]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const rootStyle = getComputedStyle(document.documentElement);
    const terminal = new Xterm({
      convertEol: true,
      cursorBlink: true,
      fontFamily: 'ui-monospace, "SFMono-Regular", Menlo, monospace',
      fontSize: 12,
      scrollback: 2_000,
      theme: {
        background: getComputedStyle(container).backgroundColor || "Canvas",
        foreground:
          rootStyle.getPropertyValue("--foreground").trim() || "CanvasText",
        cursor:
          rootStyle.getPropertyValue("--accent-default").trim() || "CanvasText",
      },
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(container);
    let disposed = false;
    let currentSessionId = "";
    let currentAttachmentId = "";
    let sequence = 0;
    let ready = false;
    let recovering = false;
    let pendingBytes = 0;
    const pending = new Map<number, string>();
    let reportedInputError = false;
    let reportedResizeError = false;

    const showError = (error: unknown, fallback: string) => {
      if (disposed) return;
      terminal.writeln(
        `\r\n\x1b[31m${terminalErrorMessage(error, fallback)}\x1b[0m`,
      );
    };
    const flush = () => {
      if (!ready) return;
      for (const [next, data] of [...pending].sort(([a], [b]) => a - b)) {
        if (next <= sequence) {
          pending.delete(next);
          pendingBytes -= data.length;
        } else if (next === sequence + 1) {
          terminal.write(data);
          sequence = next;
          pending.delete(next);
          pendingBytes -= data.length;
        } else break;
      }
    };
    const unsubscribeOutput = subscribeTerminalOutput((event) => {
      if (!currentSessionId || event.sessionId !== currentSessionId) return;
      if (event.sequence <= sequence) return;
      pending.set(event.sequence, event.data);
      pendingBytes += event.data.length;
      if (pendingBytes > MAX_PENDING_OUTPUT_BYTES) {
        pending.clear();
        pendingBytes = 0;
        ready = false;
        if (!recovering) {
          recovering = true;
          void attachTerminal(currentSessionId)
            .then(async (snapshot) => {
              if (disposed) {
                await detachTerminal(snapshot.sessionId, snapshot.attachmentId);
                return;
              }
              currentAttachmentId = snapshot.attachmentId;
              sequence = snapshot.sequence;
              resize();
              terminal.reset();
              terminal.write(snapshot.screen);
              ready = true;
              flush();
            })
            .catch((error: unknown) =>
              showError(error, t("terminalWriteFailed")),
            )
            .finally(() => {
              recovering = false;
            });
        }
        return;
      }
      flush();
    });
    const unsubscribeSnapshot = subscribeTerminalSnapshot((snapshot) => {
      if (disposed || snapshot.sessionId !== currentSessionId) return;
      currentAttachmentId = snapshot.attachmentId;
      sequence = snapshot.sequence;
      pending.clear();
      pendingBytes = 0;
      reportedInputError = false;
      reportedResizeError = false;
      terminal.reset();
      terminal.write(snapshot.screen);
      ready = true;
      resize();
    });
    const unsubscribeExit = subscribeTerminalExit((event) => {
      if (event.sessionId === currentSessionId)
        terminal.writeln(`\r\n${t("terminalExited")}`);
    });
    const dataDisposable = terminal.onData((data) => {
      if (!currentSessionId || !currentAttachmentId) return;
      void writeTerminal(currentSessionId, currentAttachmentId, data).catch(
        (error: unknown) => {
          if (reportedInputError) return;
          reportedInputError = true;
          showError(error, t("terminalWriteFailed"));
        },
      );
    });
    const resize = () => {
      if (
        disposed ||
        container.clientWidth < 120 ||
        container.clientHeight < 80
      )
        return;
      fitAddon.fit();
      if (terminal.cols < 10 || terminal.rows < 2) return;
      if (!currentSessionId || !currentAttachmentId) return;
      void resizeTerminal(currentSessionId, currentAttachmentId, {
        rows: terminal.rows,
        cols: terminal.cols,
      }).catch((error: unknown) => {
        if (reportedResizeError) return;
        reportedResizeError = true;
        showError(error, t("terminalResizeFailed"));
      });
    };
    const observer = new ResizeObserver(resize);
    observer.observe(container);
    resize();
    const refreshTheme = () => {
      const styles = getComputedStyle(document.documentElement);
      terminal.options.theme = {
        background: getComputedStyle(container).backgroundColor || "Canvas",
        foreground:
          styles.getPropertyValue("--foreground").trim() || "CanvasText",
        cursor:
          styles.getPropertyValue("--accent-default").trim() || "CanvasText",
      };
    };
    const themeObserver = new MutationObserver(refreshTheme);
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });

    void (async () => {
      if (!chatId) throw new Error(t("terminalScopeRequired"));
      const started = await startTerminal(
        workingDirectory,
        chatId,
        ownerKey,
        !initialSessionRef.current,
      );
      if (
        initialSessionRef.current &&
        started.sessionId !== initialSessionRef.current
      )
        throw new Error(t("terminalSessionChanged"));
      if (disposed) return;
      bindingRef.current(started.sessionId);
      currentSessionId = started.sessionId;
      const snapshot = await attachTerminal(started.sessionId);
      if (disposed) {
        await detachTerminal(snapshot.sessionId, snapshot.attachmentId);
        return;
      }
      currentAttachmentId = snapshot.attachmentId;
      sequence = snapshot.sequence;
      resize();
      terminal.write(snapshot.screen);
      ready = true;
      flush();
      if (snapshot.state === "exited")
        terminal.writeln(`\r\n${t("terminalExited")}`);
    })().catch((error: unknown) => {
      showError(error, t("terminalStartFailed"));
    });

    return () => {
      disposed = true;
      observer.disconnect();
      themeObserver.disconnect();
      dataDisposable.dispose();
      unsubscribeOutput();
      unsubscribeExit();
      unsubscribeSnapshot();
      if (currentSessionId) {
        void detachTerminal(currentSessionId, currentAttachmentId).catch(
          (error: unknown) => {
            logger.error("分离终端视图失败", {
              error: terminalErrorMessage(error, "未知错误"),
            });
          },
        );
      }
      terminal.dispose();
    };
  }, [chatId, ownerKey, t, workingDirectory]);

  return (
    <section
      data-chat-terminal-view
      className="bg-surface absolute inset-0 flex min-h-0 flex-col overflow-hidden"
      aria-label={t("terminal")}
    >
      <div className="min-h-0 flex-1 p-2">
        <div ref={containerRef} className="bg-surface size-full min-h-0" />
      </div>
    </section>
  );
}
