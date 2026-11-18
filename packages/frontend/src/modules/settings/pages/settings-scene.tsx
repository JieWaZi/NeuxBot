import { lazy, Suspense, useSyncExternalStore, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import {
  SettingsContentShell,
  SettingsCard,
  SettingsGroup,
  SettingsRow,
} from "../components/settings-section-layout";
import { DesktopNotificationSettings } from "../components/desktop-notification-settings";
import { SettingsPageFooterHost } from "../components/settings-page-footer";
import { IntegrationBreadcrumbs } from "../components/integration-breadcrumbs";
import { type SettingsSection } from "../settings-navigation";
import { useTheme } from "@/shared/theme";
import { interfaceScales, isInterfaceScale } from "@/shared/theme/appearance";
import {
  getLanguageMode,
  setLanguageMode,
  subscribeLanguageMode,
} from "@/shared/i18n";
import { isLanguageMode } from "@/shared/i18n/language";
import { Switch } from "@/shared/ui/switch";
import { SceneBreadcrumbs } from "@/shared/ui/scene-breadcrumbs";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";
import { Skeleton } from "@/shared/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";

// 配置面板仅在对应分类打开时加载，不增加工作台首屏成本。
const RuntimeEnvironmentsSection = lazy(() =>
  import("../components/runtime-environments-section").then((module) => ({
    default: module.RuntimeEnvironmentsSection,
  })),
);
const IntegrationsSection = lazy(() =>
  import("../components/integrations-section").then((module) => ({
    default: module.IntegrationsSection,
  })),
);

const StorageSettings = lazy(() =>
  import("../components/storage-settings").then((module) => ({
    default: module.StorageSettings,
  })),
);

function GeneralSettings() {
  const { t } = useTranslation("appSettings");
  const {
    mode,
    setMode,
    scale,
    setScale,
    transparent,
    setTransparent,
    transparencySupported,
    appearanceError,
  } = useTheme();
  const language = useSyncExternalStore(subscribeLanguageMode, getLanguageMode);
  const modes = ["system", "light", "dark"] as const;
  return (
    <SettingsContentShell title={t("general.title")} showHeader={false}>
      <SettingsGroup title={t("general.appearance.title")}>
        <SettingsCard>
          <SettingsRow
            grouped
            title={t("general.appearance.theme")}
            description={t("general.appearance.description")}
          >
            <Select
              value={mode}
              onValueChange={(value) => {
                if (value === "system" || value === "light" || value === "dark")
                  setMode(value);
              }}
            >
              <SelectTrigger
                appearance="settings"
                size="settings"
                aria-label={t("theme.pickerAria")}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {modes.map((value) => (
                  <SelectItem key={value} value={value}>
                    {t(`theme.${value}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </SettingsRow>
          <SettingsRow
            grouped
            title={t("general.appearance.transparent")}
            description={t(
              transparencySupported
                ? "general.appearance.transparentHelp"
                : "general.appearance.transparentUnavailable",
            )}
          >
            <Switch
              aria-label={t("general.appearance.transparent")}
              checked={Boolean(transparent && transparencySupported)}
              disabled={!transparencySupported}
              onClick={() => setTransparent(!transparent)}
            />
          </SettingsRow>
          <SettingsRow grouped title={t("general.appearance.language")}>
            <Select
              value={language}
              onValueChange={(value) => {
                if (isLanguageMode(value)) setLanguageMode(value);
              }}
            >
              <SelectTrigger
                appearance="settings"
                size="settings"
                aria-label={t("general.appearance.language")}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="system">{t("theme.system")}</SelectItem>
                <SelectItem value="zh-CN">
                  {t("common:languages.chinese")}
                </SelectItem>
                <SelectItem value="en-US">
                  {t("common:languages.english")}
                </SelectItem>
              </SelectContent>
            </Select>
          </SettingsRow>
          <SettingsRow grouped title={t("general.appearance.scale")}>
            <Select
              value={String(scale ?? 1)}
              onValueChange={(value) => {
                const next = Number(value);
                if (isInterfaceScale(next)) setScale(next);
              }}
            >
              <SelectTrigger
                appearance="settings"
                size="settings"
                aria-label={t("general.appearance.scale")}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {interfaceScales.map((value) => (
                  <SelectItem key={value} value={String(value)}>
                    {Math.round(value * 100)}%
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </SettingsRow>
        </SettingsCard>
        {appearanceError ? (
          <p role="alert" className="type-body text-danger">
            {t("general.appearance.failed")}
          </p>
        ) : null}
      </SettingsGroup>
      <DesktopNotificationSettings />
    </SettingsContentShell>
  );
}

function SettingsLoading() {
  const { t } = useTranslation("appSettings");
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={t("loading")}
      className="grid gap-6"
    >
      <Skeleton className="h-row-detail w-full" />
      <div className="grid gap-2">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-32 w-full" />
      </div>
      <div className="grid gap-2">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="h-row-detail w-full" />
      </div>
    </div>
  );
}

export function SettingsScene({
  imIntegrationContent,
  integrationBreadcrumbs,
  onOpenRuntime,
  onRuntimeBack,
  runtimeId,
  section,
  showSceneChrome = true,
}: {
  onClose: () => void;
  onSelectSection?: (section: SettingsSection) => void;
  onOpenRuntime?: (runtimeId: string) => void;
  onRuntimeBack?: () => void;
  runtimeId?: string;
  section: SettingsSection;
  showSceneChrome?: boolean;
  imIntegrationContent?: ReactNode;
  integrationBreadcrumbs?: ReactNode;
}) {
  const { t } = useTranslation("appSettings");
  const sectionKey = {
    general: "general",
    storage: "storage",
    agents: "runtimeEnvironments",
    integrations: "integrations",
  }[section];

  return (
    <>
      <NativeOverlayPresence />
      <section
        aria-label={t("sceneLabel")}
        className="bg-surface-scene @container/settings-scene relative z-10 flex h-full min-h-0 flex-col overflow-hidden"
      >
        {showSceneChrome &&
        section !== "agents" &&
        section !== "integrations" ? (
          <SceneBreadcrumbs
            ariaLabel={t("sceneLabel")}
            items={[{ label: t(`sections.${sectionKey}`) }]}
          />
        ) : null}
        {showSceneChrome && section === "integrations" ? (
          <Suspense fallback={<IntegrationBreadcrumbs />}>
            {integrationBreadcrumbs || <IntegrationBreadcrumbs />}
          </Suspense>
        ) : null}
        <SettingsPageFooterHost>
          <div
            data-slot="settings-page-viewport"
            key={`${section}:${runtimeId ?? ""}`}
            className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto"
          >
            <div className="max-w-settings-content mx-auto w-full px-4 py-6 @[42.5rem]/settings-scene:px-8">
              <Suspense
                fallback={
                  <>
                    {showSceneChrome && section === "agents" ? (
                      <SceneBreadcrumbs
                        ariaLabel={t("sceneLabel")}
                        items={[{ label: t("sections.runtimeEnvironments") }]}
                      />
                    ) : null}
                    <SettingsLoading />
                  </>
                }
              >
                {section === "storage" ? <StorageSettings /> : null}
                {section === "general" ? <GeneralSettings /> : null}
                {section === "agents" ? (
                  <RuntimeEnvironmentsSection
                    runtimeId={runtimeId}
                    onOpenRuntime={onOpenRuntime ?? (() => undefined)}
                    onBack={onRuntimeBack ?? (() => undefined)}
                    showSceneChrome={showSceneChrome}
                  />
                ) : null}
                {section === "integrations" ? (
                  <IntegrationsSection imContent={imIntegrationContent} />
                ) : null}
              </Suspense>
            </div>
          </div>
        </SettingsPageFooterHost>
      </section>
    </>
  );
}
