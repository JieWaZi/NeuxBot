import { Card, CardBody, CardFooter } from "@/shared/ui/card";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { MessageCircle, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";

import type { GithubComJieWaZiNeuxBotServerApisChannelsChannelDetail as ChannelDetail } from "@/shared/api/generated/types.gen";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { useSceneWorkspace } from "@/shared/workspace/workspace-context";

import { deleteChannelMutation, teamQueryKeys } from "../api/channel-queries";
import { TeamAvatarStack } from "./team-avatar-stack";
import { TeamConfirmationDialog } from "./team-confirmation-dialog";
import { TeamFormDialog } from "./team-form-dialog";

export function TeamCard({
  detail,
  onCollectionChanged,
}: {
  detail: ChannelDetail;
  onCollectionChanged?: () => void;
}) {
  const { t } = useTranslation("team");
  const channel = detail.channel;
  const members = detail.members ?? [];
  const [editing, setEditing] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const queryClient = useQueryClient();
  const workspace = useSceneWorkspace("team-management");
  const remove = useMutation(withLocalErrorMutation(deleteChannelMutation()));
  if (!channel?.id) return null;
  const lead = members.find(
    (member) => member.waker?.id === channel.leadWakerId,
  );

  return (
    <>
      <Card asChild variant="resource">
        <article>
          <CardBody asChild>
            <button
              type="button"
              aria-label={t("editCard", { name: channel.name ?? t("title") })}
              onClick={() => setEditing(true)}
              className="grid min-h-32 w-full grid-cols-[auto_minmax(0,1fr)] gap-4 p-5 text-left transition-colors"
            >
              <TeamAvatarStack members={members} />
              <span className="min-w-0">
                <span className="flex items-center gap-2">
                  <strong className="type-label truncate font-medium">
                    {channel.name}
                  </strong>
                  <span className="text-muted-foreground type-caption shrink-0">
                    {t("membersCount", { count: members.length })}
                  </span>
                </span>
                <span className="text-muted-foreground type-body mt-2 line-clamp-2">
                  {channel.description || t("noDescription")}
                </span>
                <span className="text-muted-foreground type-caption mt-2 block truncate">
                  {t("lead", {
                    name: lead?.waker?.name ?? t("unnamedMember"),
                  })}
                </span>
              </span>
            </button>
          </CardBody>
          <CardFooter className="grid grid-cols-[1fr_44px] gap-0 px-0">
            <Link
              to={`/teams/${channel.id}`}
              className="hover:bg-muted h-row-navigation type-ui flex items-center justify-center gap-1.5 transition-colors"
            >
              <MessageCircle className="size-icon-sm" strokeWidth={1.5} />
              {t("enterChat")}
            </Link>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  aria-label={t("moreActions", {
                    name: channel.name ?? t("title"),
                  })}
                  className="mx-auto"
                  size="icon"
                  variant="ghost"
                >
                  <MoreHorizontal className="size-icon-md" strokeWidth={1.5} />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => setEditing(true)}>
                  <Pencil />
                  {t("edit")}
                </DropdownMenuItem>
                <DropdownMenuItem
                  className="text-destructive data-[highlighted]:text-destructive"
                  onSelect={() => {
                    setDeleteError("");
                    setDeleteOpen(true);
                  }}
                >
                  <Trash2 />
                  {t("delete")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </CardFooter>
        </article>
      </Card>
      {editing ? (
        <TeamFormDialog detail={detail} open onOpenChange={setEditing} />
      ) : null}
      <TeamConfirmationDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("deleteDialog.title")}
        description={t("deleteDialog.description", { name: channel.name })}
        errorMessage={deleteError}
        confirmLabel={t("deleteDialog.confirm")}
        isPending={remove.isPending}
        onConfirm={async () => {
          try {
            await remove.mutateAsync({
              path: { channelId: channel.id ?? "" },
            });
            workspace.dispatch({
              type: "clear-prefix",
              scopePrefix: `team:${channel.id}:`,
            });
            await queryClient.invalidateQueries({
              queryKey: teamQueryKeys.channels(),
            });
            onCollectionChanged?.();
            setDeleteOpen(false);
          } catch (error) {
            setDeleteError(
              localApiErrorMessage(error, t("deleteDialog.failed")),
            );
          }
        }}
      />
    </>
  );
}
