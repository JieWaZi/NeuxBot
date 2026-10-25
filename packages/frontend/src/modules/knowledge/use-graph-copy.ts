import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { i18n } from "@/shared/i18n";
import {
  knowledgeGraphEn,
  knowledgeGraphZh,
} from "@/shared/i18n/knowledge-graph";

i18n.addResourceBundle("zh-CN", "knowledgeGraph", knowledgeGraphZh, true, true);
i18n.addResourceBundle("en-US", "knowledgeGraph", knowledgeGraphEn, true, true);
export function useGraphCopy() {
  const { t } = useTranslation("knowledgeGraph");
  return useCallback(
    (
      key: keyof typeof knowledgeGraphZh,
      values?: Record<string, string | number>,
    ) => String(t(key, values)),
    [t],
  );
}

export function graphPageType(type: string) {
  switch (type) {
    case "concept":
    case "entity":
    case "summary":
    case "synthesis":
    case "comparison":
    case "index":
    case "topic":
      return type;
    default:
      return "other";
  }
}

/** 图形颜色使用应用语义色，主题切换无需重新创建画布。 */
export function graphNodeColor(type: string) {
  switch (type) {
    case "entity":
      return "text-status-success-content";
    case "concept":
      return "text-status-warning-content";
    case "summary":
      return "text-primary";
    case "synthesis":
      return "text-status-info-content";
    case "comparison":
      return "text-status-danger-content";
    case "index":
      return "text-content-secondary";
    default:
      return "text-primary";
  }
}
