import { Download, LoaderCircle, PackageOpen } from "lucide-react";
import { useTranslation } from "react-i18next";
import { NotificationBell } from "@/modules/notification";
import { useApplicationUpdate } from "@/shared/platform/application-update";
import { Button } from "@/shared/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";

/** 更新操作位于原入口位置，旁侧保留原通知和任务看板入口。 */
export function DesktopAttentionAction({
  active,
  connected,
  onOpen,
  onOpenUpdates,
}: {
  active: boolean;
  connected: boolean;
  onOpen: () => void;
  onOpenUpdates: () => void;
}) {
  const { t } = useTranslation("appSettings");
  const updates = useApplicationUpdate();
  const state = updates.state;
  if (!state?.release)
    return (
      <NotificationBell active={active} connected={connected} onOpen={onOpen} />
    );

  const downloading = state.status === "downloading";
  const downloaded =
    state.status === "downloaded" || state.error === "open-installer";
  const label = t(
    downloading
      ? "about.downloadActionProgress"
      : downloaded
        ? "about.installAction"
        : "about.downloadAction",
    {
      version: state.release.version,
      progress: Math.min(
        100,
        Math.floor((state.downloadedBytes / state.release.downloadSize) * 100),
      ),
    },
  );
  const Icon = downloading ? LoaderCircle : downloaded ? PackageOpen : Download;
  return (
    <>
      <NotificationBell active={active} connected={connected} onOpen={onOpen} />
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            size="icon"
            shape="capsule"
            variant="ghost"
            aria-label={label}
            data-desktop-no-drag-region
            onClick={() => {
              onOpenUpdates();
              if (updates.busy || downloading || state.status === "checking")
                return;
              if (downloaded) updates.openInstaller();
              else updates.download();
            }}
          >
            <Icon
              aria-hidden="true"
              className={downloading ? "motion-safe:animate-spin" : undefined}
            />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </>
  );
}
