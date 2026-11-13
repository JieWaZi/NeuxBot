import type {
  HostService,
  NotificationPreferences,
} from "@neuxbot/platform-contracts/services";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { platform } from "@/shared/platform/provider";
import { Button } from "@/shared/ui/button";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Skeleton } from "@/shared/ui/skeleton";
import { Switch } from "@/shared/ui/switch";
import {
  SettingsCard,
  SettingsContentShell,
  SettingsGroup,
  SettingsRow,
} from "./settings-section-layout";

type NotificationHost = {
  get: NonNullable<HostService["get_notification_preferences"]>;
  set: NonNullable<HostService["set_notification_preferences"]>;
  sendTest?: HostService["send_test_notification"];
  openSystemSettings?: HostService["open_notification_settings"];
};
const preferencesKey = ["desktop-notification-preferences"] as const;
const modes = ["all", "needs-me", "off"] as const;

export function DesktopNotificationSettings() {
  const { t } = useTranslation("notification");
  const host = platform().host;
  return (
    <SettingsContentShell title={t("preferences.title")} showHeader={false}>
      {host.get_notification_preferences &&
      host.set_notification_preferences ? (
        <NotificationSettings
          get={host.get_notification_preferences}
          set={host.set_notification_preferences}
          sendTest={host.send_test_notification}
          openSystemSettings={host.open_notification_settings}
        />
      ) : (
        <SettingsRow title={t("preferences.desktopOnly")} />
      )}
    </SettingsContentShell>
  );
}

function NotificationSettings({
  get,
  set,
  sendTest,
  openSystemSettings,
}: NotificationHost) {
  const { t } = useTranslation("notification");
  const client = useQueryClient();
  const query = useQuery({
    queryKey: preferencesKey,
    queryFn: () => get(),
    retry: false,
  });
  const change = useMutation({
    mutationFn: (next: NotificationPreferences) => set(next),
    onSuccess: (data) => client.setQueryData(preferencesKey, data),
  });
  const test = useMutation({ mutationFn: async () => sendTest?.() });
  const systemSettings = useMutation({
    mutationFn: async () => openSystemSettings?.(),
  });
  const settings = query.data;

  return (
    <>
      <SettingsRow
        title={t("preferences.mode")}
        aria-busy={query.isPending || undefined}
      >
        {settings ? (
          <SegmentedControl
            ariaLabel={t("preferences.mode")}
            value={settings.mode}
            disabled={change.isPending}
            options={modes.map((mode) => ({
              value: mode,
              label: t(`preferences.${mode}`),
            }))}
            onValueChange={(mode) => {
              if (mode !== settings.mode) change.mutate({ ...settings, mode });
            }}
          />
        ) : query.isPending ? (
          <div role="status" aria-label={t("preferences.loading")}>
            <Skeleton className="h-control-md w-44" />
          </div>
        ) : (
          <Button
            variant="ghost"
            size="settings"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            {t("preferences.retry")}
          </Button>
        )}
      </SettingsRow>
      <SettingsGroup title={t("preferences.device")}>
        <SettingsCard>
          <SettingsRow
            grouped
            title={t("preferences.desktop")}
            description={t("preferences.desktopHelp")}
          >
            {settings ? (
              <Switch
                aria-label={t("preferences.desktop")}
                hitArea="comfortable"
                checked={settings.enabled}
                disabled={change.isPending}
                onClick={() =>
                  change.mutate({ ...settings, enabled: !settings.enabled })
                }
              />
            ) : query.isPending ? (
              <Skeleton className="size-control-md" />
            ) : null}
          </SettingsRow>
          <SettingsRow
            grouped
            title={t("preferences.preview")}
            description={t("preferences.previewHelp")}
          >
            {settings ? (
              <Switch
                aria-label={t("preferences.preview")}
                hitArea="comfortable"
                checked={settings.preview}
                disabled={
                  !settings.enabled ||
                  settings.mode === "off" ||
                  change.isPending
                }
                onClick={() =>
                  change.mutate({ ...settings, preview: !settings.preview })
                }
              />
            ) : query.isPending ? (
              <Skeleton className="size-control-md" />
            ) : null}
          </SettingsRow>
          {sendTest ? (
            <SettingsRow
              grouped
              title={t("preferences.test")}
              description={t("preferences.testHelp")}
              content={
                test.isError || systemSettings.isError ? (
                  <div role="alert" className="text-destructive type-ui mt-2">
                    {test.isError ? <p>{t("preferences.testFailed")}</p> : null}
                    {systemSettings.isError ? (
                      <p>{t("preferences.openSettingsFailed")}</p>
                    ) : null}
                  </div>
                ) : null
              }
            >
              {openSystemSettings ? (
                <Button
                  variant="ghost"
                  size="settings"
                  disabled={systemSettings.isPending}
                  onClick={() => systemSettings.mutate()}
                >
                  {t("preferences.openSystemSettings")}
                </Button>
              ) : null}
              <Button
                variant="settings-outline"
                size="settings"
                disabled={test.isPending}
                onClick={() => test.mutate()}
              >
                {t("preferences.sendTest")}
              </Button>
            </SettingsRow>
          ) : null}
        </SettingsCard>
        {query.isError || change.isError ? (
          <p role="alert" className="text-destructive type-ui">
            {t(query.isError ? "preferences.loadFailed" : "preferences.failed")}
          </p>
        ) : null}
      </SettingsGroup>
    </>
  );
}
