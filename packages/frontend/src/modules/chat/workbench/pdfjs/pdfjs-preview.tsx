import "pdfjs-dist/web/pdf_viewer.css";

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type RefObject,
} from "react";
import type { PdfDocument } from "./document";
import { renderPdfPage } from "./document";
import { PdfWorkerFailure } from "./errors";
import { openPdf } from "./runtime";
import { pdfTextRenderer } from "./text";

import {
  openWorkspaceResourceScope,
  readWorkspaceResource,
} from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Spinner } from "@/shared/ui/spinner";
import { PdfPreview as NativePdfPreview } from "../pdf-preview";
import { useTranslation } from "react-i18next";

type PdfPreviewProps = {
  chatId?: string;
  rootPath: string;
  path: string;
  title: string;
};

type LoadState =
  | { key: string; kind: "ready"; document: PdfDocument; pageWidth: number }
  | { key: string; kind: "error"; message: string };

/** PDF.js 画布与文本阅读器，原生查看器作为显式可选模式。 */
export function PdfJsPreview(props: PdfPreviewProps) {
  const { t } = useTranslation("chat");
  const [native, setNative] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<LoadState>();
  const [fitWidth, setFitWidth] = useState(true);
  const [manualZoom, setManualZoom] = useState(1);
  const [availableWidth, setAvailableWidth] = useState(0);
  const [currentPage, setCurrentPage] = useState(1);
  const scrollport = useRef<HTMLDivElement>(null);
  const sourceKey = `${props.chatId ?? ""}:${props.rootPath}:${props.path}:${attempt}`;
  const ready = load?.key === sourceKey && load.kind === "ready" ? load : null;
  const error = load?.key === sourceKey && load.kind === "error" ? load : null;
  const zoom =
    fitWidth && ready && availableWidth > 0
      ? Math.max(0.25, Math.min(2, (availableWidth - 48) / ready.pageWidth))
      : manualZoom;

  useEffect(() => {
    if (native) return;
    const controller = new AbortController();
    let session: ReturnType<typeof openPdf> | undefined;
    void (async () => {
      try {
        if (!props.chatId) throw new Error(t("workspace.pdfScopeRequired"));
        const scope = await openWorkspaceResourceScope(props.chatId);
        const file = await readWorkspaceResource(
          scope.id,
          props.path,
          controller.signal,
          props.rootPath,
        );
        if (controller.signal.aborted) return;
        // PDF.js transfers its input buffer; keep the bridge's bytes owned by
        // the shared resource layer so a second renderer can still use them.
        const bytes = Uint8Array.from(file.bytes);
        session = openPdf(bytes, controller.signal, () => {
          if (!controller.signal.aborted)
            setLoad({
              key: sourceKey,
              kind: "error",
              message: t("workspace.pdfWorkerFailed"),
            });
        });
        const document = await session.document;
        const first = await document.getPage(1);
        if (controller.signal.aborted) return;
        const width = first.getViewport({ scale: 96 / 72 }).width;
        setLoad({ key: sourceKey, kind: "ready", document, pageWidth: width });
      } catch (cause) {
        if (controller.signal.aborted) return;
        setLoad({
          key: sourceKey,
          kind: "error",
          message:
            cause instanceof PdfWorkerFailure
              ? t("workspace.pdfWorkerFailed")
              : cause instanceof Error
                ? cause.message
                : String(cause),
        });
      }
    })();
    return () => {
      controller.abort();
      if (session) void session.dispose();
    };
  }, [native, props.chatId, props.path, props.rootPath, sourceKey, t]);

  useEffect(() => {
    const node = scrollport.current;
    if (!node || native) return;
    let active = true;
    const observer = new ResizeObserver(() => {
      if (active) setAvailableWidth(node.clientWidth);
    });
    observer.observe(node);
    queueMicrotask(() => {
      if (active) setAvailableWidth(node.clientWidth);
    });
    return () => {
      active = false;
      observer.disconnect();
    };
  }, [native]);

  const onPageVisible = useCallback((page: number) => setCurrentPage(page), []);
  const changeZoom = (change: number) => {
    setManualZoom(Math.max(0.25, Math.min(4, zoom + change)));
    setFitWidth(false);
  };

  return (
    <div
      className="bg-surface flex h-full min-h-0 flex-col"
      data-workbench-pdfjs
    >
      <div className="border-border-subtle bg-surface flex shrink-0 items-center gap-1 border-b px-2 py-1">
        <Button
          size="sm"
          variant={native ? "ghost" : "secondary"}
          onClick={() => {
            setLoad(undefined);
            setNative(false);
          }}
        >
          PDF.js
        </Button>
        <Button
          size="sm"
          variant={native ? "secondary" : "ghost"}
          onClick={() => {
            setLoad(undefined);
            setNative(true);
          }}
        >
          {t("workspace.pdfNative")}
        </Button>
        {!native ? (
          <>
            <span className="text-muted-foreground type-caption ml-auto tabular-nums">
              {ready ? `${currentPage} / ${ready.document.numPages}` : ""}
            </span>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label={t("workspace.zoomOut")}
              disabled={!ready || zoom <= 0.25}
              onClick={() => changeZoom(-0.25)}
            >
              −
            </Button>
            <span className="type-caption w-10 text-center tabular-nums">
              {Math.round(zoom * 100)}%
            </span>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label={t("workspace.zoomIn")}
              disabled={!ready || zoom >= 4}
              onClick={() => changeZoom(0.25)}
            >
              +
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={!ready}
              onClick={() => setFitWidth(true)}
            >
              {t("workspace.pdfFitWidth")}
            </Button>
          </>
        ) : null}
      </div>
      <div className="relative min-h-0 flex-1">
        {native ? (
          <NativePdfPreview {...props} />
        ) : (
          <div
            ref={scrollport}
            className="h-full overflow-auto overscroll-contain"
          >
            {error ? (
              <div
                className="text-muted-foreground type-body flex h-full flex-col items-center justify-center gap-3 px-4 text-center"
                role="alert"
              >
                <span>{error.message}</span>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => setAttempt((value) => value + 1)}
                >
                  {t("workspace.retry")}
                </Button>
              </div>
            ) : null}
            {!ready && !error ? (
              <div className="grid h-full place-items-center" role="status">
                <Spinner />
              </div>
            ) : null}
            {ready ? (
              <section
                className="flex min-h-full min-w-full flex-col items-center gap-3 p-3"
                aria-label={props.title}
              >
                {Array.from({ length: ready.document.numPages }, (_, index) => (
                  <PdfPage
                    key={index}
                    document={ready.document}
                    page={index + 1}
                    zoom={zoom}
                    scrollport={scrollport}
                    onVisible={onPageVisible}
                  />
                ))}
              </section>
            ) : null}
          </div>
        )}
      </div>
    </div>
  );
}

