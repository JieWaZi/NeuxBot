export const themeModes = ["system", "light", "dark"] as const;

export type ThemeMode = (typeof themeModes)[number];
export type ResolvedTheme = Exclude<ThemeMode, "system">;

const themeStorageKey = "ally.ui.theme";
const darkThemeQuery = "(prefers-color-scheme: dark)";

function isThemeMode(value: unknown): value is ThemeMode {
  return themeModes.includes(value as ThemeMode);
}

export function readThemeMode(): ThemeMode {
  try {
    const value = window.localStorage.getItem(themeStorageKey);
    return isThemeMode(value) ? value : "system";
  } catch {
    return "system";
  }
}

export function resolveTheme(mode: ThemeMode): ResolvedTheme {
  if (mode !== "system") return mode;
  return window.matchMedia(darkThemeQuery).matches ? "dark" : "light";
}

export function applyTheme(mode: ThemeMode): ResolvedTheme {
  const resolvedTheme = resolveTheme(mode);
  document.documentElement.dataset.theme = resolvedTheme;
  document.documentElement.style.colorScheme = resolvedTheme;
  return resolvedTheme;
}

export function persistThemeMode(mode: ThemeMode) {
  try {
    window.localStorage.setItem(themeStorageKey, mode);
  } catch {
    // WebView 禁用本地存储时仍允许主题在当前会话生效。
  }
}

export function watchSystemTheme(onChange: () => void) {
  const mediaQuery = window.matchMedia(darkThemeQuery);
  mediaQuery.addEventListener("change", onChange);
  return () => mediaQuery.removeEventListener("change", onChange);
}

export function initializeTheme() {
  applyTheme(readThemeMode());
}
