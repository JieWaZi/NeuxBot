import { Code2, Copy, Download, Image, Maximize2 } from "lucide-react";
import { useContext, useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { mermaid } from "./chat-mermaid-plugin";

import { Button } from "@/shared/ui/button";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { useTheme } from "@/shared/theme";
import { cn } from "@/shared/lib/cn";
import { ContentVisibility } from "@/shared/lib/content-visibility";
import { saveBlob } from "@/shared/platform";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Skeleton } from "@/shared/ui/skeleton";
import { toast } from "@/shared/ui/sonner";

import { copyPlainText, downloadTextFile } from "./chat-markdown-export";
import { createMermaidPNG, readMermaidSize } from "./chat-mermaid-export";
import { ChatPreviewDialog } from "./chat-preview-dialog";
import {
  ChatPreviewViewport,
  useChatPreviewFit,
} from "./chat-preview-viewport";

type MermaidView = "diagram" | "source";

// 仅适配默认图元；作者明确设置的内联颜色仍由原图保留。
// CSS 变量在实际 SVG 宿主解析，避免把品牌色或浅色常量写进生成结果。
const MERMAID_THEME_CSS = `
  .label, text { font-family: var(--font-sans); }
  .node rect, .node circle, .node ellipse, .node polygon, .node path,
  .actor, .actor-top, .actor-bottom {
    fill: var(--muted); stroke: var(--border-strong);
  }
  .nodeLabel, .nodeLabel p, .label text, .edgeLabel, .edgeLabel p,
  .messageText, .labelText, .loopText {
    color: var(--foreground); fill: var(--foreground);
  }
  .flowchart-link, .messageLine0, .messageLine1, .actor-line {
    stroke: var(--muted-foreground);
  }
  .edgeLabel rect, .edgeLabel .labelBkg { fill: var(--muted); }
`;

function readDiagramSize(content: HTMLDivElement) {
  return readMermaidSize(content.querySelector("svg"));
}

function Diagram({ fullscreen, svg }: { fullscreen?: boolean; svg: string }) {
  const diagramRef = useRef<HTMLDivElement>(null);
  const { fittedSize, viewportRef } = useChatPreviewFit(
    diagramRef,
    readDiagramSize,
    { fitHeight: fullscreen, contentVersion: svg },
  );
  const renderDiagram = (zoom: number) => (
    <div
      ref={diagramRef}
      role="img"
      aria-label="Mermaid diagram"
      className={cn(
        "[&_svg]:block [&_svg]:h-auto [&_svg]:w-full [&_svg]:max-w-full",
        fullscreen ? "origin-top-left" : "max-w-full shrink-0",
      )}
      style={{
        ...fittedSize,
        transform: zoom === 1 ? undefined : `scale(${zoom})`,
      }}
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );

  return fullscreen ? (
    <ChatPreviewViewport viewportRef={viewportRef} fittedSize={fittedSize}>
      {renderDiagram}
    </ChatPreviewViewport>
  ) : (
    <div className="relative min-w-0">
      <div ref={viewportRef} className="bg-muted w-full min-w-0 rounded-lg p-4">
        <div className="flex w-full justify-center">{renderDiagram(1)}</div>
      </div>
    </div>
  );
}

export function ChatMermaid({ code }: { code: string }) {
  const { t } = useTranslation("chat");
  const { resolvedTheme } = useTheme();
  const visible = useContext(ContentVisibility);
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

  const mermaidTheme = resolvedTheme === "dark" ? "dark" : "default";
  useEffect(() => {
    if (!visible || (result.code === code && result.theme === mermaidTheme))
      return;
    let active = true;
    void mermaid
      .getMermaid({
        theme: mermaidTheme,
        fontFamily: "var(--font-sans)",
        themeCSS: MERMAID_THEME_CSS,
      })
      .render(`bot-mermaid-${reactID.replaceAll(":", "")}`, code)
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
  }, [code, mermaidTheme, reactID, t, visible, result.code, result.theme]);

  const resultIsCurrent = result.code === code && result.theme === mermaidTheme;
  // 新快照绘制期间保留上一版 SVG，避免异步 render 尚未完成时闪回加载占位。
  // 首次绘制没有可复用结果时仍显示加载状态。
  const svg = result.svg;
  const error = resultIsCurrent ? result.error : "";
  const imageReady = resultIsCurrent && Boolean(svg);

  const download = async (format: "png" | "mmd") => {
    try {
      if (format === "mmd") {
        await downloadTextFile("diagram.mmd", code, "text/plain;charset=utf-8");
      } else if (imageReady) {
        await saveBlob(await createMermaidPNG(svg), "diagram.png");
      }
    } catch {
      toast.error(t("workspace.downloadFileError"));
    }
  };

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
      className="bg-muted my-3 min-w-0 rounded-lg p-3"
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
            title={t("mermaidCopy")}
            aria-label={t("mermaidCopy")}
            onClick={() => void copySource()}
          >
            <Copy />
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                title={t("mermaidDownload")}
                aria-label={t("mermaidDownload")}
              >
                <Download />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                disabled={!imageReady}
                onSelect={() => void download("png")}
              >
                {t("mermaidDownloadPNG")}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => void download("mmd")}>
                {t("mermaidDownloadMMD")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
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
        <div
          role="status"
          aria-label={t("mermaidLoading")}
          aria-busy="true"
          className="p-4"
        >
          <Skeleton className="h-24 w-full" />
        </div>
      )}

      <ChatPreviewDialog
        open={fullscreen}
        title={t("mermaidDiagram")}
        onOpenChange={setFullscreen}
      >
        {/* Radix 会保留关闭动画中的 Content；图形随实际卸载一起释放。 */}
        {svg ? <Diagram key={svg} fullscreen svg={svg} /> : null}
      </ChatPreviewDialog>
    </section>
  );
}
