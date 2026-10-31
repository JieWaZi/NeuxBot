import {
  CircleAlert,
  CircleArrowUp,
  CircleCheck,
  Code2,
  Download,
  ExternalLink,
  Globe2,
  ScrollText,
  Info,
  MonitorOff,
  PackageOpen,
  RefreshCw,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useState } from "react";
import {
  applicationReleasesURL,
  useApplicationUpdate,
} from "@/shared/platform/application-update";
import { openExternalURL } from "@/shared/platform";
import { useTheme } from "@/shared/theme";
import { cn } from "@/shared/lib/cn";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { StaticMarkdown } from "@/shared/ui/static-markdown";
import { SettingsContentShell } from "./settings-section-layout";

export function AboutSoftwareSection() {
  const { t } = useTranslation("appSettings");
  const { resolvedTheme } = useTheme();
  const [releasePageError, setReleasePageError] = useState(false);
  const [releaseNotesOpen, setReleaseNotesOpen] = useState(false);
  const updates = useApplicationUpdate();
  const state = updates.state;
  const release = state?.release;
  const checking = (!state && !updates.error) || state?.status === "checking";
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
  const isDevelopmentPreview = state?.unavailableReason === "development";
  const canInstall = Boolean(release) && !isDevelopmentPreview;
  const canCheck =
    state && !["unavailable", "checking", "downloading"].includes(state.status);
  const versionLabel = state?.currentVersion
    ? `v${state.currentVersion}`
    : !state
      ? t(updates.error ? "about.versionUnavailable" : "about.versionLoading")
      : t(
          state.platform === "browser"
            ? "about.browserLabel"
            : "about.development",
        );
  const statusTitle = release
    ? t("about.newVersion", { version: release.version })
    : state?.status === "unavailable"
      ? t(
          state.unavailableReason === "browser"
            ? "about.browserTitle"
            : state.unavailableReason === "platform"
              ? "about.unsupportedTitle"
              : "about.development",
        )
      : isDevelopmentPreview && state.status === "idle" && !release
        ? t("about.development")
        : state
          ? t(`about.statusHeadings.${state.status}`)
          : t(updates.error ? "about.statusHeadings.error" : "about.loading");
  const statusDescription = !state
    ? t(updates.error ? "about.status.error" : "about.loading")
    : state.status === "unavailable"
      ? t(
          state.unavailableReason === "browser"
            ? "about.browserUnavailable"
            : state.unavailableReason === "platform"
              ? "about.unsupported"
              : "about.developmentHint",
        )
      : isDevelopmentPreview && state.status === "idle" && !release
        ? t("about.developmentHint")
        : t(`about.status.${state.status}`);
  const showStatusDescription = !(isDevelopmentPreview && release);
  const StatusIcon = checking
    ? RefreshCw
    : state?.status === "unavailable"
      ? state.unavailableReason === "browser"
        ? Globe2
        : state.unavailableReason === "platform"
          ? MonitorOff
          : Code2
      : isDevelopmentPreview && state.status === "idle" && !release
        ? Code2
      : updates.error || state?.status === "error"
        ? CircleAlert
        : downloaded
          ? PackageOpen
          : downloading
            ? Download
            : release
              ? CircleArrowUp
              : state?.status === "current"
              ? CircleCheck
              : Info;

  return (
    <SettingsContentShell
      title={t("about.title")}
      description={t("about.description")}
    >
      <div className="space-y-4">
        <section
          aria-label={t("about.productInformation")}
          className="flex items-center gap-3"
        >
          <img
            src={
              resolvedTheme === "dark"
                ? "/ally-logo-dark.svg"
                : "/ally-logo.svg"
            }
            alt={t("common:appName")}
            className="size-avatar-md shrink-0 object-contain"
            draggable={false}
          />
          <div className="min-w-0 flex flex-1 flex-wrap items-center gap-2">
            <h2 className="type-title text-content-primary font-semibold">
              {t("common:appName")}
            </h2>
            <Badge variant="secondary">{versionLabel}</Badge>
            <Button
              variant="ghost"
              className="ms-auto shrink-0"
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
              {t(
                state?.status === "checking" ? "about.checking" : "about.check",
              )}
            </Button>
          </div>
        </section>

        <Card variant="section" asChild>
          <section
            aria-label={t("about.updateTitle")}
            className="space-y-3"
            aria-busy={checking || downloading}
          >
            <div className="space-y-3">
              <div role="status" aria-live="polite" className="space-y-2">
                <div className="flex flex-wrap items-center gap-3">
                  <div className="flex min-w-0 flex-1 items-center gap-2">
                    <StatusIcon
                      aria-hidden="true"
                      className={cn(
                        "text-content-secondary size-icon-sm shrink-0",
                        checking && "motion-safe:animate-spin",
                      )}
                    />
                    <h3 className="type-label text-content-primary min-w-0 font-medium">
                      {statusTitle}
                    </h3>
                    {release?.notes ? (
                      <Button
                        type="button"
                        variant="plain"
                        size="icon-sm"
                        className="ms-auto shrink-0"
                        aria-label={t("about.viewReleaseNotes", {
                          version: release.version,
                        })}
                        onClick={() => setReleaseNotesOpen(true)}
                      >
                        <ScrollText aria-hidden="true" />
                      </Button>
                    ) : null}
                  </div>
                  {canInstall ? (
                    <Button
                      disabled={
                        updates.busy ||
                        downloading ||
                        state?.status === "checking"
                      }
                      onClick={
                        downloaded ? updates.openInstaller : updates.download
                      }
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
                  ) : null}
                </div>
                {showStatusDescription ? (
                  <p className="type-body text-content-secondary ps-6">
                    {statusDescription}
                  </p>
                ) : null}
                {downloading ? (
                  <div className="flex items-center gap-3 ps-6">
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
              </div>

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
            </div>
          </section>
        </Card>
        {release?.notes ? (
          <Dialog open={releaseNotesOpen} onOpenChange={setReleaseNotesOpen}>
            <DialogContent
              size="lg"
              title={t("about.releaseNotes")}
              description={t("about.newVersion", {
                version: release.version,
              })}
            >
              <div className="text-content-primary max-h-[min(60vh,520px)] overflow-y-auto px-6 pb-6">
                <StaticMarkdown
                  source={release.notes}
                  onLink={(href) => {
                    if (/^(https?:|mailto:)/i.test(href))
                      void openExternalURL(href);
                  }}
                />
              </div>
            </DialogContent>
          </Dialog>
        ) : null}
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
          <Button
            variant="link"
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
          {releasePageError ? (
            <p role="alert" className="type-body text-destructive basis-full">
              {t("about.releasePageError")}
            </p>
          ) : null}
        </div>
      </div>
    </SettingsContentShell>
  );
}
