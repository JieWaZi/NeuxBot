import { StatusNotice } from "@/shared/ui/status-notice";
import {
  hashKey,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useState, useEffect, useEffectEvent, useRef } from "react";
import { RefreshCw, Unplug } from "lucide-react";
import type {
  GetPluginAuthorizationResponse,
  StartPluginAuthorizationResponse,
} from "@/shared/api/generated/types.gen";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { PluginAuthorizationActions } from "./plugin-authorization-dialog";
import { pluginLocale } from "../model/plugin-view";
import { Button } from "@/shared/ui/button";
import { QueryState } from "@/shared/ui/query-state";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import {
  getPluginAuthorizationOptions,
  startPluginAuthorizationMutation,
  cancelPluginAuthorizationMutation,
  logoutPluginAuthorizationMutation,
  invalidatePluginQueries,
} from "../api/plugin-queries";

export function PluginAuthPanel({
  pluginId,
  wakerId = "",
  versionId,
  onReady,
  installation = false,
  starting = false,
  preferredAction,
  onRestart,
  onSessionStarted,
  onAvailabilityChange,
}: {
  pluginId: string;
  wakerId?: string;
  versionId?: string;
  onReady?: () => void;
  onAvailabilityChange?: (available: boolean) => void;
  onSessionStarted?: (session: StartPluginAuthorizationResponse) => void;
  installation?: boolean;
  starting?: boolean;
  preferredAction?: "show-qr" | "open-url" | "";
  onRestart?: (additionalScopes: string[]) => Promise<void>;
}) {
  const { t, i18n } = useTranslation("plugin");
  const client = useQueryClient();
  const active = useRouteSceneActive();
  const scope = {
    path: { pluginId },
    query: { wakerId, versionId, verify: true },
  };
  const options = getPluginAuthorizationOptions(withLocalErrorSurface(scope));
  const status = useQuery({ ...options, enabled: active });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    active,
  });
  const [openError, setOpenError] = useState("");
  const [expiredDevice, setExpiredDevice] = useState("");
  const refresh = async () => {
    await invalidatePluginQueries(client);
  };
  const previousWaitingSession = useRef("");
  const login = useMutation({
    ...withLocalErrorMutation(startPluginAuthorizationMutation()),
    onError: () => {
      void status.refetch();
    },
    onSuccess: async (session) => {
      if (session.id && session.id !== previousWaitingSession.current)
        onSessionStarted?.(session);
      await refresh();
    },
  });
  const cancel = useMutation({
    ...withLocalErrorMutation(cancelPluginAuthorizationMutation()),
    onSuccess: refresh,
  });
  const logout = useMutation({
    ...withLocalErrorMutation(logoutPluginAuthorizationMutation()),
    onError: () => {
      void status.refetch();
    },
    onSuccess: refresh,
  });
  const account = status.data?.account;
  const activeUses = status.data?.activeUses ?? [];
  const session = status.data?.session;
  const noticeMessage =
    session?.notice?.messages?.[
      pluginLocale(i18n.resolvedLanguage ?? i18n.language)
    ]?.trim() || session?.notice?.messages?.en?.trim();
  const deviceKey = `${session?.id ?? ""}:${session?.challengeId ?? ""}:${session?.expiresAt ?? ""}`;
  const expired = session?.state === "expired" || expiredDevice === deviceKey;
  const waiting = session?.state === "waiting" && !expired;
  const ready = Boolean(account?.available);
  const pending =
    starting || login.isPending || cancel.isPending || logout.isPending;
  const hasQR = Boolean(
    session?.actions?.some(
      (action) => action.type === "show-qr" && action.value,
    ),
  );
  const hasActions = Boolean(
    session?.actions?.some((action) =>
      action.type === "show-qr" || action.type === "copy-code"
        ? action.value
        : action.type === "open-url" &&
          action.url &&
          /^https?:\/\//.test(action.url),
    ),
  );
  const preparing = status.isPending || pending || (waiting && !hasActions);
  const qrMode = preferredAction === "show-qr" || hasQR;
  const qrState: "loading" | "expired" | undefined =
    qrMode && (status.isPending || pending || (waiting && !hasQR))
      ? "loading"
      : qrMode && expired
        ? "expired"
        : undefined;
  const showQR = qrMode && (status.isPending || waiting || pending || expired);
  const error = login.error ?? cancel.error ?? logout.error;
  const message = expired
    ? t("authExpired")
    : session?.state === "failed"
      ? t("authFailed")
      : session?.state === "cancelled"
        ? t("authCancelled")
        : t("authUnknown");
  const notifyReady = useEffectEvent(() => onReady?.());
  const notifyAvailability = useEffectEvent((available: boolean) =>
    onAvailabilityChange?.(available),
  );
  const notifiedSession = useRef("");
  const statusHash = hashKey(options.queryKey);
  useEffect(() => {
    const cache = client.getQueryCache();
    const notify = () => {
      const current =
        cache.get<GetPluginAuthorizationResponse>(statusHash)?.state.data;
      notifyAvailability(
        Boolean(current?.account?.available) &&
          current?.session?.state !== "waiting",
      );
      const id = current?.session?.id;
      if (
        id &&
        current.session?.state === "ready" &&
        current.account?.available &&
        notifiedSession.current !== id
      ) {
        notifiedSession.current = id;
        notifyReady();
      }
    };
    const close = cache.subscribe((event) => {
      if (
        event.type === "updated" &&
        event.action.type === "success" &&
        event.query.queryHash === statusHash
      )
        notify();
      if (
        event.type === "updated" &&
        event.action.type === "error" &&
        event.query.queryHash === statusHash
      )
        notifyAvailability(false);
    });
    notify();
    return close;
  }, [client, statusHash]);
  const onDeadline = useEffectEvent((expiredKey: string) => {
    setExpiredDevice(expiredKey);
    void status.refetch();
  });
  useEffect(() => {
    if (session?.state !== "waiting" || !session.expiresAt) return;
    // 当前设备期限只唤醒一次：移除旧二维码并对账，不轮询认证状态。
    const timer = setTimeout(
      () => onDeadline(deviceKey),
      Math.max(0, session.expiresAt - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [deviceKey, session?.expiresAt, session?.state]);
  async function restart() {
    setOpenError("");
    onAvailabilityChange?.(false);
    if (session?.state === "waiting") setExpiredDevice(deviceKey);
    try {
      previousWaitingSession.current =
        session?.state === "waiting" ? (session.id ?? "") : "";
      if (onRestart) {
        await onRestart(status.data?.missingScopes ?? []);
        return;
      }
      if (session?.state === "waiting" && session.id) {
        await cancel.mutateAsync({
          path: { pluginId, sessionId: session.id },
          query: { wakerId, versionId },
        });
      }
      await login.mutateAsync({
        path: { pluginId },
        query: { wakerId, versionId },
        body: { additionalScopes: status.data?.missingScopes ?? [] },
      });
    } catch {
      // Mutation 的稳定错误在当前面板统一呈现。
    }
  }
  return (
    <section className="space-y-4">
      {!installation && (
        <div>
          <h3 className="type-label font-medium">{t("authentication")}</h3>
        </div>
      )}
      {(status.isPending && !installation && preferredAction !== "show-qr") ||
      status.isError ? (
        <QueryState
          loadingLayout="content"
          pending={status.isPending}
          error={status.error}
          onRetry={() => void status.refetch()}
        />
      ) : (
        <>
          {ready && !waiting ? (
            <StatusNotice tone="success">
              {t("sharedIdentity", { name: account?.name || account?.id })}
            </StatusNotice>
          ) : (
            <p className="type-body text-content-secondary" role="status">
              {waiting
                ? t("authorizationWaiting")
                : pending || status.isPending
                  ? t("busy")
                  : message}
            </p>
          )}
          {waiting && noticeMessage && (
            <StatusNotice tone="warning">{noticeMessage}</StatusNotice>
          )}
          {(waiting || qrState || preparing) && (
            <PluginAuthorizationActions
              key={deviceKey}
              actions={waiting && !pending ? (session?.actions ?? []) : []}
              qrState={qrState}
              preparing={preparing}
              disabled={pending || activeUses.length > 0}
              onRefresh={() => void restart()}
              onOpenError={() => setOpenError(t("failed"))}
            />
          )}
          {waiting && Boolean(session?.expiresAt) && (
            <p className="type-caption text-muted-foreground">
              {t("localWaitUntil", {
                time: new Intl.DateTimeFormat(undefined, {
                  timeStyle: "medium",
                }).format(session?.expiresAt),
              })}
            </p>
          )}
          {!waiting &&
            !pending &&
            Boolean(account?.id || account?.name) &&
            Boolean(status.data?.missingScopes?.length) && (
              <p className="type-body text-content-secondary">
                {t("missingScopesHelp")}
              </p>
            )}
          <div className="flex flex-wrap gap-2">
            {(!installation || (!waiting && !pending && !status.isPending)) &&
              (!ready || waiting) &&
              !showQR && (
                <Button
                  type="button"
                  disabled={pending || activeUses.length > 0}
                  onClick={() => void restart()}
                >
                  {t(
                    installation
                      ? "retry"
                      : account?.configured || expired || waiting
                        ? "retryAuthorization"
                        : "login",
                  )}
                </Button>
              )}
            {!installation && waiting && session?.id && (
              <Button
                type="button"
                variant="secondary"
                disabled={pending}
                onClick={() =>
                  cancel.mutate({
                    path: { pluginId, sessionId: session.id ?? "" },
                    query: { wakerId, versionId },
                  })
                }
              >
                {t("cancelAuth")}
              </Button>
            )}
            {!installation && !waiting && !pending && !showQR && (
              <Button
                type="button"
                variant="ghost"
                onClick={() => void status.refetch().then(refresh)}
              >
                <RefreshCw aria-hidden="true" />
                {t("authRefresh")}
              </Button>
            )}
            {ready && !installation && (
              <Button
                type="button"
                variant="ghost"
                disabled={pending || activeUses.length > 0}
                onClick={() =>
                  logout.mutate({
                    path: { pluginId },
                    query: { wakerId, versionId },
                  })
                }
              >
                <Unplug aria-hidden="true" />
                {t("logout")}
              </Button>
            )}
          </div>
          {activeUses.length > 0 && (
            <section className="space-y-2">
              <h4 className="type-label">{t("activeWork")}</h4>
              {activeUses.map((use) => (
                <Link
                  key={use.executionId}
                  to={`/chats/${use.chatId}`}
                  className="type-body text-primary block underline underline-offset-4"
                >
                  {use.title || t("viewActiveWork")}
                </Link>
              ))}
              <p className="type-body text-content-secondary">
                {t("accountInUse")}
              </p>
            </section>
          )}
          <p className="type-caption text-muted-foreground">
            {t("authorizationScopeHelp")}
          </p>
        </>
      )}
      {(error || openError) && (
        <p role="alert" className="type-body text-destructive">
          {openError || localApiErrorMessage(error, t("failed"))}
        </p>
      )}
    </section>
  );
}
