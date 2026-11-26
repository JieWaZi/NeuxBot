import { DiffFile } from "@git-diff-view/file";
import { DiffModeEnum, DiffView } from "@git-diff-view/react";
import "@git-diff-view/react/styles/diff-view-pure.css";
import { useMemo } from "react";

import { useTheme } from "@/shared/theme";

export type DiffMode = "split" | "unified";

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
  const file = useMemo(() => {
    const result = new DiffFile(
      path,
      "",
      path,
      "",
      [patch],
      undefined,
      undefined,
      path,
    );
    result.initTheme(resolvedTheme);
    result.init();
    if (mode === "split") result.buildSplitDiffLines();
    else result.buildUnifiedDiffLines();
    return result;
  }, [mode, patch, path, resolvedTheme]);

  return (
    <DiffView
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
}
