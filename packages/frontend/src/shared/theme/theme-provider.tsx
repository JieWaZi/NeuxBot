import { i18n } from "@/shared/i18n";
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type PropsWithChildren,
} from "react";
import { useTranslation } from "react-i18next";
import { platform } from "@/shared/platform/provider";
import { logger } from "@/shared/diagnostics/logger";
import {
  persistAppearance,
  isInterfaceScale,
  readAppearance,
  type AppearancePreferences,
  type InterfaceScale,
} from "./appearance";

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
  scale: InterfaceScale;
  setScale: (scale: InterfaceScale) => void;
  transparent: boolean;
  setTransparent: (transparent: boolean) => void;
  transparencySupported: boolean;
  appearanceError: boolean;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: PropsWithChildren) {
  const { i18n } = useTranslation();
  const language = i18n.resolvedLanguage?.startsWith("en") ? "en-US" : "zh-CN";
  const [appearance, setAppearance] = useState(readAppearance);
  const [transparent, setTransparent] = useState(false);
  const [appearanceReady, setAppearanceReady] = useState(
    () => !platform().host.get_window_appearance,
  );
  const [transparencySupported, setTransparencySupported] = useState(false);
  const [appearanceError, setAppearanceError] = useState(false);
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

  useEffect(() => {
    const read = platform().host.get_window_appearance;
    if (!read) return;
    let active = true;
    void read().then(
      (value) => {
        if (!active) return;
        setTransparent(value.transparent);
        setTransparencySupported(value.transparencySupported);
        setAppearanceReady(true);
      },
      (error: unknown) => {
        if (!active) return;
        setAppearanceError(true);
        logger.warn("Window appearance could not be read", { error });
      },
    );
    return () => {
      active = false;
    };
  }, []);

  useEffect(
    () =>
      platform().host.onInterfaceScale?.((scale) => {
        if (!isInterfaceScale(scale)) return;
        const next: AppearancePreferences = { ...appearance, scale };
        persistAppearance(next);
        setAppearance(next);
      }),
    [appearance],
  );

  useEffect(() => {
    const configure = platform().host.set_window_appearance;
    let active = true;
    if (!configure) {
      document.documentElement.style.zoom = String(appearance.scale);
      delete document.documentElement.dataset.windowTransparent;
      return;
    }
    if (!appearanceReady) return;
    document.documentElement.style.removeProperty("zoom");
    void configure({ ...appearance, theme: mode, language, transparent }).then(
      ({ transparencySupported: supported }) => {
        if (!active) return;
        setAppearanceError(false);
        setTransparencySupported(supported);
        document.documentElement.dataset.windowTransparent = String(
          supported && transparent,
        );
      },
      (error: unknown) => {
        if (!active) return;
        setAppearanceError(true);
        logger.warn("Window appearance could not be applied", { error });
      },
    );
    return () => {
      active = false;
    };
  }, [appearance, resolvedTheme, mode, language, transparent, appearanceReady]);

  const value = useMemo<ThemeContextValue>(
    () => ({
      mode,
      resolvedTheme,
      ...appearance,
      transparent,
      transparencySupported,
      appearanceError,
      setScale(scale) {
        const next: AppearancePreferences = { ...appearance, scale };
        persistAppearance(next);
        setAppearance(next);
      },
      setTransparent(transparent) {
        setTransparent(transparent);
      },
      setMode(nextMode) {
        persistThemeMode(nextMode);
        setModeState(nextMode);
      },
    }),
    [
      mode,
      resolvedTheme,
      appearance,
      transparent,
      transparencySupported,
      appearanceError,
    ],
  );

  return (
    <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
  );
}

export function useTheme() {
  const value = useContext(ThemeContext);
  if (!value) {
    throw new Error(i18n.t("common:errors.themeProviderMissing"));
  }
  return value;
}
