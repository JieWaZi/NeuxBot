import { DiffFile } from "@git-diff-view/file";
import { DiffModeEnum, DiffView } from "@git-diff-view/react";
import "@git-diff-view/react/styles/diff-view-pure.css";
import {
  AlignJustify,
  ChevronDown,
  ChevronRight,
  Columns2,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { useTheme } from "@/shared/theme";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";

type Props = {
  path: string;
  patch: string;
  fullHeight?: boolean;
  diffMode?: DiffModeEnum;
  onDiffModeChange?: (mode: DiffModeEnum) => void;
};

export function GitPatchDiffModeToggle({
  mode,
  onModeChange,
}: {
  mode: DiffModeEnum;
  onModeChange: (mode: DiffModeEnum) => void;
}) {
  const { t } = useTranslation("chat");
  const split = mode === DiffModeEnum.Split;
  const nextMode = split ? DiffModeEnum.Unified : DiffModeEnum.Split;
  const label = split ? t("workspace.diffUnified") : t("workspace.diffSplit");
  return (
    <Button
      size="icon-sm"
      variant="ghost"
      aria-label={label}
      title={label}
      onClick={() => onModeChange(nextMode)}
    >
      {split ? <AlignJustify /> : <Columns2 />}
    </Button>
  );
}

/** 补丁视图复用共享 Diff 引擎，并在打开时按需加载。 */
export function GitPatchPreview({
  path,
  patch,
  fullHeight = false,
  diffMode: controlledDiffMode,
}: Props) {
  const { resolvedTheme } = useTheme();
  const mode = controlledDiffMode ?? DiffModeEnum.Unified;
  const files = useMemo(() => {
    const chunks = patch
      .split(/(?=^diff --git )/m)
      .filter((chunk) => chunk.startsWith("diff --git "));
    return chunks.map((chunk, index) => {
      const label = /^\+\+\+ b\/(.+)$/m.exec(chunk)?.[1] ?? path;
      let added = 0;
      let removed = 0;
      for (const line of chunk.split("\n")) {
        if (line.startsWith("+") && !line.startsWith("+++")) added++;
        if (line.startsWith("-") && !line.startsWith("---")) removed++;
      }
      return { label, chunk, index, added, removed };
    });
  }, [patch, path]);

  return (
    <div
      className={cn("min-w-0", fullHeight && "flex h-full min-h-0 flex-col")}
    >
      <div
        className={cn(
          "scrollbar-hidden min-w-0 overflow-auto",
          fullHeight ? "min-h-0 flex-1" : "max-h-96",
        )}
      >
        {files.map(({ label, chunk, index, added, removed }) => (
          <GitPatchFile
            key={`${path}:${index}:${chunk.length}`}
            label={label}
            chunk={chunk}
            index={index}
            path={path}
            mode={mode}
            theme={resolvedTheme}
            initiallyExpanded={files.length === 1 && chunk.length < 75_000}
            added={added}
            removed={removed}
          />
        ))}
      </div>
    </div>
  );
}

function GitPatchFile({
  label,
  chunk,
  index,
  path,
  mode,
  theme,
  initiallyExpanded,
  added,
  removed,
}: {
  label: string;
  chunk: string;
  index: number;
  path: string;
  mode: DiffModeEnum;
  theme: "light" | "dark";
  initiallyExpanded: boolean;
  added: number;
  removed: number;
}) {
  const [expanded, setExpanded] = useState(initiallyExpanded);
  const file = useMemo(() => {
    if (!expanded) return null;
    const result = new DiffFile(
      label,
      "",
      label,
      "",
      [chunk],
      undefined,
      undefined,
      `${path}:${index}`,
    );
    result.initTheme(theme);
    result.init();
    if (mode === DiffModeEnum.Split) result.buildSplitDiffLines();
    else result.buildUnifiedDiffLines();
    return result;
  }, [chunk, expanded, index, label, mode, path, theme]);
  return (
    <section aria-label={label} className="border-border-subtle mb-2 border-b">
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((value) => !value)}
        className="hover:bg-muted focus-visible:ring-ring type-caption min-h-row-default flex w-full min-w-0 items-center gap-2 px-2 text-left outline-none focus-visible:ring-2"
      >
        {expanded ? (
          <ChevronDown className="size-icon-sm shrink-0" />
        ) : (
          <ChevronRight className="size-icon-sm shrink-0" />
        )}
        <span className="min-w-0 flex-1 truncate" title={label}>
          {label}
        </span>
        <span className="text-success">+{added}</span>
        <span className="text-destructive">-{removed}</span>
      </button>
      {file ? (
        <DiffView
          diffFile={file}
          diffViewMode={mode}
          diffViewTheme={theme}
          diffViewHighlight={chunk.length < 40_000}
          diffViewWrap={false}
          diffViewFontSize={12}
          className="workspace-diff-view [&_.diff-table-scroll-container]:scrollbar-hidden"
        />
      ) : null}
    </section>
  );
}
