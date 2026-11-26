import { DiffFile } from "@git-diff-view/file";
import { DiffModeEnum, DiffView } from "@git-diff-view/react";
import "@git-diff-view/react/styles/diff-view-pure.css";
import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { useTheme } from "@/shared/theme";
import { Button } from "@/shared/ui/button";

export type DiffMode = "split" | "unified";

const patchPageLineLimit = 300;
const hunkHeader = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$/;

function* splitHunkRows(rows: string[], firstPageRoom: number) {
  let pageRoom = firstPageRoom;
  let segment: string[] = [];

  for (let index = 0; index < rows.length;) {
    const row = rows[index];
    if (row === undefined) break;
    if (!row.startsWith("-")) {
      segment.push(row);
      index++;
    } else {
      let oldEnd = index;
      while (rows[oldEnd]?.startsWith("-")) oldEnd++;
      let newEnd = oldEnd;
      while (rows[newEnd]?.startsWith("+")) newEnd++;

      // 同一替换块的两侧一起分段，并排模式仍能对照新旧行。
      let oldIndex = index;
      let newIndex = oldEnd;
      while (oldIndex < oldEnd || newIndex < newEnd) {
        const room = pageRoom - segment.length;
        const oldRemaining = oldEnd - oldIndex;
        const newRemaining = newEnd - newIndex;
        const newCount = Math.min(
          newRemaining,
          room - Math.min(oldRemaining, Math.ceil(room / 2)),
        );
        const oldCount = Math.min(oldRemaining, room - newCount);
        segment.push(
          ...rows.slice(oldIndex, oldIndex + oldCount),
          ...rows.slice(newIndex, newIndex + newCount),
        );
        oldIndex += oldCount;
        newIndex += newCount;
        if (segment.length === pageRoom) {
          yield segment;
          segment = [];
          pageRoom = patchPageLineLimit;
        }
      }
      index = newEnd;
    }

    if (segment.length === pageRoom) {
      yield segment;
      segment = [];
      pageRoom = patchPageLineLimit;
    }
  }
  if (segment.length) yield segment;
}

function paginatePatch(patch: string) {
  const lines = patch.split("\n");
  if (lines.length <= patchPageLineLimit) return [patch];

  const firstHunk = lines.findIndex((line) => hunkHeader.test(line));
  if (firstHunk === -1) return [patch];

  const fileHeader = lines.slice(0, firstHunk);
  const pages: string[] = [];
  let page: string[] = [];
  let pageSize = 0;
  const finishPage = () => {
    pages.push([...fileHeader, ...page, ""].join("\n"));
    page = [];
    pageSize = 0;
  };

  for (let index = firstHunk; index < lines.length;) {
    const header = hunkHeader.exec(lines[index++] ?? "");
    if (!header) break;
    const [, oldStart, oldLength, newStart, newLength, sectionTitle] = header;
    let oldLine = Number(oldStart) + Number(oldLength === "0");
    let newLine = Number(newStart) + Number(newLength === "0");
    const rows: string[] = [];
    while (index < lines.length) {
      const row = lines[index];
      if (row === undefined || hunkHeader.test(row)) break;
      index++;
      if (row.startsWith("\\ ") && rows.length) {
        rows[rows.length - 1] += `\n${row}`;
      } else if (/^[ +-]/.test(row)) {
        rows.push(row);
      }
    }

    for (const segment of splitHunkRows(rows, patchPageLineLimit - pageSize)) {
      const oldCount = segment.filter((row) => !row.startsWith("+")).length;
      const newCount = segment.filter((row) => !row.startsWith("-")).length;
      // Unified Diff 的空行范围使用插入或删除点之前的行号。
      const oldRange = `${oldLine - Number(oldCount === 0)},${oldCount}`;
      const newRange = `${newLine - Number(newCount === 0)},${newCount}`;
      page.push(`@@ -${oldRange} +${newRange} @@${sectionTitle}`, ...segment);
      oldLine += oldCount;
      newLine += newCount;
      pageSize += segment.length;
      if (pageSize === patchPageLineLimit) finishPage();
    }
  }
  if (pageSize) finishPage();
  return pages.length > 1 ? pages : [patch];
}

/** 此模块在已展开文件的补丁就绪后加载 Diff 引擎。 */
export function GitPatchBody({
  path,
  patch,
  mode,
  wrap,
}: {
  path: string;
  patch: string;
  mode: DiffMode;
  wrap: boolean;
}) {
  const { resolvedTheme } = useTheme();
  const { t } = useTranslation("chat");
  const [page, setPage] = useState(0);
  const pageTop = useRef<HTMLDivElement>(null);
  const pages = useMemo(() => paginatePatch(patch), [patch]);
  const pageIndex = Math.min(page, pages.length - 1);
  const visiblePatch = pages[pageIndex] ?? patch;
  const file = useMemo(() => {
    // 默认按内容标识缓存，同一路径的不同区段不能共用补全文本。
    const result = new DiffFile(path, "", path, "", [visiblePatch]);
    result.initTheme(resolvedTheme);
    result.init();
    if (mode === "split") result.buildSplitDiffLines();
    else result.buildUnifiedDiffLines();
    return result;
  }, [mode, path, resolvedTheme, visiblePatch]);

  const view = (
    <DiffView
      key={pageIndex}
      diffFile={file}
      diffViewMode={
        mode === "split" ? DiffModeEnum.Split : DiffModeEnum.Unified
      }
      diffViewTheme={resolvedTheme}
      diffViewHighlight={patch.length < 40_000}
      diffViewWrap={wrap}
      diffViewFontSize={12}
      className="workspace-diff-view [&_.diff-table-scroll-container]:scrollbar-hidden"
    />
  );

  if (pages.length === 1) return view;

  const changePage = (nextPage: number) => {
    setPage(nextPage);
    pageTop.current?.scrollIntoView?.({ block: "start" });
  };

  return (
    <div ref={pageTop} className="min-w-0">
      <nav
        aria-label={t("workspace.git.diffPreview")}
        className="bg-surface-panel sticky top-0 z-20 flex flex-wrap items-center justify-between gap-1 px-3 py-1"
      >
        <Button
          type="button"
          size="sm"
          variant="ghost"
          disabled={pageIndex === 0}
          onClick={() => changePage(pageIndex - 1)}
        >
          {t("workspace.git.patchPrevious")}
        </Button>
        <span
          className="text-content-muted type-caption tabular-nums"
          role="status"
        >
          {t("workspace.git.patchPage", {
            page: pageIndex + 1,
            total: pages.length,
          })}
        </span>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          disabled={pageIndex === pages.length - 1}
          onClick={() => changePage(pageIndex + 1)}
        >
          {t("workspace.git.patchNext")}
        </Button>
      </nav>
      {view}
    </div>
  );
}
