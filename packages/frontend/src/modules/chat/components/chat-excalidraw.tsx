import { Download, Maximize2 } from "lucide-react";
import { useContext, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { ContentVisibility } from "@/shared/lib/content-visibility";
import { observeViewportActivation } from "@/shared/lib/viewport-activation";
import { saveBlob } from "@/shared/platform";
import { useTheme } from "@/shared/theme";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Skeleton } from "@/shared/ui/skeleton";
import { toast } from "@/shared/ui/sonner";

import { ChatDiagramFrame } from "./chat-diagram-frame";
import { renderExcalidraw } from "./chat-excalidraw-render";
import { ChatPreviewDialog } from "./chat-preview-dialog";
import {
  ChatPreviewViewport,
  useChatPreviewFit,
} from "./chat-preview-viewport";

type Drawing = Awaited<ReturnType<typeof renderExcalidraw>>;

function naturalSize(content: HTMLDivElement) {
  const image = content.querySelector("img");
  if (image) {
    return {
      width: Number(image.getAttribute("width")),
      height: Number(image.getAttribute("height")),
    };
  }
}

function FullDrawing({ drawing, label }: { drawing: Drawing; label: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const { fittedSize, viewportRef } = useChatPreviewFit(ref, naturalSize, {
    fitHeight: true,
    contentVersion: drawing.image,
  });

  return (
    <ChatPreviewViewport
      fittedSize={fittedSize}
      viewportRef={viewportRef}
      naturalWidth={drawing.width}
    >
      {(zoom) => (
        <div
          ref={ref}
          className="origin-top-left"
          style={{
            ...fittedSize,
            transform: zoom === 1 ? undefined : `scale(${zoom})`,
          }}
        >
          <img
            src={drawing.image}
            alt={label}
            width={drawing.width}
            height={drawing.height}
            className="block h-auto w-full"
          />
        </div>
      )}
    </ChatPreviewViewport>
  );
}

export function ChatExcalidraw({ elements }: { elements?: string }) {
  const { t } = useTranslation("chat");
  if (!elements) {
    return (
      <p className="text-muted-foreground type-body">
        {t("excalidrawUnavailable")}
      </p>
    );
  }
  return <ExcalidrawPreview key={elements} elements={elements} />;
}

function ExcalidrawPreview({ elements }: { elements: string }) {
  const { t } = useTranslation("chat");
  const { resolvedTheme } = useTheme();
  const visible = useContext(ContentVisibility);
  const slot = useRef<HTMLElement>(null);
  const opener = useRef<HTMLElement>(null);
  const [fullscreen, setFullscreen] = useState(false);
  const [result, setResult] = useState<{
    theme: string;
    drawing: Drawing | "error";
  }>();
  const drawing = result?.theme === resolvedTheme ? result.drawing : undefined;

  useEffect(() => {
    const element = slot.current;
    if (!element || !visible || drawing) return;
    let active = true;
    const stop = observeViewportActivation(element, () => {
      void renderExcalidraw(elements, resolvedTheme === "dark").then(
        (next) => {
          if (active) setResult({ theme: resolvedTheme, drawing: next });
        },
        () => {
          if (active) setResult({ theme: resolvedTheme, drawing: "error" });
        },
      );
    });
    return () => {
      active = false;
      stop();
    };
  }, [elements, drawing, resolvedTheme, visible]);

  const openPreview = (button: HTMLButtonElement) => {
    opener.current = button;
    setFullscreen(true);
  };
  const download = async (format: "png" | "excalidraw", result: Drawing) => {
    try {
      const data =
        format === "png"
          ? await result.createPNG()
          : new Blob([result.source], { type: "application/json" });
      await saveBlob(data, `diagram.${format}`);
    } catch {
      toast.error(t("workspace.downloadFileError"));
    }
  };

  const label = t("excalidrawDiagram");
  const expandLabel = t("previewFullscreen");
  return (
    <ChatDiagramFrame
      kind="excalidraw"
      frameRef={slot}
      actions={
        drawing && drawing !== "error" ? (
          <>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  size="icon-sm"
                  variant="ghost"
                  title={t("excalidrawDownload")}
                  aria-label={t("excalidrawDownload")}
                >
                  <Download />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  onSelect={() => void download("png", drawing)}
                >
                  {t("mermaidDownloadPNG")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  onSelect={() => void download("excalidraw", drawing)}
                >
                  {t("excalidrawSource")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              title={expandLabel}
              aria-label={expandLabel}
              aria-haspopup="dialog"
              onClick={(event) => openPreview(event.currentTarget)}
            >
              <Maximize2 />
            </Button>
          </>
        ) : null
      }
    >
      {drawing === "error" ? (
        <p className="text-destructive type-body p-4">
          {t("excalidrawRenderError")}
        </p>
      ) : drawing ? (
        <button
          type="button"
          aria-label={t("excalidrawOpen")}
          aria-haspopup="dialog"
          onClick={(event) => openPreview(event.currentTarget)}
          className="focus-visible:ring-ring grid w-full cursor-zoom-in place-items-center rounded-sm p-4 outline-none focus-visible:ring-2"
        >
          <img
            src={drawing.image}
            alt=""
            width={drawing.width}
            height={drawing.height}
            className="block h-auto max-h-[64rem] max-w-full object-contain"
          />
        </button>
      ) : (
        <div
          role="status"
          aria-busy="true"
          aria-label={t("mermaidLoading")}
          className="p-4"
        >
          <Skeleton className="h-24 w-full" />
        </div>
      )}
      <ChatPreviewDialog
        title={label}
        open={fullscreen}
        onOpenChange={setFullscreen}
        returnFocusRef={opener}
      >
        {drawing && drawing !== "error" ? (
          <FullDrawing drawing={drawing} label={label} />
        ) : null}
      </ChatPreviewDialog>
    </ChatDiagramFrame>
  );
}
