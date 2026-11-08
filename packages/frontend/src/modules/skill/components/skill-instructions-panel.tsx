import { DetailSection } from "@/shared/ui/detail-section";
import { MarkdownContent } from "@/shared/ui/markdown-content";
import { useTranslation } from "react-i18next";

/** 与源端 SkillPreview 一致，隐藏 YAML 与仅重复技能名称的首标题。 */
export function SkillInstructionsPanel({
  content,
  name,
  embedded = false,
}: {
  content: string;
  name?: string;
  embedded?: boolean;
}) {
  const { t } = useTranslation("skill");
  const body = content
    .replace(/^\uFEFF?---[ \t]*\r?\n[\s\S]*?\r?\n---[ \t]*(?:\r?\n|$)/, "")
    .trim();
  const [first, ...rest] = body.split(/\r?\n/);
  const markdown =
    name &&
    /^#\s+/.test(first ?? "") &&
    first?.replace(/^#\s+/, "").trim() === name
      ? rest.join("\n").trim()
      : body;
  const document = (
    <div className="bg-resource-surface text-resource-content border-resource-border min-w-0 rounded-lg border px-6 py-4 [overflow-wrap:anywhere]">
      <MarkdownContent content={markdown} appearance="catalog-document" />
    </div>
  );
  return embedded ? (
    document
  ) : (
    <DetailSection appearance="catalog" title={t("detail.content")}>
      {document}
    </DetailSection>
  );
}
