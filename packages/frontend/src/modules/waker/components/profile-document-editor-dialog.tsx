import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { LoaderCircle } from "lucide-react";
import { useState } from "react";

import {
  getWakerDocumentOptions,
  getWakerDocumentQueryKey,
  getWakerQueryKey,
  listWakerDocumentsQueryKey,
  updateWakerDocumentMutation,
} from "../api/waker-queries";
import type { ProfileDocument } from "../model/types";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Textarea } from "@/shared/ui/textarea";

type Props = {
  document?: ProfileDocument;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  wakerId: string;
};

export function ProfileDocumentEditorDialog({
  document,
  onOpenChange,
  open,
  wakerId,
}: Props) {
  const queryClient = useQueryClient();
  const kind = document?.kind ?? "identity";
  const editable = document?.editable === true && kind !== "memory";
  const queryOptions = getWakerDocumentOptions(
    withLocalErrorSurface({
      path: { kind, wakerId },
    }),
  );
  const query = useQuery({
    ...queryOptions,
    enabled: open && editable,
  });
  const update = useMutation(
    withLocalErrorMutation(updateWakerDocumentMutation()),
  );
  const [draft, setDraft] = useState<string>();
  const [confirmClose, setConfirmClose] = useState(false);
  const serverContent = query.data?.content ?? "";
  const content = draft ?? serverContent;
  const dirty = editable && draft !== undefined && draft !== serverContent;

  const finishClose = () => {
    setDraft(undefined);
    setConfirmClose(false);
    update.reset();
    onOpenChange(false);
  };

  const requestClose = () => {
    if (dirty) {
      setConfirmClose(true);
      return;
    }
    finishClose();
  };

  const requestOpenChange = (nextOpen: boolean) => {
    if (nextOpen) return;
    if (confirmClose) {
      setConfirmClose(false);
      return;
    }
    requestClose();
  };

  const save = async () => {
    if (!editable) return;
    try {
      await update.mutateAsync({
        path: { kind, wakerId },
        body: { content },
      });
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: getWakerDocumentQueryKey({ path: { kind, wakerId } }),
        }),
        queryClient.invalidateQueries({
          queryKey: getWakerQueryKey({ path: { wakerId } }),
        }),
        queryClient.invalidateQueries({
          queryKey: listWakerDocumentsQueryKey({ path: { wakerId } }),
        }),
      ]);
      finishClose();
    } catch {
      // React Query 保存错误，由编辑弹窗就近展示。
    }
  };

  return (
    <Dialog open={open} onOpenChange={requestOpenChange}>
      <DialogContent
        size={confirmClose ? "sm" : "lg"}
        title={
          confirmClose
            ? "放弃未保存的修改？"
            : (document?.fileName ?? "伙伴档案")
        }
        description={
          confirmClose
            ? "关闭后，本次对 Markdown 的修改不会保存。"
            : editable
              ? "编辑完整 Markdown，保存后会刷新概览摘要。"
              : "记忆功能将在后续阶段开放。"
        }
        onPointerDownOutside={(event) => {
          if (!dirty || confirmClose) return;
          event.preventDefault();
          setConfirmClose(true);
        }}
        onEscapeKeyDown={(event) => {
          if (confirmClose) {
            event.preventDefault();
            setConfirmClose(false);
            return;
          }
          if (!dirty) return;
          event.preventDefault();
          setConfirmClose(true);
        }}
      >
        {confirmClose ? (
          <DialogFooter>
            <Button
              type="button"
              variant="secondary"
              onClick={() => setConfirmClose(false)}
            >
              继续编辑
            </Button>
            <Button type="button" variant="danger" onClick={finishClose}>
              放弃修改
            </Button>
          </DialogFooter>
        ) : editable ? (
          <>
            <div className="min-h-0 flex-1 px-6 pb-5">
              {query.isPending ? (
                <div className="text-muted-foreground type-body grid min-h-80 place-items-center">
                  正在载入档案…
                </div>
              ) : query.isError ? (
                <div className="grid min-h-80 place-items-center text-center">
                  <div>
                    <p className="type-body">暂时无法载入档案</p>
                    <Button
                      className="mt-3"
                      size="sm"
                      variant="secondary"
                      onClick={() => void query.refetch()}
                    >
                      重新载入
                    </Button>
                  </div>
                </div>
              ) : (
                <Textarea
                  aria-label={`${document?.fileName ?? "档案"} Markdown`}
                  className="min-h-[420px] resize-none font-mono"
                  value={content}
                  onChange={(event) => setDraft(event.target.value)}
                />
              )}
              {update.error ? (
                <p role="alert" className="text-destructive type-body mt-3">
                  {localApiErrorMessage(
                    update.error,
                    "保存失败，请检查内容后重试。",
                  )}
                </p>
              ) : null}
            </div>
            <DialogFooter>
              <Button
                type="button"
                disabled={!dirty || query.isPending || update.isPending}
                onClick={() => void save()}
              >
                {update.isPending ? (
                  <LoaderCircle className="animate-spin" />
                ) : null}
                保存
              </Button>
              <Button type="button" variant="secondary" onClick={requestClose}>
                取消
              </Button>
            </DialogFooter>
          </>
        ) : (
          <div className="text-muted-foreground type-body grid min-h-48 place-items-center px-6 pb-6 text-center">
            该档案文档为只读内容。
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
