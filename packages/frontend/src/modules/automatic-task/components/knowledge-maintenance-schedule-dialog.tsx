import { useTranslation } from "react-i18next";
import { PERSONAL_ASSISTANT_WAKER_ID } from "@/modules/waker";
import type { SavedTask } from "../model/types";
import { DEFAULT_SCHEDULE_TIMEZONE } from "../model/schedule-defaults";
import { TaskFormDialog } from "./task-dialogs";

/** 知识维护复用自动任务的计划、模型选择与保存流程，用户保存前不创建任务。 */
export function KnowledgeMaintenanceScheduleDialog({
  open,
  onOpenChange,
  libraryId,
  libraryName,
  onSaved,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  libraryId: string;
  libraryName: string;
  onSaved?: (value: SavedTask) => void;
}) {
  const { t } = useTranslation("automaticTask");
  return (
    <TaskFormDialog
      key={libraryId}
      open={open}
      onOpenChange={onOpenChange}
      wakerId={PERSONAL_ASSISTANT_WAKER_ID}
      initialConfiguration={{
        name: t("wikiMaintenanceName", { name: libraryName }),
        instruction: t("wikiMaintenanceInstruction"),
        targetKind: "waker",
        workspaceReadOnly: true,
        knowledge: {
          backend: "local",
          connectionId: "local",
          knowledgeBaseId: libraryId,
          operation: "maintain",
        },
        schedule: {
          kind: "weekly",
          timezone: DEFAULT_SCHEDULE_TIMEZONE,
          weekday: 1,
          hour: 9,
          minute: 0,
        },
      }}
      onSaved={(value) => {
        onOpenChange(false);
        onSaved?.(value);
      }}
    />
  );
}
