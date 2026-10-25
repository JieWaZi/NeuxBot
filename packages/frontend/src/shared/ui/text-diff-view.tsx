import { generateDiffFile } from "@git-diff-view/file";
import { DiffModeEnum, DiffView } from "@git-diff-view/react";
import "@git-diff-view/react/styles/diff-view-pure.css";
import { useMemo } from "react";

/** 技能与 Wiki 共用文本差异呈现；调用方按需加载，避免进入启动依赖。 */
export function TextDiffView({
  beforeContent,
  afterContent,
  beforeLabel,
  afterLabel,
  language = "text",
  theme,
}: {
  beforeContent: string;
  afterContent: string;
  beforeLabel: string;
  afterLabel: string;
  language?: string;
  theme: "light" | "dark";
}) {
  const diffFile = useMemo(() => {
    const file = generateDiffFile(
      beforeLabel,
      beforeContent,
      afterLabel,
      afterContent,
      language,
      language,
    );
    file.initTheme(theme);
    file.init();
    file.buildUnifiedDiffLines();
    return file;
  }, [beforeContent, afterContent, beforeLabel, afterLabel, language, theme]);
  return (
    <DiffView
      diffFile={diffFile}
      diffViewMode={DiffModeEnum.Unified}
      diffViewTheme={theme}
      diffViewHighlight={beforeContent.length + afterContent.length < 100000}
      diffViewWrap
      diffViewFontSize={12}
    />
  );
}
