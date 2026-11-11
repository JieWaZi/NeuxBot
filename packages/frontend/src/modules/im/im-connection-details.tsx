import { useId, type ReactNode } from "react";
import { QueryState } from "@/shared/ui/query-state";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { useTranslation } from "react-i18next";
import {
  SettingsCard,
  SettingsDangerZone,
  SettingsFieldGroup,
  SettingsFieldRow,
  SettingsGroup,
} from "@/modules/settings";
import { BindingForm } from "./components/binding-form";
import { Button } from "@/shared/ui/button";
import { Skeleton } from "@/shared/ui/skeleton";
import { BindingOverview } from "./components/binding-overview";
import { ConnectionConversationsTable } from "./components/connection-conversations-table";
import {
  BindingStatus,
  ConnectionStatus,
} from "./components/connection-status";
import { useIMBindings, useIMConnections } from "./queries";

export function IMConnectionDetails({
  connectionId,
  initialBindingId,
  authorizationAction,
  conversationAction,
  deleteAction,
  configurationAction,
  configurationContent,
  editingBindingId,
  onEditBinding,
  onBindingSaved,
  editDisabled,
}: {
  connectionId: string;
  initialBindingId?: string;
  authorizationAction?: ReactNode;
  conversationAction?: ReactNode;
  deleteAction?: ReactNode;
  configurationAction?: ReactNode;
  configurationContent?: ReactNode;
  editingBindingId?: string;
  onEditBinding?: (id: string | undefined) => void;
  onBindingSaved?: () => void;
  editDisabled?: boolean;
}) {
  const bindingActionId = useId();
  const closeBindingEditor = (id?: string) => {
    onEditBinding?.(undefined);
    requestAnimationFrame(() =>
      document.getElementById(`${bindingActionId}-${id}`)?.focus(),
    );
  };
  const { t, i18n } = useTranslation("im");
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
    <div className="grid min-h-0 gap-6">
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
          <SettingsGroup
            title={t("connectionInformation")}
            trailing={configurationAction}
          >
            <SettingsCard>
              {query.isError ? (
                <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
                  <p role="alert" className="type-body text-destructive">
                    {localApiErrorMessage(query.error, t("operationFailed"))}
                  </p>
                  <Button
                    variant="settings-outline"
                    size="settings"
                    disabled={query.isFetching}
                    onClick={() => void query.refetch()}
                  >
                    {t("reload")}
                  </Button>
                </div>
              ) : null}
              {configurationContent || (
                <SettingsFieldGroup surface="plain">
                  <SettingsFieldRow label={t("state")}>
                    <ConnectionStatus
                      connection={connection}
                      stale={query.isError}
                      appearance="pill"
                    />
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
              )}
              {connection.errorCode ? (
                <p role="alert" className="text-destructive type-body">
                  {t("errors." + connection.errorCode, {
                    defaultValue: t("needsAttention"),
                  })}
                </p>
              ) : null}
            </SettingsCard>
          </SettingsGroup>
          <SettingsGroup title={t("chatAccess")} trailing={authorizationAction}>
            <SettingsCard>
              {bindingsQuery.isError ? (
                <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
                  <p role="alert" className="type-body text-destructive">
                    {localApiErrorMessage(
                      bindingsQuery.error,
                      t("authorizationReadFailed"),
                    )}
                  </p>
                  <Button
                    variant="settings-outline"
                    size="settings"
                    disabled={bindingsQuery.isFetching}
                    onClick={() => void bindingsQuery.refetch()}
                  >
                    {t("reload")}
                  </Button>
                </div>
              ) : null}
              {bindingsQuery.isPending ? (
                <ConnectionDetailSkeleton label={t("loading")} />
              ) : bindings.length ? (
                bindings.map((item) => (
                  <div
                    key={item.id}
                    className="before:bg-border-subtle relative grid min-w-0 gap-3 px-4 py-3 before:absolute before:inset-x-4 before:top-0 before:h-px first:before:hidden"
                  >
                    <header className="flex flex-wrap items-center justify-between gap-3">
                      <h4 className="type-body min-w-0 font-medium [overflow-wrap:anywhere]">
                        {item.name || item.externalChatId || t("chatAccess")}
                      </h4>
                      <BindingStatus binding={item} />
                    </header>
                    {editingBindingId === item.id ? (
                      <BindingForm
                        binding={item}
                        host="page"
                        onClose={() => closeBindingEditor(item.id)}
                        onSaved={onBindingSaved ?? (() => undefined)}
                      />
                    ) : (
                      <>
                        <BindingOverview binding={item} compact />
                        {onEditBinding ? (
                          <div className="flex justify-end">
                            <Button
                              id={`${bindingActionId}-${item.id}`}
                              size="settings"
                              variant="ghost"
                              aria-label={t("configureNamedChat", {
                                name: item.name || item.externalChatId,
                              })}
                              disabled={editDisabled}
                              onClick={() => onEditBinding(item.id)}
                            >
                              {t("configureChat")}
                            </Button>
                          </div>
                        ) : null}
                      </>
                    )}
                  </div>
                ))
              ) : !bindingsQuery.isError ? (
                <p className="text-content-secondary type-body px-4 py-3">
                  {t(
                    connection.platform === "wecom"
                      ? "wecomFirstMessage"
                      : "waitingForChat",
                  )}
                </p>
              ) : null}
            </SettingsCard>
          </SettingsGroup>
          <ConnectionConversationsTable
            trailing={conversationAction}
            connectionId={connectionId}
            bindings={bindings}
            ready={!bindingsQuery.isPending && !bindingsQuery.isError}
            sourceError={bindingsQuery.error}
            onRetrySource={() => void bindingsQuery.refetch()}
          />
          {deleteAction ? (
            <SettingsDangerZone
              title={t("dangerZone")}
              actionTitle={t("deleteConnection")}
              description={t("deleteConnectionHint", { name: connection.name })}
              action={deleteAction}
            />
          ) : null}
        </>
      )}
    </div>
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
