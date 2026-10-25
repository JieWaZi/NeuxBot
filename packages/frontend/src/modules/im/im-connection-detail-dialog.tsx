import { useDialogFocusRestore } from "./components/use-dialog-focus-restore";
import { QueryState } from "@/shared/ui/query-state";
import { useTranslation } from "react-i18next";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import {
  SettingsFieldGroup,
  SettingsFieldRow,
  SettingsGroup,
} from "@/modules/settings";
import { Skeleton } from "@/shared/ui/skeleton";
import { BindingOverview } from "./components/binding-overview";
import { ConnectionConversationsTable } from "./components/connection-conversations-table";
import {
  BindingStatus,
  ConnectionStatus,
} from "./components/connection-status";
import { useIMBindings, useIMConnections } from "./queries";

type IMConnectionDetailDialogProps = {
  connectionId: string;
  initialBindingId?: string;
  returnFocusTo?: HTMLElement | null;
  onClose: () => void;
};

export function IMConnectionDetailDialog(props: IMConnectionDetailDialogProps) {
  return <ConnectionDetailDialogContent key={props.connectionId} {...props} />;
}

function ConnectionDetailDialogContent({
  connectionId,
  initialBindingId,
  returnFocusTo,
  onClose,
}: IMConnectionDetailDialogProps) {
  const { t, i18n } = useTranslation("im");
  const dialogFocus = useDialogFocusRestore(returnFocusTo);
  const query = useIMConnections();
  const bindingsQuery = useIMBindings(connectionId);
  const bindings = initialBindingId
    ? [...(bindingsQuery.data || [])].sort(
        (left, right) =>
          Number(right.id === initialBindingId) -
          Number(left.id === initialBindingId),
      )
    : bindingsQuery.data || [];
  const connection = query.data?.resources?.find(
    (item) => item.id === connectionId,
  );
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent
        {...dialogFocus}
        size="lg"
        title={connection?.name || t("connectionDetail")}
        description={t("connectionDetailDescription")}
      >
        <div className="grid min-h-0 gap-6 overflow-y-auto px-6 pb-6">
          {!connection && query.isPending ? (
            <ConnectionDetailSkeleton label={t("loading")} />
          ) : !connection ? (
            <QueryState
              error={query.error}
              emptyLabel={t("connectionNotFound")}
              onRetry={() => void query.refetch()}
            />
          ) : (
            <>
              {query.isError ? (
                <QueryState
                  error={query.error}
                  onRetry={() => void query.refetch()}
                />
              ) : null}
              <SettingsGroup title={t("connectionInformation")}>
                <SettingsFieldGroup>
                  <SettingsFieldRow label={t("state")}>
                    <ConnectionStatus connection={connection} />
                  </SettingsFieldRow>
                  <SettingsFieldRow
                    label={t(
                      connection.platform === "wecom"
                        ? "wecomBotId"
                        : connection.platform === "feishu"
                          ? "appId"
                          : "botAccount",
                    )}
                  >
                    <p className="type-ui text-content-secondary break-all select-text">
                      {connection.appId || t("notConnected")}
                    </p>
                  </SettingsFieldRow>
                  <SettingsFieldRow label={t("createdAt")}>
                    <p className="type-ui text-content-secondary">
                      {connection.createdAt
                        ? new Intl.DateTimeFormat(i18n.resolvedLanguage, {
                            dateStyle: "medium",
                            timeStyle: "short",
                          }).format(new Date(connection.createdAt))
                        : "—"}
                    </p>
                  </SettingsFieldRow>
                </SettingsFieldGroup>
                {connection.errorCode ? (
                  <p role="alert" className="text-destructive type-body">
                    {t("errors." + connection.errorCode, {
                      defaultValue: t("needsAttention"),
                    })}
                  </p>
                ) : null}
              </SettingsGroup>
              <SettingsGroup
                title={t("chatAccess")}
                description={t("chatNameHint")}
              >
                {bindingsQuery.isError ? (
                  <QueryState
                    error={bindingsQuery.error}
                    onRetry={() => void bindingsQuery.refetch()}
                  />
                ) : bindingsQuery.isPending ? (
                  <ConnectionDetailSkeleton label={t("loading")} />
                ) : bindings.length ? (
                  bindings.map((item) => (
                    <div key={item.id} className="grid min-w-0 gap-3">
                      <header className="flex flex-wrap items-center justify-between gap-3">
                        <h4 className="type-body min-w-0 font-medium [overflow-wrap:anywhere]">
                          {item.name || item.externalChatId || t("chatAccess")}
                        </h4>
                        <BindingStatus binding={item} />
                      </header>
                      <BindingOverview binding={item} />
                    </div>
                  ))
                ) : (
                  <p className="text-content-secondary type-body">
                    {t(
                      connection.platform === "wecom"
                        ? "wecomFirstMessage"
                        : "waitingForChat",
                    )}
                  </p>
                )}
              </SettingsGroup>
              <ConnectionConversationsTable
                connectionId={connectionId}
                bindings={bindings}
                ready={!bindingsQuery.isPending && !bindingsQuery.isError}
                sourceError={bindingsQuery.error}
                onRetrySource={() => void bindingsQuery.refetch()}
              />
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function ConnectionDetailSkeleton({ label }: { label: string }) {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label={label}
      className="grid gap-3"
    >
      <Skeleton className="h-4 w-24" />
      <SettingsFieldGroup>
        {["account", "status", "created"].map((field) => (
          <SettingsFieldRow
            key={field}
            label={<Skeleton className="h-4 w-20" />}
          >
            <Skeleton className="h-4 w-2/3" />
          </SettingsFieldRow>
        ))}
      </SettingsFieldGroup>
    </div>
  );
}
