import { Pencil } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { SettingsContentShell } from "@/modules/settings";
import { Button } from "@/shared/ui/button";
import { ConnectionConfigurationForm } from "./components/connection-dialog";
import { ConnectionAuthorizationDialog } from "./components/connection-authorization-dialog";
import { ConnectionNewConversationDialog } from "./components/connection-new-conversation-dialog";
import { ConnectionDeleteDialog } from "./components/connection-delete-dialog";
import { IMConnectionDetails } from "./im-connection-details";
import { useIMBindings, useIMConnections } from "./queries";
import type { IMPlatform } from "./types";

export function IMConnectionDetailPage({
  connectionId: suppliedId,
  initialBindingId: suppliedBinding,
  onBack,
}: {
  connectionId?: string;
  initialBindingId?: string;
  onBack?: (platform?: IMPlatform) => void;
}) {
  const route = useParams();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const connectionId = suppliedId || route.connectionId || "";
  return (
    <ConnectionPage
      key={connectionId}
      connectionId={connectionId}
      initialBindingId={
        suppliedBinding ||
        params.get("binding") ||
        params.get("chat") ||
        undefined
      }
      onBack={
        onBack ||
        ((platform) => navigate(platform ? `/im?platform=${platform}` : "/im"))
      }
    />
  );
}

function ConnectionPage({
  connectionId,
  initialBindingId,
  onBack,
}: {
  connectionId: string;
  initialBindingId?: string;
  onBack: (platform?: IMPlatform) => void;
}) {
  const { t } = useTranslation("im");
  const query = useIMConnections();
  const bindingQuery = useIMBindings(connectionId);
  const newConversationHintId = useId();
  const hasAuthorizedChat =
    bindingQuery.data?.some((item) => item.id && item.state === "approved") &&
    !bindingQuery.isError;
  const editTriggerId = useId();
  const connection = query.data?.resources?.find(
    (item) => item.id === connectionId,
  );
  const [action, setAction] = useState<
    "edit" | "authorize" | "new" | "delete"
  >();
  const [editingBindingId, setEditingBindingId] = useState<string>();
  const editing = action === "edit" || Boolean(editingBindingId);
  const refresh = () => {
    void query.refresh();
  };
  const back = () => onBack(connection?.platform || undefined);
  return (
    <SettingsContentShell showHeader={false}>
      <IMConnectionDetails
        connectionId={connectionId}
        initialBindingId={initialBindingId}
        editingBindingId={editingBindingId}
        onEditBinding={setEditingBindingId}
        onBindingSaved={refresh}
        editDisabled={editing}
        configurationContent={
          connection && action === "edit" ? (
            <ConnectionConfigurationForm
              connection={connection}
              includeBindings={false}
              statusStale={query.isError}
              onSaved={refresh}
              onClose={() => {
                setAction(undefined);
                requestAnimationFrame(() =>
                  document.getElementById(editTriggerId)?.focus(),
                );
              }}
            />
          ) : undefined
        }
        configurationAction={
          connection && !editing ? (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    id={editTriggerId}
                    variant="plain"
                    size="icon"
                    aria-label={t("editConnection")}
                    onClick={() => setAction("edit")}
                  >
                    <Pencil />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t("editConnection")}</TooltipContent>
              </Tooltip>
            </TooltipProvider>
          ) : undefined
        }
        authorizationAction={
          <Button
            variant="settings-outline"
            size="settings"
            disabled={editing}
            onClick={() => setAction("authorize")}
          >
            {t("authorize")}
          </Button>
        }
        conversationAction={
          <Button
            variant="settings-outline"
            size="settings"
            disabled={editing || !hasAuthorizedChat}
            aria-describedby={
              !hasAuthorizedChat ? newConversationHintId : undefined
            }
            onClick={() => setAction("new")}
          >
            {t("newConversation")}
          </Button>
        }
        deleteAction={
          <Button
            variant="danger-quiet"
            size="settings"
            disabled={editing}
            onClick={() => setAction("delete")}
          >
            {t("delete")}
          </Button>
        }
      />
      {!hasAuthorizedChat ? (
        <span id={newConversationHintId} className="sr-only">
          {t("noAuthorizedChats")}
        </span>
      ) : null}
      {action === "authorize" ? (
        <ConnectionAuthorizationDialog
          connectionId={connectionId}
          onSaved={refresh}
          onClose={() => setAction(undefined)}
        />
      ) : null}
      {action === "new" ? (
        <ConnectionNewConversationDialog
          connectionId={connectionId}
          onSaved={refresh}
          onClose={() => setAction(undefined)}
        />
      ) : null}
      {action === "delete" && connection ? (
        <ConnectionDeleteDialog
          connection={connection}
          onClose={() => setAction(undefined)}
          onDeleted={() => {
            refresh();
            back();
          }}
        />
      ) : null}
    </SettingsContentShell>
  );
}
