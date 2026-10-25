import { i18n } from "@/shared/i18n";
import {
  knowledgeExtendedZh,
  knowledgeExtendedEn,
} from "@/shared/i18n/knowledge-extended-translations";

// 扩展模块初始化先于其首帧渲染，返回页面与语言切换复用同一资源。
i18n.addResourceBundle("zh-CN", "knowledge", knowledgeExtendedZh, true, true);
i18n.addResourceBundle("en-US", "knowledge", knowledgeExtendedEn, true, true);

export { useCopy } from "./use-copy";
