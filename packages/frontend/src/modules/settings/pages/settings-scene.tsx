import { Laptop, Moon, Settings, Sun } from "lucide-react";
import { lazy, Suspense, useMemo, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import {
  SettingsContentShell,
  SettingsRow,
} from "../components/settings-section-layout";
import { type SettingsSection } from "../settings-navigation";
import { useTheme, type ThemeMode } from "@/shared/theme";
import { SceneTabStrip } from "@/shared/ui/scene-tab-strip";
import { SceneChromePortal } from "@/shared/ui/scene-chrome-portal";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";
import { SegmentedControl } from "@/shared/ui/segmented-control";

// 配置面板在用户打开对应设置时加载，不增加双端工作台首屏成本。
const AIAccountsSection = lazy(() =>
  import("../components/ai-accounts-section").then((module) => ({
    default: module.AIAccountsSection,
  })),
);
const RuntimeEnvironmentsSection = lazy(() =>
  import("../components/runtime-environments-section").then((module) => ({
    default: module.RuntimeEnvironmentsSection,
  })),
);

const APIIntegrationSection = lazy(() =>
  import("../components/api-integration-section").then((module) => ({
    default: module.APIIntegrationSection,
  })),
);

function GeneralSettings() {
  const { t } = useTranslation("appSettings");
  const { mode, setMode } = useTheme();
  const themeOptions = useMemo(
    () =>
      [
        {
          value: "system",
          label: t("theme.system"),
          icon: <Laptop className="size-icon-sm" strokeWidth={1.5} />,
        },
        {
          value: "light",
          label: t("theme.light"),
          icon: <Sun className="size-icon-sm" strokeWidth={1.5} />,
        },
        {
          value: "dark",
          label: t("theme.dark"),
          icon: <Moon className="size-icon-sm" strokeWidth={1.5} />,
        },
      ] as const satisfies ReadonlyArray<{
        value: ThemeMode;
        label: string;
        icon: ReactNode;
      }>,
    [t],
  );

  return (
    <SettingsContentShell
      title={t("general.title")}
      description={t("general.description")}
    >
      <SettingsRow
        title={t("general.appearance.title")}
        description={t("general.appearance.description")}
      >
        <SegmentedControl
          className="max-w-full"
          ariaLabel={t("theme.pickerAria")}
          value={mode}
          options={themeOptions}
          onValueChange={setMode}
        />
      </SettingsRow>
    </SettingsContentShell>
  );
}

function SettingsSectionContent({
  onSelectSection,
  section,
}: {
  onSelectSection?: (section: SettingsSection) => void;
  section: SettingsSection;
}) {
  const { t } = useTranslation("appSettings");
  if (section === "integration-api")
    return (
      <Suspense
        fallback={
          <SettingsContentShell title={t("sections.integrationAPI")}>
            <div
              role="status"
              aria-busy="true"
              className="bg-surface-tertiary h-40 rounded-lg"
            />
          </SettingsContentShell>
        }
      >
        <APIIntegrationSection />
      </Suspense>
    );
  if (section === "general") return <GeneralSettings />;
  if (section === "ai-accounts") return <AIAccountsSection />;
  return (
    <RuntimeEnvironmentsSection
      onOpenAccounts={
        onSelectSection ? () => onSelectSection("ai-accounts") : undefined
      }
    />
  );
}

export function SettingsScene({
  onClose,
  onSelectSection,
  section,
  showSceneChrome = true,
}: {
  onClose: () => void;
  onSelectSection?: (section: SettingsSection) => void;
  section: SettingsSection;
  showSceneChrome?: boolean;
}) {
  const { t } = useTranslation("appSettings");

  return (
    <>
      <NativeOverlayPresence />
      <section
        aria-label={t("sceneLabel")}
        className="bg-surface-scene @container/settings-scene relative z-10 flex h-full min-h-0 flex-col overflow-hidden"
      >
        {showSceneChrome ? (
          <SceneChromePortal>
            <SceneTabStrip
              activeTabId="settings"
              ariaLabel={t("sceneTabList")}
              tabs={[
                {
                  id: "settings",
                  label: t("title"),
                  icon: Settings,
                  closeable: true,
                  autoFocus: true,
                },
              ]}
              onClose={onClose}
              onSelect={() => undefined}
            />
          </SceneChromePortal>
        ) : null}
        <div className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto px-4 py-6 @[42.5rem]/settings-scene:px-8 @[42.5rem]/settings-scene:py-8">
          <Suspense
            fallback={
              <div
                aria-busy="true"
                className="bg-surface-tertiary h-40 rounded-lg"
              />
            }
          >
            <SettingsSectionContent
              section={section}
              onSelectSection={onSelectSection}
            />
          </Suspense>
        </div>
      </section>
    </>
  );
}
