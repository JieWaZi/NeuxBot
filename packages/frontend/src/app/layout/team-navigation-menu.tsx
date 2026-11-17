import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Pencil, Trash2 } from "lucide-react";
import { useRef, useState, type ReactElement } from "react";
import { useTranslation } from "react-i18next";

import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelDetail as ChannelDetail } from "@/shared/api/generated/types.gen";
import {
  deleteChannelMutation,
  teamQueryKeys,
} from "@/modules/team/api/channel-queries";
import { TeamConfirmationDialog } from "@/modules/team/components/team-confirmation-dialog";
import { TeamFormDialog } from "@/modules/team/components/team-form-dialog";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/shared/ui/context-menu";
import { useSceneWorkspace } from "@/shared/workspace/workspace-context";

/** 团队管理归属侧栏身份行，沿用原编辑、删除及工作区清理能力。 */
export function TeamNavigationMenu({
  children,
  detail,
  onDeleted,
}: {
  children: ReactElement;
  detail: ChannelDetail;
  onDeleted: () => void;
}) {
  const { t } = useTranslation("team");
  const client = useQueryClient();
  const workspace = useSceneWorkspace("team-management");
  const [dialog, setDialog] = useState<"edit" | "delete">();
  const [error, setError] = useState("");
  const origin = useRef<HTMLElement | null>(null);
  const remove = useMutation(withLocalErrorMutation(deleteChannelMutation()));
  const channel = detail.channel;
  const closeDialog = (open: boolean) => {
    if (open) return;
    setDialog(undefined);
    requestAnimationFrame(() => origin.current?.focus());
  };
  if (!channel?.id) return children;
  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <div
            onContextMenu={(event) => {
              origin.current = event.currentTarget.querySelector(
                "[data-team-nav-row]",
              );
            }}
          >
            {children}
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent
          aria-label={t("moreActions", { name: channel.name ?? t("title") })}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            if (!dialog) origin.current?.focus();
          }}
        >
          <ContextMenuItem onSelect={() => setDialog("edit")}>
            <Pencil />
            {t("edit")}
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem
            variant="destructive"
            onSelect={() => {
              setError("");
              setDialog("delete");
            }}
          >
            <Trash2 />
            {t("delete")}
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
      {dialog === "edit" ? (
        <TeamFormDialog detail={detail} open onOpenChange={closeDialog} />
      ) : null}
      <TeamConfirmationDialog
        open={dialog === "delete"}
        onOpenChange={closeDialog}
        title={t("deleteDialog.title")}
        description={t("deleteDialog.description", { name: channel.name })}
        errorMessage={error}
        confirmLabel={t("deleteDialog.confirm")}
        isPending={remove.isPending}
        onConfirm={async () => {
          try {
            await remove.mutateAsync({ path: { channelId: channel.id ?? "" } });
            workspace.dispatch({
              type: "clear-prefix",
              scopePrefix: `team:${channel.id}:`,
            });
            await client.invalidateQueries({
              queryKey: teamQueryKeys.channels(),
            });
            onDeleted();
            closeDialog(false);
          } catch (cause) {
            setError(localApiErrorMessage(cause, t("deleteDialog.failed")));
          }
        }}
      />
    </>
  );
}
