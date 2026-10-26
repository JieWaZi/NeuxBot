import {
  BookOpen,
  FileText,
  Settings,
  Upload,
  Network,
  CalendarClock,
  ListChecks,
} from "lucide-react";
import { Button } from "@/shared/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/shared/ui/tooltip";
import { IconAction } from "./knowledge-controls";
import type { Library } from "./knowledge-model";
import { useCopy } from "./use-copy";
import { useWikiCopy } from "./use-wiki-copy";
import type { ReactNode } from "react";

/** 禁用按钮的提示由外层命中区承载，鼠标与键盘仍可查看等待原因。 */
function OrganizeBusyHint({
  busy,
  label,
  children,
}: {
  busy: boolean;
  label: string;
  children: ReactNode;
}) {
  if (!busy) return children;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0} className="inline-flex">
          {children}
        </span>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/** 文档操作与筛选并列，权限和任务状态仍由库详情提供。 */
export function KnowledgeLibraryActions({
  library,
  uploadDisabled = false,
  settingsDisabled = false,
  onUpload,
  onOrganize,
  organizeDisabled,
  onSettings,
  onGraph,
  graphActive = false,
  graphDisabled = false,
  onMaintain,
  onScheduleMaintenance,
  maintenanceDisabled = false,
  scheduleMaintenanceDisabled = maintenanceDisabled,
  busy = false,
}: {
  library?: Library;
  uploadDisabled?: boolean;
  settingsDisabled?: boolean;
  onUpload: () => void;
  onOrganize: () => void;
  organizeDisabled: boolean;
  onSettings: () => void;
  onGraph?: () => void;
  graphActive?: boolean;
  graphDisabled?: boolean;
  onMaintain?: () => void;
  onScheduleMaintenance?: () => void;
  maintenanceDisabled?: boolean;
  scheduleMaintenanceDisabled?: boolean;
  busy?: boolean;
}) {
  const t = useCopy();
  const wiki = useWikiCopy();
  return (
    <div
      role="group"
      aria-label={t("libraryActions")}
      className="flex flex-wrap items-center justify-end gap-2"
    >
      {onGraph && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t(graphActive ? "documents" : "graph")}
              disabled={graphDisabled}
              onClick={onGraph}
            >
              {graphActive ? <FileText /> : <Network />}
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            {t(graphActive ? "documents" : "graph")}
          </TooltipContent>
        </Tooltip>
      )}
      <IconAction
        icon={Settings}
        label={t("settings")}
        disabled={!library || settingsDisabled}
        onClick={onSettings}
      />
      <OrganizeBusyHint busy={busy} label={wiki("maintenanceBusy")}>
        <IconAction
          icon={BookOpen}
          label={t("organizeDocuments")}
          disabled={organizeDisabled || busy}
          onClick={onOrganize}
        />
      </OrganizeBusyHint>
      {onScheduleMaintenance && (
        <IconAction
          icon={CalendarClock}
          label={wiki("scheduleMaintenance")}
          disabled={!library || scheduleMaintenanceDisabled}
          onClick={onScheduleMaintenance}
        />
      )}
      {onMaintain && (
        <OrganizeBusyHint busy={busy} label={wiki("maintenanceBusy")}>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="plain"
                size="icon"
                aria-label={wiki("organizeWiki")}
                disabled={!library || maintenanceDisabled || busy}
                onClick={onMaintain}
              >
                <ListChecks />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{wiki("organizeWiki")}</TooltipContent>
          </Tooltip>
        </OrganizeBusyHint>
      )}
      <Button
        aria-label={t("upload")}
        disabled={!library || uploadDisabled}
        onClick={onUpload}
      >
        <Upload />
        {t("upload")}
      </Button>
    </div>
  );
}
