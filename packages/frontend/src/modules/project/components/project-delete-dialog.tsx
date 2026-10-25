import { useMutation, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { LoaderCircle, TriangleAlert } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
  deleteProjectMutation,
  listProjectsQueryKey,
} from "../api/project-queries";
import type { Project } from "../model/types";
import { ApiError } from "@/shared/api/errors";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";

type Props = {
  open: boolean;
  onDeleted?: () => void;
  onOpenChange: (open: boolean) => void;
  project: Project;
  wakerId: string;
};

export function ProjectDeleteDialog({
  onDeleted,
  onOpenChange,
  open,
  project,
  wakerId,
}: Props) {
  const { t } = useTranslation("project");
  const queryClient = useQueryClient();
  const remove = useMutation(withLocalErrorMutation(deleteProjectMutation()));

  const confirmDelete = async () => {
    if (!project.id) return;
    try {
      await remove.mutateAsync({
        path: { projectId: project.id, wakerId },
      });
      await queryClient.invalidateQueries({
        queryKey: listProjectsQueryKey({ path: { wakerId } }),
      });
      onOpenChange(false);
      onDeleted?.();
    } catch {
      // React Query 保存错误，由确认弹窗就近展示。
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!remove.isPending) {
          if (!next) remove.reset();
          onOpenChange(next);
        }
      }}
    >
      <DialogContent
        size="sm"
        title={t("delete.title")}
        description={t("delete.description")}
      >
        <div className="px-6 pb-5">
          <div className="bg-destructive/8 text-destructive type-body flex gap-2 rounded-md p-3">
            <TriangleAlert className="size-icon-md mt-0.5 shrink-0" />
            <p>{t("delete.warning", { name: project.name })}</p>
          </div>
          {remove.error ? (
            <p role="alert" className="text-destructive type-body mt-3">
              {remove.error instanceof ApiError
                ? remove.error.message
                : t("delete.failed")}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="secondary"
            disabled={remove.isPending}
            onClick={() => {
              remove.reset();
              onOpenChange(false);
            }}
          >
            {t("delete.cancel")}
          </Button>
          <Button
            type="button"
            variant="danger"
            disabled={remove.isPending || !project.id}
            onClick={() => void confirmDelete()}
          >
            {remove.isPending ? (
              <LoaderCircle className="animate-spin" />
            ) : null}
            {t("delete.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
