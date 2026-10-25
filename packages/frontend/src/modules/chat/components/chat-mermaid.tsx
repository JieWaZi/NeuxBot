import {
  Code2,
  Copy,
  Download,
  Image,
  Maximize2,
  RotateCcw,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { mermaid } from "./chat-mermaid-plugin";

import { Button } from "@/shared/ui/button";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { useTheme } from "@/shared/theme";

import { copyPlainText, downloadTextFile } from "./chat-markdown-export";
import { ChatPreviewDialog } from "./chat-preview-dialog";

type MermaidView = "diagram" | "source";

function Diagram({ fullscreen, svg }: { fullscreen?: boolean; svg: string }) {
  const { t } = useTranslation("chat");
  const [zoom, setZoom] = useState(1);
  return (
    <div
      className={
        fullscreen
          ? "bg-muted relative h-[calc(100dvh-144px)] overflow-auto rounded-lg p-4"
          : "bg-muted h-[min(520px,60vh)] overflow-auto rounded-lg p-4"
      }
    >
      {fullscreen && (
        <div className="border-border bg-surface-elevated sticky top-0 left-0 z-10 float-left grid gap-1 rounded-md border p-1 shadow-sm">
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            title={t("mermaidZoomIn")}
            aria-label={t("mermaidZoomIn")}
            disabled={zoom >= 2}
            onClick={() => setZoom((current) => Math.min(2, current + 0.25))}
          >
            <ZoomIn />
          </Button>
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            title={t("mermaidZoomOut")}
            aria-label={t("mermaidZoomOut")}
            disabled={zoom <= 0.5}
            onClick={() => setZoom((current) => Math.max(0.5, current - 0.25))}
          >
            <ZoomOut />
          </Button>
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            title={t("mermaidZoomReset")}
            aria-label={t("mermaidZoomReset")}
            onClick={() => setZoom(1)}
          >
            <RotateCcw />
          </Button>
        </div>
      )}
      <div
        role="img"
        aria-label="Mermaid diagram"
        className="grid size-full place-items-center transition-transform [&_svg]:h-auto [&_svg]:max-h-full [&_svg]:max-w-full"
        style={{ transform: `scale(${zoom})`, transformOrigin: "top center" }}
        dangerouslySetInnerHTML={{ __html: svg }}
      />
    </div>
  );
}

export function ChatMermaid({ code }: { code: string }) {
  const { t } = useTranslation("chat");
  const { resolvedTheme } = useTheme();
  const reactID = useId();
  const [view, setView] = useState<MermaidView>("diagram");
  const [result, setResult] = useState({
    code: "",
    theme: "",
    svg: "",
    error: "",
  });
  const [fullscreen, setFullscreen] = useState(false);

  const copySource = async () => {
    await copyPlainText(code);
  };

  const downloadSource = () => {
    void downloadTextFile("diagram.mmd", code, "text/plain;charset=utf-8");
  };

  const mermaidTheme = resolvedTheme === "dark" ? "dark" : "default";
  useEffect(() => {
    let active = true;
    void mermaid
      .getMermaid({ theme: mermaidTheme })
      .render(`ally-mermaid-${reactID.replaceAll(":", "")}`, code)
      .then((rendered) => {
        if (active) {
          setResult({
            code,
            theme: mermaidTheme,
            svg: rendered.svg,
            error: "",
          });
        }
      })
      .catch((cause: unknown) => {
        if (!active) return;
        setResult({
          code,
          theme: mermaidTheme,
          svg: "",
          error: cause instanceof Error ? cause.message : t("mermaidError"),
        });
      });
    return () => {
      active = false;
    };
  }, [code, mermaidTheme, reactID, t]);

  const resultIsCurrent = result.code === code && result.theme === mermaidTheme;
  // 新快照绘制期间保留上一版 SVG，避免异步 render 尚未完成时闪回加载占位。
  // 首次绘制没有可复用结果时仍显示加载状态。
  const svg = result.svg;
  const error = resultIsCurrent ? result.error : "";

  const viewOptions = [
    {
      value: "diagram" as const,
      label: t("mermaidDiagram"),
      icon: <Image className="size-icon-sm" strokeWidth={1.5} />,
    },
    {
      value: "source" as const,
      label: t("mermaidSource"),
      icon: <Code2 className="size-icon-sm" strokeWidth={1.5} />,
    },
  ];

  return (
    <section
      className="border-border bg-surface-elevated rounded-lg border p-3"
      data-streamdown="mermaid-block"
    >
      <header className="mb-3 flex items-center justify-between gap-3">
        <SegmentedControl
          ariaLabel={t("mermaidView")}
          options={viewOptions}
          size="sm"
          value={view}
          onValueChange={setView}
        />
        <div className="flex items-center gap-1">
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            title={t("mermaidDownload")}
            aria-label={t("mermaidDownload")}
            onClick={downloadSource}
          >
            <Download />
          </Button>
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            title={t("mermaidCopy")}
            aria-label={t("mermaidCopy")}
            onClick={() => void copySource()}
          >
            <Copy />
          </Button>
          {view === "diagram" && svg && (
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              title={t("previewFullscreen")}
              aria-label={t("previewFullscreen")}
              onClick={() => setFullscreen(true)}
            >
              <Maximize2 />
            </Button>
          )}
        </div>
      </header>

      {view === "source" ? (
        <pre className="bg-muted type-body max-h-[520px] overflow-auto rounded-lg p-4 font-mono whitespace-pre">
          <code>{code}</code>
        </pre>
      ) : error ? (
        <p className="text-destructive type-body rounded-lg p-4">{error}</p>
      ) : svg ? (
        <Diagram svg={svg} />
      ) : (
        <p className="text-muted-foreground type-body bg-muted rounded-lg p-4">
          {t("mermaidLoading")}
        </p>
      )}

      <ChatPreviewDialog
        open={fullscreen}
        title={t("mermaidDiagram")}
        onOpenChange={setFullscreen}
      >
        <Diagram fullscreen svg={svg} />
      </ChatPreviewDialog>
    </section>
  );
}
