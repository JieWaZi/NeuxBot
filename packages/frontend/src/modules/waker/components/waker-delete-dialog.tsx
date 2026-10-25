import { useMutation, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { LoaderCircle, TriangleAlert } from "lucide-react";
import { useState } from "react";

import { deleteWakerMutation, listWakersQueryKey } from "../api/waker-queries";
import { isBuiltinWaker } from "../model/builtin-waker";
import type { Waker } from "../model/types";
import { ApiError } from "@/shared/api/errors";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDeleted?: () => void;
  waker: Waker;
};

export function WakerDeleteDialog({
  onDeleted,
  onOpenChange,
  open,
  waker,
}: Props) {
  const queryClient = useQueryClient();
  const remove = useMutation(withLocalErrorMutation(deleteWakerMutation()));
  const [confirmationName, setConfirmationName] = useState("");
  const expectedName = waker.name ?? "";

  if (isBuiltinWaker(waker)) {
    return null;
  }

  const closeDialog = () => {
    setConfirmationName("");
    remove.reset();
    onOpenChange(false);
  };

  const confirmDelete = async () => {
    if (!waker.id || confirmationName !== expectedName) return;
    try {
      await remove.mutateAsync({
        path: { wakerId: waker.id },
        body: { confirmationName },
      });
      await queryClient.invalidateQueries({ queryKey: listWakersQueryKey() });
      closeDialog();
      onDeleted?.();
    } catch {
      // React Query 保存错误，由确认弹窗就近展示。
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) closeDialog();
      }}
    >
      <DialogContent
        size="sm"
        title="删除伙伴"
        description="此操作不可恢复，请确认不再需要这位伙伴。"
      >
        <div className="px-6 pb-5">
          <div className="bg-destructive/8 text-destructive type-body flex gap-2 rounded-md p-3">
            <TriangleAlert className="size-icon-md mt-0.5 shrink-0" />
            <p>将删除伙伴档案、记忆和执行记录；角色预设本身不会被删除。</p>
          </div>
          <label className="type-label mt-4 grid gap-2 font-medium">
            输入完整名称“{expectedName}”以确认
            <Input
              autoFocus
              placeholder={`请输入“${expectedName}”`}
              value={confirmationName}
              onChange={(event) => setConfirmationName(event.target.value)}
            />
          </label>
          {remove.error ? (
            <p role="alert" className="text-destructive type-body mt-3">
              {remove.error instanceof ApiError
                ? remove.error.message
                : "删除失败，请稍后重试。"}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={closeDialog}>
            取消
          </Button>
          <Button
            type="button"
            variant="danger"
            disabled={
              remove.isPending || confirmationName !== expectedName || !waker.id
            }
            onClick={() => void confirmDelete()}
          >
            {remove.isPending ? (
              <LoaderCircle className="animate-spin" />
            ) : null}
            删除伙伴
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
