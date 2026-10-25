import { Download, ExternalLink, PackageOpen, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useState } from "react";
import {
  applicationReleasesURL,
  useApplicationUpdate,
} from "@/shared/platform/application-update";
import { openExternalURL } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { SettingsContentShell, SettingsRow } from "./settings-section-layout";

export function AboutSoftwareSection() {
  const { t } = useTranslation("appSettings");
  const [releasePageError, setReleasePageError] = useState(false);
  const updates = useApplicationUpdate();
  const state = updates.state;
  const release = state?.release;
  const downloading = state?.status === "downloading";
  const downloaded =
    state?.status === "downloaded" || state?.error === "open-installer";
  const progress = release
    ? Math.min(
        100,
        Math.floor(
          ((state?.downloadedBytes ?? 0) / release.downloadSize) * 100,
        ),
      )
    : 0;
  const canCheck =
    state && !["unavailable", "checking", "downloading"].includes(state.status);

  return (
    <SettingsContentShell
      title={t("about.title")}
      description={t("about.description")}
    >
      <SettingsRow
        title={t("common:appName")}
        description={t("about.productDescription")}
      >
        <span className="type-body text-content-primary tabular-nums">
          {!state
            ? t("about.loading")
            : state.currentVersion
              ? `v${state.currentVersion}`
              : t(
                  state.platform === "browser"
                    ? "about.browserVersion"
                    : "about.development",
                )}
        </span>
      </SettingsRow>
      <SettingsRow
        title={t("about.updateTitle")}
        description={t("about.updateDescription")}
      >
        <Button
          variant="secondary"
          disabled={!canCheck || updates.busy}
          onClick={updates.check}
        >
          <RefreshCw
            aria-hidden="true"
            className={
              state?.status === "checking"
                ? "motion-safe:animate-spin"
                : undefined
            }
          />
          {t(state?.status === "checking" ? "about.checking" : "about.check")}
        </Button>
      </SettingsRow>
      <div
        className="flex flex-col gap-4"
        aria-live="polite"
        aria-busy={downloading || state?.status === "checking"}
      >
        <p className="type-body text-content-secondary" role="status">
          {!state
            ? t("about.loading")
            : state.status === "unavailable"
              ? t(
                  state.unavailableReason === "browser"
                    ? "about.browserUnavailable"
                    : state.unavailableReason === "platform"
                      ? "about.unsupported"
                      : "about.developmentHint",
                )
              : t(`about.status.${state.status}`)}
        </p>
        {state?.error ? (
          <p role="alert" className="type-body text-destructive">
            {t(`about.errors.${state.error}`)}
          </p>
        ) : null}
        {updates.error ? (
          <div className="flex flex-wrap items-center gap-2">
            <p role="alert" className="type-body text-destructive">
              {t("about.bridgeError")}
            </p>
            <Button variant="secondary" onClick={updates.retry}>
              {t("about.retry")}
            </Button>
          </div>
        ) : null}
        {release ? (
          <div className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center justify-between gap-4">
              <h2 className="type-label text-content-primary">
                {t("about.newVersion", { version: release.version })}
              </h2>
              <Button
                disabled={
                  updates.busy || downloading || state?.status === "checking"
                }
                onClick={downloaded ? updates.openInstaller : updates.download}
              >
                {downloaded ? (
                  <PackageOpen aria-hidden="true" />
                ) : (
                  <Download aria-hidden="true" />
                )}
                {t(
                  downloaded
                    ? "about.openInstaller"
                    : downloading
                      ? "about.downloading"
                      : "about.download",
                )}
              </Button>
            </div>
            {downloading ? (
              <div className="flex items-center gap-3">
                <progress
                  className="accent-accent min-w-0 flex-1"
                  max={100}
                  value={progress}
                  aria-label={t("about.progress")}
                />
                <span className="type-body text-content-secondary tabular-nums">
                  {progress}%
                </span>
              </div>
            ) : null}
            <p className="type-body text-content-secondary">
              {t(
                state?.platform === "win32"
                  ? "about.windowsInstall"
                  : "about.macInstall",
              )}
            </p>
            <p className="type-body text-content-secondary">
              {t("about.dataPreserved")}
            </p>
            {release.notes ? (
              <section className="flex flex-col gap-2">
                <h3 className="type-body text-content-primary font-medium">
                  {t("about.releaseNotes")}
                </h3>
                <p className="type-body text-content-secondary wrap-anywhere whitespace-pre-wrap">
                  {release.notes}
                </p>
              </section>
            ) : null}
          </div>
        ) : null}
        <div className="flex flex-wrap items-center gap-3">
          {state?.checkedAt ? (
            <span className="type-caption text-content-secondary">
              {t("about.lastChecked", {
                time: new Intl.DateTimeFormat("zh-CN", {
                  dateStyle: "short",
                  timeStyle: "short",
                }).format(new Date(state.checkedAt)),
              })}
            </span>
          ) : null}
          <Button
            variant="ghost"
            onClick={() => {
              setReleasePageError(false);
              void openExternalURL(
                release?.url ?? applicationReleasesURL,
              ).catch(() => setReleasePageError(true));
            }}
          >
            <ExternalLink aria-hidden="true" />
            {t("about.releasePage")}
          </Button>
        </div>
        {releasePageError ? (
          <p role="alert" className="type-body text-destructive">
            {t("about.releasePageError")}
          </p>
        ) : null}
      </div>
    </SettingsContentShell>
  );
}
