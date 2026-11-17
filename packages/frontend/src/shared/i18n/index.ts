import { commonZh, commonEn } from "./common";
import {
  persistLanguageMode,
  readLanguageMode,
  resolveLanguage,
  type LanguageMode,
} from "./language";
import i18n from "i18next";
import { initReactI18next } from "react-i18next";

const resources = {
  "zh-CN": { common: commonZh },
  "en-US": { common: commonEn },
} as const;

void i18n.use(initReactI18next).init({
  resources,
  lng: resolveLanguage(readLanguageMode()),
  supportedLngs: ["zh-CN", "en-US"],
  fallbackLng: "zh-CN",
  defaultNS: "common",
  interpolation: {
    escapeValue: false,
  },
});

export { i18n };

let languageMode = readLanguageMode();
const languageModeListeners = new Set<() => void>();

export function getLanguageMode(): LanguageMode {
  return languageMode;
}

export function subscribeLanguageMode(listener: () => void): () => void {
  languageModeListeners.add(listener);
  return () => languageModeListeners.delete(listener);
}

export function setLanguageMode(mode: LanguageMode): void {
  languageMode = mode;
  persistLanguageMode(mode);
  void i18n.changeLanguage(resolveLanguage(mode));
  for (const listener of languageModeListeners) listener();
}

/** 启动和系统语言变化共用同一语言解析；文档语言跟随实际展示语言。 */
export function initializeLanguage(): () => void {
  const applyDocumentLanguage = () => {
    document.documentElement.lang = i18n.resolvedLanguage ?? "zh-CN";
  };
  const onSystemLanguage = () => {
    if (languageMode === "system")
      void i18n.changeLanguage(resolveLanguage(languageMode));
  };
  i18n.on("languageChanged", applyDocumentLanguage);
  window.addEventListener("languagechange", onSystemLanguage);
  applyDocumentLanguage();
  return () => {
    i18n.off("languageChanged", applyDocumentLanguage);
    window.removeEventListener("languagechange", onSystemLanguage);
  };
}
