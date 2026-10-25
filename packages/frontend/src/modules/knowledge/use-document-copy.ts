import { useTranslation } from "react-i18next";
import { i18n } from "@/shared/i18n";
import {
  knowledgeDocumentsZh,
  knowledgeDocumentsEn,
} from "@/shared/i18n/knowledge-documents";
i18n.addResourceBundle(
  "zh-CN",
  "knowledgeDocuments",
  knowledgeDocumentsZh,
  true,
  true,
);
i18n.addResourceBundle(
  "en-US",
  "knowledgeDocuments",
  knowledgeDocumentsEn,
  true,
  true,
);
export function useDocumentCopy() {
  const { t } = useTranslation("knowledgeDocuments");
  return (
    key: keyof typeof knowledgeDocumentsZh,
    values?: Record<string, string | number>,
  ) => String(t(key, values));
}
