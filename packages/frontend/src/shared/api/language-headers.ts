import { i18n } from "@/shared/i18n";

/** 请求发送时读取语言，生成配置和上传传输共用 ginx 的原生语言头。 */
export function apiLanguageHeaders(): Record<string, string> {
  const english = i18n.resolvedLanguage?.startsWith("en");
  return {
    "GinX-Lang-Header": english ? "en" : "zh",
    "Accept-Language": english ? "en-US" : "zh-CN",
  };
}
