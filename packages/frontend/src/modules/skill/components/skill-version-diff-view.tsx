import { TextDiffView } from "@/shared/ui/text-diff-view";
/** 重型差异库只在用户查看版本时加载，弹窗结构由调用方同步挂载。 */
export function SkillVersionDiffView({
  currentContent,
  currentVersion,
  selectedContent,
  selectedVersion,
  path,
  theme,
}: {
  currentContent: string;
  currentVersion: string;
  selectedContent: string;
  selectedVersion: string;
  path: string;
  theme: "light" | "dark";
}) {
  return (
    <div className="border-border bg-surface overflow-hidden rounded-lg border">
      <TextDiffView
        beforeContent={currentContent}
        afterContent={selectedContent}
        beforeLabel={`${path} (v${currentVersion})`}
        afterLabel={`${path} (v${selectedVersion})`}
        language={languageOf(path)}
        theme={theme}
      />
    </div>
  );
}

function languageOf(path: string): string {
  if (path.endsWith(".md")) return "markdown";
  if (path.endsWith(".json")) return "json";
  if (path.endsWith(".yaml") || path.endsWith(".yml")) return "yaml";
  if (path.endsWith(".sh")) return "shell";
  return "text";
}
