import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  CircleDot,
  Copy,
  Folder,
  FolderInput,
  FolderPlus,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { useId, useRef, useState, type ReactElement } from "react";
import { useTranslation } from "react-i18next";

import { listChatsOptions } from "@/modules/chat";
import {
  duplicateWakerMutation,
  isBuiltinWaker,
  listWakerNavigationGroupsQueryKey,
  listWakersQueryKey,
  WakerDeleteDialog,
  WakerFormDialog,
  type Waker,
} from "@/modules/waker";
import {
  markLatestChatUnread,
  markLatestConversationUnread,
} from "@/shared/api/chat-reading";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/shared/ui/context-menu";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import { toast } from "@/shared/ui/sonner";

import type { WakerNavigationGroup } from "./partner-navigation-preferences";

export function WakerNavigationMenu({
  canDuplicate,
  canMove,
  children,
  groups,
  onDeleted,
  onMove,
  onNewChat,
  waker,
  wakerId,
}: {
  canDuplicate: boolean;
  canMove: boolean;
  children: ReactElement;
  groups: readonly WakerNavigationGroup[];
  onDeleted: () => void;
  onMove: (groupName: string) => Promise<boolean>;
  onNewChat: () => void;
  waker: Waker;
  wakerId: string;
}) {
  const { t } = useTranslation("common", {
    keyPrefix: "partnerNavigation.wakerActions",
  });
  const { t: common } = useTranslation("common");
  const client = useQueryClient();
  const [dialog, setDialog] = useState<"edit" | "delete" | "group">();
  const groupNameId = useId();
  const [groupName, setGroupName] = useState("");
  const [markingUnread, setMarkingUnread] = useState(false);
  const origin = useRef<HTMLElement | null>(null);
  const copyRequest = useRef<string | undefined>(undefined);
  const groupRequest = useRef<Promise<boolean> | undefined>(undefined);
  const currentGroup =
    groups.find((group) => group.wakerIds.includes(wakerId))?.name ?? "";
  const duplicate = useMutation(
    withLocalErrorMutation(duplicateWakerMutation()),
  );
  const markUnread = async () => {
    setMarkingUnread(true);
    try {
      if (
        await markLatestConversationUnread(client, {
          subjectType: "waker",
          subjectId: wakerId,
        })
      )
        return;
      const result = await client.fetchQuery({
        ...listChatsOptions(
          withLocalErrorSurface({ query: { wakerId, limit: 1 } }),
        ),
        staleTime: 0,
      });
      const chatId = result.resources?.[0]?.id;
      if (!chatId || !(await markLatestChatUnread(client, chatId)))
        toast.info(t("noReadableMessages"));
    } catch (error) {
      toast.error(localApiErrorMessage(error, t("markUnreadError")));
    } finally {
      setMarkingUnread(false);
    }
  };
  const copy = async () => {
    try {
      const requestId = copyRequest.current ?? crypto.randomUUID();
      copyRequest.current = requestId;
      await duplicate.mutateAsync({
        path: { wakerId },
        body: { requestId },
      });
      copyRequest.current = undefined;
      await Promise.all([
        client.invalidateQueries({ queryKey: listWakersQueryKey() }),
        client.invalidateQueries({
          queryKey: listWakerNavigationGroupsQueryKey(),
        }),
      ]);
    } catch (error) {
      toast.error(localApiErrorMessage(error, t("duplicateError")));
    }
  };
  const closeDialog = (open: boolean) => {
    if (open) return;
    groupRequest.current = undefined;
    setDialog(undefined);
    requestAnimationFrame(() => origin.current?.focus());
  };
  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <div
            onContextMenu={(event) => {
              origin.current = event.currentTarget.querySelector(
                "[data-waker-nav-row]",
              );
            }}
          >
            {children}
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent
          aria-label={t("menu", { name: waker.name })}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            if (!dialog) origin.current?.focus();
          }}
        >
          <ContextMenuItem onSelect={onNewChat}>
            <Plus />
            {common("partnerNavigation.newChat")}
          </ContextMenuItem>
          <ContextMenuItem
            disabled={markingUnread}
            onSelect={() => void markUnread()}
          >
            <CircleDot />
            {t("markUnread")}
          </ContextMenuItem>
          <ContextMenuSub>
            <ContextMenuSubTrigger disabled={!canMove}>
              <FolderInput />
              {t("moveTo")}
            </ContextMenuSubTrigger>
            <ContextMenuSubContent aria-label={t("moveTo")}>
              {[...groups.map((group) => group.name), ""].map((name) => {
                const Icon = name === currentGroup ? Check : Folder;
                return (
                  <ContextMenuItem
                    key={name}
                    aria-current={name === currentGroup || undefined}
                    onSelect={() => void onMove(name)}
                  >
                    <Icon />
                    <span className="truncate">{name || t("ungrouped")}</span>
                  </ContextMenuItem>
                );
              })}
              <ContextMenuSeparator />
              <ContextMenuItem
                onSelect={() => {
                  setGroupName("");
                  setDialog("group");
                }}
              >
                <FolderPlus />
                {t("newGroup")}
              </ContextMenuItem>
            </ContextMenuSubContent>
          </ContextMenuSub>
          <ContextMenuItem onSelect={() => setDialog("edit")}>
            <Pencil />
            {t("edit")}
          </ContextMenuItem>
          <ContextMenuItem
            disabled={duplicate.isPending || !canDuplicate}
            onSelect={() => void copy()}
          >
            <Copy />
            {t(duplicate.isPending ? "duplicating" : "duplicate")}
          </ContextMenuItem>
          {!isBuiltinWaker(waker) ? (
            <>
              <ContextMenuSeparator />
              <ContextMenuItem
                variant="destructive"
                onSelect={() => setDialog("delete")}
              >
                <Trash2 />
                {t("delete")}
              </ContextMenuItem>
            </>
          ) : null}
        </ContextMenuContent>
      </ContextMenu>
      {dialog === "edit" ? (
        <WakerFormDialog open waker={waker} onOpenChange={closeDialog} />
      ) : null}
      {dialog === "delete" ? (
        <WakerDeleteDialog
          open
          waker={waker}
          onOpenChange={closeDialog}
          onDeleted={onDeleted}
        />
      ) : null}
      {dialog === "group" ? (
        <Dialog open onOpenChange={closeDialog}>
          <DialogContent title={t("newGroup")} size="sm">
            <form
              onSubmit={(event) => {
                event.preventDefault();
                const name = groupName.trim();
                if (!name || !canMove) return;
                const request = onMove(name);
                groupRequest.current = request;
                void request.then((moved) => {
                  if (moved && groupRequest.current === request)
                    closeDialog(false);
                });
              }}
            >
              <div className="grid gap-2 px-6 pb-5">
                <FieldLabel htmlFor={groupNameId}>{t("groupName")}</FieldLabel>
                <Input
                  id={groupNameId}
                  autoFocus
                  maxLength={60}
                  value={groupName}
                  onChange={(event) => setGroupName(event.target.value)}
                />
              </div>
              <DialogFooter>
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => closeDialog(false)}
                >
                  {common("cancel")}
                </Button>
                <Button type="submit" disabled={!groupName.trim() || !canMove}>
                  {common("save")}
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      ) : null}
    </>
  );
}
