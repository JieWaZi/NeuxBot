import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { PptxViewer as PptxViewerInstance } from "@aiden0z/pptx-renderer";

import { Button } from "@/shared/ui/button";
import { Spinner } from "@/shared/ui/spinner";
import { readOfficeBytes } from "./read-office-bytes";
import { officePreviewErrorKey } from "./office-preview-error";

type PptxPreviewProps = {
  chatId?: string;
  rootPath: string;
  path: string;
  title: string;
};

/** OFFICE PptxView 的按需渲染、分页与清理，读文件改走 NeuxBot Chat scope。 */
export function PptxPreview({
  chatId,
  rootPath,
  path,
  title,
}: PptxPreviewProps) {
  const { t } = useTranslation("chat");
  const hostRef = useRef<HTMLDivElement>(null);
  const viewerRef = useRef<PptxViewerInstance | null>(null);
  const generationRef = useRef(0);
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<
    | { status: "loading" }
    | { status: "ready"; count: number }
    | { status: "error"; key: ReturnType<typeof officePreviewErrorKey> }
  >({ status: "loading" });
  const [slide, setSlide] = useState(0);
  const [navigating, setNavigating] = useState(false);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const controller = new AbortController();
    const generation = ++generationRef.current;
    const active = () =>
      !controller.signal.aborted && generationRef.current === generation;
    void (async () => {
      try {
        const bytes = await readOfficeBytes(
          chatId,
          rootPath,
          path,
          controller.signal,
        );
        const { PptxViewer, RECOMMENDED_ZIP_LIMITS } =
          await import("@aiden0z/pptx-renderer");
        if (!active()) return;
        const viewer = await PptxViewer.open(bytes, host, {
          renderMode: "slide",
          fitMode: "contain",
          lazyMedia: true,
          lazySlides: true,
          pdfjs: false,
          signal: controller.signal,
          zipLimits: RECOMMENDED_ZIP_LIMITS,
          onSlideChange: (index) => {
            if (active()) setSlide(index);
          },
        });
        if (!active()) {
          viewer.destroy();
          return;
        }
        viewerRef.current = viewer;
        setSlide(viewer.currentSlideIndex);
        setLoad({ status: "ready", count: viewer.slideCount });
      } catch (cause) {
        if (!active()) return;
        try {
          viewerRef.current?.destroy();
        } catch {
          // A partially opened viewer can already be disposed.
        }
        viewerRef.current = null;
        host.replaceChildren();
        setLoad({
          status: "error",
          key: officePreviewErrorKey(cause),
        });
      }
    })();
    return () => {
      controller.abort();
      generationRef.current += 1;
      try {
        viewerRef.current?.destroy();
      } catch {
        // The viewer may already have failed during initialization.
      }
      viewerRef.current = null;
      host.replaceChildren();
    };
  }, [attempt, chatId, path, rootPath]);

  const navigate = (target: number) => {
    const viewer = viewerRef.current;
    if (!viewer || load.status !== "ready" || navigating) return;
    const next = Math.max(0, Math.min(load.count - 1, target));
    if (next === slide) return;
    const generation = generationRef.current;
    setNavigating(true);
    void viewer
      .goToSlide(next, { behavior: "auto" })
      .then(() => {
        if (
          generationRef.current === generation &&
          viewerRef.current === viewer
        )
          setSlide(viewer.currentSlideIndex);
      })
      .catch((cause: unknown) => {
        if (
          generationRef.current === generation &&
          viewerRef.current === viewer
        )
          setLoad({
            status: "error",
            key: officePreviewErrorKey(cause),
          });
      })
      .finally(() => {
        if (
          generationRef.current === generation &&
          viewerRef.current === viewer
        )
          setNavigating(false);
      });
  };

  return (
    <div
      className="flex h-full min-h-0 flex-col"
      aria-label={title}
      data-workbench-pptx
    >
      <div className="border-border bg-surface type-caption flex shrink-0 items-center justify-center gap-3 border-b px-3 py-2">
        <Button
          size="sm"
          variant="secondary"
          disabled={load.status !== "ready" || slide <= 0 || navigating}
          onClick={() => navigate(slide - 1)}
        >
          {t("workbench.previousSlide")}
        </Button>
        <span className="tabular-nums">
          {load.status === "ready" ? `${slide + 1} / ${load.count}` : "– / –"}
        </span>
        <Button
          size="sm"
          variant="secondary"
          disabled={
            load.status !== "ready" || slide >= load.count - 1 || navigating
          }
          onClick={() => navigate(slide + 1)}
        >
          {t("workbench.nextSlide")}
        </Button>
      </div>
      <div className="bg-surface-tertiary relative min-h-0 flex-1 overflow-hidden">
        <div ref={hostRef} className="h-full w-full" />
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
                setNavigating(false);
                setSlide(0);
                setLoad({ status: "loading" });
                setAttempt((value) => value + 1);
              }}
            >
              {t("workspace.retry")}
            </Button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