function PdfPage({
  document,
  page,
  zoom,
  scrollport,
  onVisible,
}: {
  document: PdfDocument;
  page: number;
  zoom: number;
  scrollport: RefObject<HTMLDivElement | null>;
  onVisible: (page: number) => void;
}) {
  const { t } = useTranslation("chat");
  const host = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const text = useRef<HTMLDivElement>(null);
  const renderQueue = useRef(Promise.resolve());
  const renderedZoom = useRef<number | undefined>(undefined);
  const textTask = useRef<
    ReturnType<ReturnType<typeof pdfTextRenderer>> | undefined
  >(undefined);
  const [requested, setRequested] = useState(page === 1);
  const [visible, setVisible] = useState(page === 1);
  const [size, setSize] = useState<{ width: number; height: number }>();
  const [failure, setFailure] = useState("");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const node = host.current;
    if (!node) return;
    if (typeof IntersectionObserver === "undefined") {
      let active = true;
      queueMicrotask(() => {
        if (!active) return;
        setRequested(true);
        setVisible(true);
      });
      return () => {
        active = false;
      };
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) {
          setVisible(false);
          return;
        }
        setRequested(true);
        setVisible(true);
        onVisible(page);
      },
      { root: scrollport.current, rootMargin: "100% 0px" },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [onVisible, page, scrollport]);

  useEffect(() => () => textTask.current?.cancel(), []);

  useEffect(() => {
    if (!requested || (!visible && renderedZoom.current !== undefined)) return;
    if (renderedZoom.current === zoom && size) return;
    const target = canvas.current;
    const textHost = text.current;
    if (!target || !textHost) return;
    const controller = new AbortController();
    let newText: ReturnType<ReturnType<typeof pdfTextRenderer>> | undefined;
    renderQueue.current = renderQueue.current
      .then(async () => {
        controller.signal.throwIfAborted();
        const buffer = target.ownerDocument.createElement("canvas");
        try {
          const next = await renderPdfPage(
            document,
            page,
            buffer,
            controller.signal,
            (window.devicePixelRatio || 1) * zoom,
            textTask.current
              ? undefined
              : (pdfPage, viewport) => {
                  const task = pdfTextRenderer(textHost)(pdfPage, viewport);
                  newText = task;
                  return task;
                },
          );
          controller.signal.throwIfAborted();
          const context = target.getContext("2d");
          if (!context) throw new Error("PDF canvas has no 2D context");
          target.width = buffer.width;
          target.height = buffer.height;
          context.drawImage(buffer, 0, 0);
          textTask.current ??= newText;
          newText = undefined;
          renderedZoom.current = zoom;
          setSize(next);
          setFailure("");
        } finally {
          buffer.width = 0;
          buffer.height = 0;
          newText?.cancel();
        }
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted)
          setFailure(cause instanceof Error ? cause.message : String(cause));
      });
    return () => {
      controller.abort();
      newText?.cancel();
    };
  }, [attempt, document, page, requested, size, visible, zoom]);

  return (
    <div ref={host} className="w-full" data-pdf-page={page}>
      {failure ? (
        <div
          className="border-border-subtle text-muted-foreground type-body flex items-center justify-center gap-2 border p-3"
          role="alert"
        >
          <span>{failure}</span>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => setAttempt((value) => value + 1)}
          >
            {t("workspace.retry")}
          </Button>
        </div>
      ) : null}
      {!size && !failure ? (
        <div
          className="bg-muted mx-auto aspect-[3/4] w-full max-w-3xl"
          role="status"
        />
      ) : null}
      <div
        className="border-border-subtle relative mx-auto border bg-[var(--document-page-surface)]"
        style={{
          width: size ? `${size.width * zoom}px` : undefined,
          height: size ? `${size.height * zoom}px` : undefined,
          display: size ? undefined : "none",
        }}
      >
        <canvas
          ref={canvas}
          className="block"
          style={{ width: "100%", height: "100%" }}
        />
        <div ref={text} className="absolute inset-0 overflow-hidden" />
      </div>
    </div>
  );
}
