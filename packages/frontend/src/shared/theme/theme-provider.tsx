import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type PropsWithChildren,
} from "react";

import {
  applyTheme,
  persistThemeMode,
  readThemeMode,
  resolveTheme,
  watchSystemTheme,
  type ResolvedTheme,
  type ThemeMode,
} from "./theme";

type ThemeContextValue = {
  mode: ThemeMode;
  resolvedTheme: ResolvedTheme;
  setMode: (mode: ThemeMode) => void;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: PropsWithChildren) {
  const [mode, setModeState] = useState<ThemeMode>(readThemeMode);
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>(() =>
    resolveTheme(mode),
  );

  useEffect(() => {
    const syncTheme = () => setResolvedTheme(applyTheme(mode));
    syncTheme();
    if (mode !== "system") return;
    return watchSystemTheme(syncTheme);
  }, [mode]);

  const value = useMemo<ThemeContextValue>(
    () => ({
      mode,
      resolvedTheme,
      setMode(nextMode) {
        persistThemeMode(nextMode);
        setModeState(nextMode);
      },
    }),
    [mode, resolvedTheme],
  );

  return (
    <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
  );
}

export function useTheme() {
  const value = useContext(ThemeContext);
  if (!value) {
    throw new Error("useTheme 必须在 ThemeProvider 中使用");
  }
  return value;
}
