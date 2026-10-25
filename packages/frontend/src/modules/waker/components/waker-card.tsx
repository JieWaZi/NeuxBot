import { Card, CardBody, CardFooter } from "@/shared/ui/card";
import {
  Clock3,
  MessageCircle,
  MoreHorizontal,
  Pencil,
  Trash2,
} from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { isBuiltinWaker } from "../model/builtin-waker";
import type { Waker } from "../model/types";
import { chatDraftHref } from "@/shared/lib/chat-navigation";
import { WakerDeleteDialog } from "./waker-delete-dialog";
import { WakerFormDialog } from "./waker-form-dialog";
import { Avatar } from "@/shared/ui/avatar";
import { wakerCompanionActivity } from "@/shared/companion/activity";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";

export function WakerCard({ waker }: { waker: Waker }) {
  const { t } = useTranslation("automaticTask");
  const id = waker.id ?? "";
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const builtin = isBuiltinWaker(waker);

  return (
    <>
      <Card asChild variant="resource">
        <article>
          <CardBody className="grid min-h-32 grid-cols-[auto_minmax(0,1fr)] gap-4 p-5 text-left">
            <Avatar
              name={waker.name ?? "伙伴"}
              src={waker.avatar}
              activity={wakerCompanionActivity(waker.activityState)}
              size="lg"
            />
            <Link
              to={`/wakers/${id}`}
              aria-label={`查看 ${waker.name} 的详情`}
              className="focus-visible:ring-ring min-w-0 rounded-sm outline-none focus-visible:ring-2"
            >
              <span className="flex items-center gap-2">
                <strong className="type-label truncate font-medium">
                  {waker.name}
                </strong>
                <span className="text-muted-foreground type-caption">
                  {waker.roleName}
                </span>
              </span>
              <span className="text-muted-foreground type-body mt-2 line-clamp-3 break-words">
                {waker.bio || "还没有填写介绍。"}
              </span>
            </Link>
          </CardBody>

          <CardFooter className="grid grid-cols-[1fr_1fr_44px] gap-0 px-0">
            <Link
              to={chatDraftHref(id)}
              className="hover:bg-muted h-row-navigation type-ui flex items-center justify-center gap-1.5 transition-colors"
            >
              <MessageCircle className="size-icon-sm" strokeWidth={1.5} />
              创建对话任务
            </Link>
            <Link
              to={`/automatic-tasks?create=1&wakerId=${id}`}
              className="hover:bg-muted h-row-navigation type-ui flex items-center justify-center gap-1.5 transition-colors"
            >
              <Clock3 className="size-icon-sm" strokeWidth={1.5} />
              {t("create")}
            </Link>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  aria-label="更多操作"
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
                  编辑资料
                </DropdownMenuItem>
                {!builtin ? (
                  <DropdownMenuItem
                    className="text-destructive data-[highlighted]:text-destructive"
                    onSelect={() => setDeleting(true)}
                  >
                    <Trash2 />
                    删除伙伴
                  </DropdownMenuItem>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>
          </CardFooter>
        </article>
      </Card>
      <WakerFormDialog open={editing} onOpenChange={setEditing} waker={waker} />
      {!builtin ? (
        <WakerDeleteDialog
          open={deleting}
          onOpenChange={setDeleting}
          waker={waker}
        />
      ) : null}
    </>
  );
}
