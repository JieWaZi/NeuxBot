import { lazy, Suspense } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Skeleton } from "@/shared/ui/skeleton";
import type { SavedTask, TaskView } from "../model/types";

const TaskForm = lazy(() =>
  import("./task-form-dialog").then((module) => ({ default: module.TaskForm })),
);
const APISettingsForm = lazy(() =>
  import("./api-settings-dialog").then((module) => ({
    default: module.APISettingsForm,
  })),
);

function TaskDialogLoading({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation("automaticTask");
  return (
    <>
      <div role="status" aria-busy="true" className="grid gap-4 px-6 pb-6">
        <Skeleton className="h-control-md w-full" />
        <Skeleton className="h-control-md w-full" />
      </div>
      <DialogFooter>
        <Button variant="ghost" onClick={onClose}>
          {t("cancel")}
        </Button>
      </DialogFooter>
    </>
  );
}

/** 弹窗外壳立即挂载，表单与查询依赖只在打开时载入。 */
export function TaskFormDialog({
  open,
  onOpenChange,
  onSaved,
  wakerId = "",
  task,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
  onSaved: (value: SavedTask) => void;
  wakerId?: string;
  task?: TaskView;
}) {
  const { t } = useTranslation("automaticTask");
  const close = () => onOpenChange(false);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="lg"
        layout="form"
        title={t(task ? "edit" : "create")}
      >
        {open && (
          <Suspense fallback={<TaskDialogLoading onClose={close} />}>
            <TaskForm
              key={task?.id || "create"}
              task={task}
              wakerId={wakerId}
              onSaved={onSaved}
              onCancel={close}
            />
          </Suspense>
        )}
      </DialogContent>
    </Dialog>
  );
}

/** 统一服务配置的表单按需加载，关闭状态不拉入设置依赖。 */
export function AutomaticTaskAPISettingsDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useTranslation("automaticTask");
  const close = () => onOpenChange(false);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent title={t("portTitle")} description={t("portHint")}>
        {open && (
          <Suspense fallback={<TaskDialogLoading onClose={close} />}>
            <APISettingsForm onClose={close} />
          </Suspense>
        )}
      </DialogContent>
    </Dialog>
  );
}
