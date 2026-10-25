import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { i18n } from "@/shared/i18n";
import { knowledgeZh, knowledgeEn } from "@/shared/i18n/knowledge";
import type { knowledgeExtendedZh } from "@/shared/i18n/knowledge-extended-translations";

// 模块初始化先于首屏渲染；首次直链、切换语言和返回均使用同步基础词典。
i18n.addResourceBundle("zh-CN", "knowledge", knowledgeZh, true, true);
i18n.addResourceBundle("en-US", "knowledge", knowledgeEn, true, true);

export function useCopy() {
  const { t } = useTranslation("knowledge");
  return useCallback(
    (
      key: keyof typeof knowledgeZh | keyof typeof knowledgeExtendedZh,
      values?: Record<string, string | number>,
    ) => String(t(key, values)),
    [t],
  );
}
