import { lazy, Suspense, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import {
  SettingsContentShell,
  SettingsGroup,
  SettingsRow,
} from "../components/settings-section-layout";
import { DesktopNotificationSettings } from "../components/desktop-notification-settings";
import { SettingsPageFooterHost } from "../components/settings-page-footer";
import { IntegrationBreadcrumbs } from "../components/integration-breadcrumbs";
import { type SettingsSection } from "../settings-navigation";
import { useTheme } from "@/shared/theme";
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

function GeneralSettings() {
  const { t } = useTranslation("appSettings");
  const { mode, setMode } = useTheme();
  const modes = ["system", "light", "dark"] as const;
  return (
    <SettingsContentShell title={t("general.title")} showHeader={false}>
      <SettingsGroup title={t("general.appearance.title")}>
        <SettingsRow
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
      </SettingsGroup>
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
    notifications: "notifications",
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
                {section === "general" ? <GeneralSettings /> : null}
                {section === "notifications" ? (
                  <DesktopNotificationSettings />
                ) : null}
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
