import { Workbook } from "@fortune-sheet/react";
import fortuneCss from "@fortune-sheet/react/dist/index.css?inline";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  openWorkspaceResourceScope,
  readWorkspaceResource,
  statWorkspaceResource,
} from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Spinner } from "@/shared/ui/spinner";
import { workspaceSingleFileBytesLimit } from "@neuxbot/platform-contracts/workspace-byte-limits";
import { excelFormat } from "./format";
import type { ExcelPreview as PreviewData } from "./model";
import { parseExcel } from "./parse";

type ExcelPreviewProps = {
  chatId?: string;
  rootPath: string;
  path: string;
  title: string;
};

const limits = {
  maxBytes: workspaceSingleFileBytesLimit,
  maxCells: 250_000,
  timeoutMs: 15_000,
} as const;
const scopedStyles = `@scope ([data-excel-preview]) { ${fortuneCss} }`;

type LoadState =
  | { key: string; kind: "ready"; value: PreviewData }
  | { key: string; kind: "error"; code: string };

/** 使用当前 Chat 文件字节、隔离解析器和只读 FortuneSheet 展示表格。 */
export function ExcelPreview({
  chatId,
  rootPath,
  path,
  title,
}: ExcelPreviewProps) {
  const { t, i18n } = useTranslation("chat");
  const [attempt, setAttempt] = useState(0);
  const [load, setLoad] = useState<LoadState>();
  const workbookRef = useRef<HTMLDivElement>(null);
  const key = `${chatId ?? ""}:${rootPath}:${path}:${attempt}`;
  const ready = load?.key === key && load.kind === "ready" ? load.value : null;
  const error = load?.key === key && load.kind === "error" ? load.code : null;

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        if (!chatId) throw new Error("scope");
        const format = excelFormat(path);
        const scope = await openWorkspaceResourceScope(chatId);
        const info = await statWorkspaceResource(scope.id, path, rootPath);
        if (!info) throw new Error("invalid");
        if (info.size > limits.maxBytes) throw new Error("tooLarge");
        const file = await readWorkspaceResource(
          scope.id,
          path,
          controller.signal,
          rootPath,
        );
        const preview = await parseExcel(
          Uint8Array.from(file.bytes),
          format,
          limits,
          controller.signal,
        );
        if (!controller.signal.aborted)
          setLoad({ key, kind: "ready", value: preview });
      } catch (cause) {
        if (!controller.signal.aborted)
          setLoad({
            key,
            kind: "error",
            code: cause instanceof Error ? cause.message : "invalid",
          });
      }
    })();
    return () => controller.abort();
  }, [chatId, key, path, rootPath]);

  useEffect(() => {
    if (!ready || !workbookRef.current) return;
    const observer = new ResizeObserver(() => {
      window.dispatchEvent(new Event("resize"));
    });
    observer.observe(workbookRef.current);
    return () => observer.disconnect();
  }, [ready]);

  const errorText =
    error === "tooLarge"
      ? t("workspace.excelTooLarge")
      : error === "timeout"
        ? t("workspace.excelTimeout")
        : error === "encoding"
          ? t("workspace.excelEncoding")
          : error === "scope"
            ? t("workspace.excelScopeRequired")
            : t("workspace.excelInvalid");
  const unsupported = ready?.unsupportedFeatures.map((feature) =>
    t(`workspace.excelFeature.${feature}`),
  );

  return (
    <section
      className="bg-surface flex h-full min-h-0 flex-col"
      data-excel-preview
      aria-label={title}
    >
      <style>{scopedStyles}</style>
      {error ? (
        <div
          className="text-muted-foreground type-body flex h-full flex-col items-center justify-center gap-3 px-4 text-center"
          role="alert"
        >
          <span>{errorText}</span>
          <Button
            size="sm"
            variant="secondary"
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
        <>
          {unsupported && unsupported.length > 0 ? (
            <p
              className="border-border-subtle bg-muted text-muted-foreground type-caption border-b px-3 py-2"
              role="note"
            >
              {t("workspace.excelUnsupported", {
                features: unsupported.join("、"),
              })}
            </p>
          ) : null}
          {ready.missingResults > 0 ? (
            <p
              className="border-border-subtle bg-muted text-muted-foreground type-caption border-b px-3 py-2"
              role="note"
            >
              {t("workspace.excelMissingResults", {
                count: ready.missingResults,
              })}
            </p>
          ) : null}
          <div ref={workbookRef} className="min-h-0 flex-1">
            <Workbook
              data={ready.sheets}
              lang={i18n.language.startsWith("zh") ? "zh" : "en"}
              allowEdit={false}
              showToolbar={false}
              showFormulaBar
              showSheetTabs
              forceCalculation={false}
              cellContextMenu={["copy"]}
              headerContextMenu={[]}
              sheetTabContextMenu={[]}
              filterContextMenu={[]}
            />
          </div>
        </>
      ) : null}
    </section>
  );
}
