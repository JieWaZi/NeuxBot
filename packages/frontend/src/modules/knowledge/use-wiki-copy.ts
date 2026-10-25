import { useTranslation } from "react-i18next";
import { i18n } from "@/shared/i18n";
import { knowledgeWikiEn, knowledgeWikiZh } from "@/shared/i18n/knowledge-wiki";

i18n.addResourceBundle("zh-CN", "knowledgeWiki", knowledgeWikiZh, true, true);
i18n.addResourceBundle("en-US", "knowledgeWiki", knowledgeWikiEn, true, true);
export function useWikiCopy() {
  const { t } = useTranslation("knowledgeWiki");
  return (
    key: keyof typeof knowledgeWikiZh,
    values?: Record<string, string | number>,
  ) => String(t(key, values));
}
