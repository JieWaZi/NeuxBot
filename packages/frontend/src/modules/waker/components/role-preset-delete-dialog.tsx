import { useMutation, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { LoaderCircle } from "lucide-react";

import {
  deleteRolePresetMutation,
  listRolePresetsQueryKey,
} from "../api/waker-queries";
import type { RolePreset } from "../model/types";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";

type Props = {
  open: boolean;
  onDeleted: () => void;
  onOpenChange: (open: boolean) => void;
  preset?: RolePreset;
};

export function RolePresetDeleteDialog({
  onDeleted,
  onOpenChange,
  open,
  preset,
}: Props) {
  const queryClient = useQueryClient();
  const remove = useMutation(
    withLocalErrorMutation(deleteRolePresetMutation()),
  );

  const confirmDelete = async () => {
    if (!preset?.id) return;
    try {
      await remove.mutateAsync({ path: { rolePresetId: preset.id } });
      await queryClient.invalidateQueries({
        queryKey: listRolePresetsQueryKey(),
      });
      onOpenChange(false);
      onDeleted();
    } catch {
      // React Query 保存错误，由确认弹窗就近展示。
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (nextOpen) remove.reset();
        onOpenChange(nextOpen);
      }}
    >
      <DialogContent
        size="sm"
        title="删除角色预设"
        description={`确定删除“${preset?.roleName ?? ""}”吗？`}
      >
        <div className="px-6 pb-5">
          <p className="text-muted-foreground type-body">
            已经从该预设创建的伙伴及其档案不会受到影响。此操作只删除预设及其初始档案。
          </p>
          {remove.error ? (
            <p role="alert" className="text-destructive type-body mt-3">
              删除失败，请稍后重试。
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="secondary"
            onClick={() => onOpenChange(false)}
          >
            取消
          </Button>
          <Button
            type="button"
            variant="danger"
            disabled={!preset?.id || remove.isPending}
            onClick={() => void confirmDelete()}
          >
            {remove.isPending ? (
              <LoaderCircle className="animate-spin" />
            ) : null}
            删除预设
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
