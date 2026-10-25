import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Minus, Plus } from "lucide-react";

import { Button } from "@/shared/ui/button";
import { Spinner } from "@/shared/ui/spinner";
import { readOfficeBytes } from "./read-office-bytes";
import { officePreviewErrorKey } from "./office-preview-error";

type DocxPreviewProps = {
  chatId?: string;
  rootPath: string;
  path: string;
  title: string;
};

/** OFFICE DocxView：渲染到本次 generation 独有的容器，旧任务不能污染新文件。 */
export function DocxPreview({
  chatId,
  rootPath,
  path,
  title,
}: DocxPreviewProps) {
  const { t } = useTranslation("chat");
  const hostRef = useRef<HTMLDivElement>(null);
  const [attempt, setAttempt] = useState(0);
  const [zoom, setZoom] = useState(100);
  const [load, setLoad] = useState<
    | { status: "loading" }
    | { status: "ready" }
    | { status: "error"; key: ReturnType<typeof officePreviewErrorKey> }
  >({ status: "loading" });

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const controller = new AbortController();
    const stage = document.createElement("div");
    host.replaceChildren(stage);
    void (async () => {
      try {
        const bytes = await readOfficeBytes(
          chatId,
          rootPath,
          path,
          controller.signal,
        );
        const { renderAsync } = await import("docx-preview");
        controller.signal.throwIfAborted();
        await renderAsync(bytes, stage, undefined, {
          className: "docx",
          inWrapper: true,
          ignoreWidth: false,
          ignoreHeight: false,
          breakPages: true,
          experimental: false,
        });
        if (!controller.signal.aborted) setLoad({ status: "ready" });
      } catch (cause) {
        if (controller.signal.aborted) return;
        stage.remove();
        setLoad({
          status: "error",
          key: officePreviewErrorKey(cause),
        });
      }
    })();
    return () => {
      controller.abort();
      stage.remove();
    };
  }, [attempt, chatId, path, rootPath]);

  return (
    <div className="flex h-full min-h-0 flex-col" data-workbench-docx>
      <div className="bg-surface-tertiary relative min-h-0 flex-1 overflow-auto">
        <div
          ref={hostRef}
          className="mx-auto w-max max-w-full"
          aria-label={title}
          style={{ zoom: zoom / 100 }}
        />
        {load.status === "loading" ? (
          <div
            className="absolute inset-0 grid place-items-center"
            role="status"
          >
            <Spinner />
          </div>
        ) : null}
        {load.status === "error" ? (
          <div
            className="text-muted-foreground type-body absolute inset-0 flex flex-col items-center justify-center gap-3 px-4 text-center"
            role="alert"
          >
            <span>{t(load.key)}</span>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                setLoad({ status: "loading" });
                setAttempt((value) => value + 1);
              }}
            >
              {t("workspace.retry")}
            </Button>
          </div>
        ) : null}
      </div>
      <div className="border-border bg-surface type-caption flex shrink-0 items-center justify-end gap-2 border-t px-3 py-2">
        <span>{t("workbench.zoom")}</span>
        <Button
          size="sm"
          variant="ghost"
          aria-label={t("workspace.zoomOut")}
          disabled={zoom <= 50}
          onClick={() => setZoom((value) => Math.max(50, value - 10))}
        >
          <Minus />
        </Button>
        <span className="w-10 text-right tabular-nums">{zoom}%</span>
        <Button
          size="sm"
          variant="ghost"
          aria-label={t("workspace.zoomIn")}
          disabled={zoom >= 200}
          onClick={() => setZoom((value) => Math.min(200, value + 10))}
        >
          <Plus />
        </Button>
      </div>
    </div>
  );
}
