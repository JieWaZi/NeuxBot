import {
  Eye,
  MessageSquarePlus,
  Pencil,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/shared/ui/badge";
import { Button } from "@/shared/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import type { IMBinding, IMConnection } from "../types";
import { ConnectionDeleteDialog } from "./connection-delete-dialog";
import { ConnectionDialog } from "./connection-dialog";
import { ConnectionAuthorizationDialog } from "./connection-authorization-dialog";
import { ConnectionNewConversationDialog } from "./connection-new-conversation-dialog";
import { ConnectionStatus } from "./connection-status";

export function ConnectionSettingsRow({
  connection,
  bindings,
  onView,
  onSaved,
}: {
  connection: IMConnection;
  bindings: IMBinding[];
  onView: (trigger: HTMLElement) => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation("im");
  const [action, setAction] = useState<
    "edit" | "authorize" | "new" | "delete"
  >();
  const pendingAuthorization = bindings.some(
    (binding) => binding.state !== "approved",
  );
  const editLabel = t("editNamedConnection", { name: connection.name });
  const deleteLabel = t("deleteNamedConnection", { name: connection.name });
  const viewLabel = t("viewConnection", { name: connection.name });
  const authorizeLabel = t("authorizeNamedConnection", {
    name: connection.name,
  });
  const newLabel = t("newNamedConversation", { name: connection.name });
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
      <div className="min-w-0 flex-1 basis-40">
        <p className="type-body text-content-primary font-medium [overflow-wrap:anywhere]">
          {connection.name}
        </p>
        <p className="type-ui text-content-secondary mt-1 [overflow-wrap:anywhere]">
          {connection.appId || t("notConnected")}
        </p>
        {connection.errorCode ? (
          <p role="alert" className="type-ui text-destructive mt-1">
            {t("errors." + connection.errorCode, {
              defaultValue: t("needsAttention"),
            })}
          </p>
        ) : null}
      </div>
      <div className="flex max-w-full flex-wrap items-center gap-x-3 gap-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <ConnectionStatus connection={connection} appearance="inline" />
          {pendingAuthorization ? (
            <Badge variant="warning">{t("pendingAuthorization")}</Badge>
          ) : null}
        </div>
        <div
          role="group"
          aria-label={t("connectionActions", { name: connection.name })}
          className="flex max-w-full flex-wrap items-center gap-1"
        >
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={viewLabel}
                  onClick={(event) => onView(event.currentTarget)}
                >
                  <Eye />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{viewLabel}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={editLabel}
                  onClick={() => setAction("edit")}
                >
                  <Pencil />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{editLabel}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={authorizeLabel}
                  onClick={() => setAction("authorize")}
                >
                  <ShieldCheck />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{authorizeLabel}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={newLabel}
                  onClick={() => setAction("new")}
                >
                  <MessageSquarePlus />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{newLabel}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="danger-quiet"
                  size="icon"
                  aria-label={deleteLabel}
                  onClick={() => setAction("delete")}
                >
                  <Trash2 />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{deleteLabel}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </div>
      </div>
      {action === "edit" ? (
        <ConnectionDialog
          connection={connection}
          onClose={() => setAction(undefined)}
          onSaved={onSaved}
        />
      ) : null}
      {action === "authorize" ? (
        <ConnectionAuthorizationDialog
          connectionId={connection.id || ""}
          onClose={() => setAction(undefined)}
          onSaved={onSaved}
        />
      ) : null}
      {action === "new" ? (
        <ConnectionNewConversationDialog
          connectionId={connection.id || ""}
          onClose={() => setAction(undefined)}
          onSaved={onSaved}
        />
      ) : null}
      {action === "delete" ? (
        <ConnectionDeleteDialog
          connection={connection}
          onClose={() => setAction(undefined)}
          onDeleted={onSaved}
        />
      ) : null}
    </div>
  );
}
