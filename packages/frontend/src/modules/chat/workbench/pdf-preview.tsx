import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  openWorkspaceResourceScope,
  readWorkspaceResource,
} from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Spinner } from "@/shared/ui/spinner";

type PdfPreviewProps = {
  chatId?: string;
  rootPath: string;
  path: string;
  title: string;
};

/** 在当前 Chat 范围管理 PDF Blob URL、原生查看器和拖动屏障。 */
export function PdfPreview({ chatId, rootPath, path, title }: PdfPreviewProps) {
  const { t } = useTranslation("chat");
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<
    | { status: "loading" }
    | { status: "ready"; url: string }
    | { status: "error"; message: string }
  >({ status: "loading" });
  const [interactionBlocked, setInteractionBlocked] = useState(false);
  const frameRef = useRef<HTMLIFrameElement>(null);

  useEffect(() => {
    const controller = new AbortController();
    let objectUrl: string | undefined;
    void (async () => {
      try {
        if (!chatId) throw new Error(t("workspace.pdfScopeRequired"));
        const scope = await openWorkspaceResourceScope(chatId);
        const file = await readWorkspaceResource(
          scope.id,
          path,
          controller.signal,
          rootPath,
        );
        if (controller.signal.aborted) return;
        objectUrl = URL.createObjectURL(
          new Blob([Uint8Array.from(file.bytes)], {
            type: "application/pdf",
          }),
        );
        setLoad({ status: "ready", url: objectUrl });
      } catch (cause) {
        if (controller.signal.aborted) return;
        setLoad({
          status: "error",
          message: cause instanceof Error ? cause.message : String(cause),
        });
      }
    })();
    return () => {
      controller.abort();
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [attempt, chatId, path, rootPath, t]);

  useEffect(() => {
    const block = () => {
      setInteractionBlocked(true);
      if (frameRef.current) frameRef.current.style.pointerEvents = "none";
    };
    const unblock = () => {
      setInteractionBlocked(false);
      if (frameRef.current) frameRef.current.style.pointerEvents = "";
    };
    const blockResize = (event: PointerEvent) => {
      if (
        event.target instanceof Element &&
        event.target.closest('[role="separator"][aria-orientation="vertical"]')
      )
        block();
    };
    document.addEventListener("dragstart", block, true);
    document.addEventListener("dragend", unblock, true);
    document.addEventListener("drop", unblock, true);
    window.addEventListener("pointerdown", blockResize, true);
    window.addEventListener("pointerup", unblock, true);
    window.addEventListener("pointercancel", unblock, true);
    window.addEventListener("blur", unblock);
    return () => {
      document.removeEventListener("dragstart", block, true);
      document.removeEventListener("dragend", unblock, true);
      document.removeEventListener("drop", unblock, true);
      window.removeEventListener("pointerdown", blockResize, true);
      window.removeEventListener("pointerup", unblock, true);
      window.removeEventListener("pointercancel", unblock, true);
      window.removeEventListener("blur", unblock);
    };
  }, []);

  return (
    <div className="relative h-full min-h-0 w-full" data-workbench-pdf>
      {load.status === "loading" ? (
        <div className="grid h-full place-items-center" role="status">
          <Spinner />
        </div>
      ) : null}
      {load.status === "error" ? (
        <div
          className="text-muted-foreground type-body flex h-full flex-col items-center justify-center gap-3 px-4 text-center"
          role="alert"
        >
          <span>{load.message}</span>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              setLoad({ status: "loading" });
              setAttempt((value) => value + 1);
            }}
          >
            {t("workspace.retry")}
          </Button>
        </div>
      ) : null}
      {load.status === "ready" ? (
        <iframe
          ref={frameRef}
          className="h-full w-full border-0"
          src={load.url}
          title={title}
        />
      ) : null}
      <div
        aria-hidden="true"
        className="absolute inset-0"
        style={{ pointerEvents: interactionBlocked ? "auto" : "none" }}
      />
    </div>
  );
}
