import { DetailSection } from "@/shared/ui/detail-section";
import { MarkdownContent } from "@/shared/ui/markdown-content";
import { useTranslation } from "react-i18next";

/** 元数据由资料区展示，正文预览隐藏闭合的 YAML 头部。 */
export function SkillInstructionsPanel({ content }: { content: string }) {
  const { t } = useTranslation("skill");
  const markdown = content.replace(
    /^\uFEFF?---[ \t]*\r?\n[\s\S]*?\r?\n---[ \t]*(?:\r?\n|$)/,
    "",
  );
  return (
    <DetailSection title={t("detail.content")}>
      <MarkdownContent content={markdown} />
    </DetailSection>
  );
}
