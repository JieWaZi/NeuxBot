export const languageModes = ["system", "zh-CN", "en-US"] as const;
export type LanguageMode = (typeof languageModes)[number];
export type AppLanguage = Exclude<LanguageMode, "system">;
const storageKey = "bot.ui.language";

export function isLanguageMode(value: unknown): value is LanguageMode {
  return languageModes.some((mode) => mode === value);
}

export function readLanguageMode(): LanguageMode {
  try {
    const value = localStorage.getItem(storageKey);
    return isLanguageMode(value) ? value : "system";
  } catch {
    return "system";
  }
}

export function resolveLanguage(mode: LanguageMode): AppLanguage {
  if (mode !== "system") return mode;
  const languages =
    typeof navigator === "undefined"
      ? []
      : navigator.languages?.length
        ? navigator.languages
        : [navigator.language];
  for (const language of languages) {
    if (/^zh(?:-|$)/i.test(language)) return "zh-CN";
    if (/^en(?:-|$)/i.test(language)) return "en-US";
  }
  return "zh-CN";
}

export function persistLanguageMode(mode: LanguageMode): void {
  try {
    localStorage.setItem(storageKey, mode);
  } catch {
    // 存储不可用时仍允许当前会话切换语言。
  }
}
