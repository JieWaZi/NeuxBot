import { BookOpen, FileText, Settings, Upload, Network } from "lucide-react";
import { Button } from "@/shared/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/shared/ui/tooltip";
import { IconAction } from "./knowledge-controls";
import type { Library } from "./knowledge-model";
import { useCopy } from "./use-copy";

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
}) {
  const t = useCopy();
  return (
    <div
      role="group"
      aria-label={t("libraryActions")}
      className="flex shrink-0 items-center gap-2"
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
      <IconAction
        icon={BookOpen}
        label={t("organizeDocuments")}
        disabled={organizeDisabled}
        onClick={onOrganize}
      />
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
