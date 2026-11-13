import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  CircleDot,
  Copy,
  Folder,
  FolderInput,
  FolderPlus,
  Pencil,
  Trash2,
} from "lucide-react";
import { useId, useRef, useState, type ReactElement } from "react";
import { useTranslation } from "react-i18next";

import { listChatsOptions } from "@/modules/chat";
import {
  duplicateWakerMutation,
  isBuiltinWaker,
  listWakersQueryKey,
  WakerDeleteDialog,
  WakerFormDialog,
  type Waker,
} from "@/modules/waker";
import { markLatestChatUnread } from "@/shared/api/chat-reading";
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
  children,
  groups,
  onCopied,
  onDeleted,
  onMove,
  waker,
  wakerId,
}: {
  children: ReactElement;
  groups: readonly WakerNavigationGroup[];
  onCopied: (newId: string) => void;
  onDeleted: () => void;
  onMove: (groupName: string) => void;
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
  const currentGroup =
    groups.find((group) => group.wakerIds.includes(wakerId))?.name ?? "";
  const duplicate = useMutation(
    withLocalErrorMutation(duplicateWakerMutation()),
  );
  const markUnread = async () => {
    setMarkingUnread(true);
    try {
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
      const result = await duplicate.mutateAsync({
        path: { wakerId },
        body: { requestId },
      });
      copyRequest.current = undefined;
      if (result.wakerId) onCopied(result.wakerId);
      await client.invalidateQueries({ queryKey: listWakersQueryKey() });
    } catch (error) {
      toast.error(localApiErrorMessage(error, t("duplicateError")));
    }
  };
  const closeDialog = (open: boolean) => {
    if (open) return;
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
          <ContextMenuItem
            disabled={markingUnread}
            onSelect={() => void markUnread()}
          >
            <CircleDot />
            {t("markUnread")}
          </ContextMenuItem>
          <ContextMenuSub>
            <ContextMenuSubTrigger>
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
                    onSelect={() => onMove(name)}
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
            disabled={duplicate.isPending}
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
                if (!name) return;
                closeDialog(false);
                onMove(name);
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
                <Button type="submit" disabled={!groupName.trim()}>
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
