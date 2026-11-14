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
  CheckPluginAccountData,
} from "@/shared/api/generated/types.gen";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { PluginAuthorizationActions } from "./plugin-authorization-dialog";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { pluginAccountContext } from "../model/plugin-account-context";
import { pluginLocale } from "../model/plugin-view";
import { Button } from "@/shared/ui/button";
import { QueryState } from "@/shared/ui/query-state";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { applicationErrorMessage } from "@/shared/api/error-presentation";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import {
  getPluginAuthorizationOptions,
  checkPluginAccountMutation,
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
  versionId: string;
  onReady?: () => void;
  onAvailabilityChange?: (available: boolean) => void;
  onSessionStarted?: (
    session: StartPluginAuthorizationResponse,
    context: CheckPluginAccountData["body"] & { ownershipNonce: string },
  ) => void;
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
    query: { wakerId, versionId },
  };
  const options = getPluginAuthorizationOptions(withLocalErrorSurface(scope));
  const status = useQuery({
    ...options,
    enabled: active && Boolean(pluginId && versionId),
  });
  const context = pluginAccountContext(status.data);
  const ownedContexts = useRef(
    new Map<
      string,
      CheckPluginAccountData["body"] & { ownershipNonce: string }
    >(),
  );
  const [confirmLogout, setConfirmLogout] = useState(false);
  const alive = useRef(true);
  const selfOwned = useRef(new Map<string, StartPluginAuthorizationResponse>());
  const refresh = async () => {
    await invalidatePluginQueries(client);
  };
  const cancel = useMutation({
    ...withLocalErrorMutation(cancelPluginAuthorizationMutation()),
    onSuccess: async (_value, variables) => {
      selfOwned.current.delete(variables.path.sessionId);
      ownedContexts.current.delete(variables.path.sessionId);
      await refresh();
    },
  });
  function closeOwned() {
    for (const [id, started] of selfOwned.current) {
      const cached = client.getQueryData<GetPluginAuthorizationResponse>(
        options.queryKey,
      )?.session;
      const body = ownedContexts.current.get(id);
      if (
        !body ||
        started.state !== "waiting" ||
        (cached?.id === id && cached.state !== "waiting")
      )
        continue;
      selfOwned.current.delete(id);
      void cancel
        .mutateAsync({
          path: { pluginId, sessionId: id },
          query: { wakerId, versionId },
          body,
        })
        .catch(() => undefined);
    }
  }
  const closeOnUnmount = useEffectEvent(() => closeOwned());
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      closeOnUnmount();
    };
  }, []);
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    active,
  });
  const [openError, setOpenError] = useState("");
  const [expiredDevice, setExpiredDevice] = useState("");
  const login = useMutation({
    ...withLocalErrorMutation(startPluginAuthorizationMutation()),
    onError: () => {
      void status.refetch();
    },
    onSuccess: async (session, variables) => {
      const {
        expectedConfigurationRevision,
        expectedAccountGeneration,
        ownershipNonce,
      } = variables.body;
      if (session.created && session.id && ownershipNonce) {
        const owned = {
          expectedConfigurationRevision,
          expectedAccountGeneration,
          ownershipNonce,
        };
        ownedContexts.current.set(session.id, owned);
        if (onSessionStarted) onSessionStarted(session, owned);
        else selfOwned.current.set(session.id, session);
      }
      if (!alive.current) closeOwned();
      await refresh();
    },
  });
  const logout = useMutation({
    ...withLocalErrorMutation(logoutPluginAuthorizationMutation()),
    onError: () => {
      void status.refetch();
    },
    onSuccess: async () => {
      setConfirmLogout(false);
      await refresh();
    },
  });
  const check = useMutation({
    ...withLocalErrorMutation(checkPluginAccountMutation()),
    onSuccess: refresh,
    onError: () => {
      void status.refetch();
    },
  });
  const account = status.data?.account;
  const activeUses = status.data?.activeUses ?? [];
  // 账号接口已返回同一上下文的完整会话；不再并行查询可能刚被替换的旧会话。
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
    starting ||
    login.isPending ||
    cancel.isPending ||
    logout.isPending ||
    check.isPending;
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
  const error = login.error ?? cancel.error ?? logout.error ?? check.error;
  const message = expired
    ? t("authExpired")
    : session?.state === "failed"
      ? applicationErrorMessage({
          code: session.errorCode,
          fallback: t("authFailed"),
        })
      : session?.state === "cancelled"
        ? t("authCancelled")
        : t(account?.checkedAt ? "authUnknown" : "accountUnchecked");
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
    if (!context) return;
    setOpenError("");
    onAvailabilityChange?.(false);
    if (session?.state === "waiting") setExpiredDevice(deviceKey);
    try {
      if (onRestart) {
        await onRestart(status.data?.missingScopes ?? []);
        return;
      }
      if (session?.state === "waiting" && session.id) {
        await cancel.mutateAsync({
          path: { pluginId, sessionId: session.id },
          query: { wakerId, versionId },
          body: context,
        });
      }
      await login.mutateAsync({
        path: { pluginId },
        query: { wakerId, versionId },
        body: {
          ...context,
          ownershipNonce: crypto.randomUUID(),
          additionalScopes: status.data?.missingScopes ?? [],
        },
      });
    } catch {
      // Mutation 的稳定错误在当前面板统一呈现。
    }
  }
  return (
    <section className={installation ? "space-y-4 text-center" : "space-y-4"}>
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
          ) : !waiting &&
            !pending &&
            !status.isPending &&
            (expired ||
              session?.state === "failed" ||
              session?.state === "cancelled") ? (
            <StatusNotice role="status">{message}</StatusNotice>
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
              disabled={pending || !context || activeUses.length > 0}
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
          <div
            className={
              installation
                ? "flex flex-wrap justify-center gap-2"
                : "flex flex-wrap gap-2"
            }
          >
            {(!installation || (!waiting && !pending && !status.isPending)) &&
              (!ready || waiting) &&
              !showQR && (
                <Button
                  type="button"
                  variant="text"
                  disabled={pending || !context || activeUses.length > 0}
                  onClick={() => void restart()}
                >
                  {t(
                    account?.configured ||
                      expired ||
                      waiting ||
                      session?.state === "failed" ||
                      session?.state === "cancelled"
                      ? "retryAuthorization"
                      : "login",
                  )}
                </Button>
              )}
            {!installation && waiting && session?.id && (
              <Button
                type="button"
                variant="secondary"
                disabled={pending || !context}
                onClick={() => {
                  const body =
                    ownedContexts.current.get(session.id ?? "") ?? context;
                  if (body)
                    cancel.mutate({
                      path: { pluginId, sessionId: session.id ?? "" },
                      query: { wakerId, versionId },
                      body,
                    });
                }}
              >
                {t("cancelAuth")}
              </Button>
            )}
            {!installation && !waiting && !pending && !showQR && (
              <Button
                type="button"
                variant="ghost"
                disabled={pending || !context || activeUses.length > 0}
                onClick={() => {
                  if (context)
                    check.mutate({
                      path: { pluginId },
                      query: { wakerId, versionId },
                      body: context,
                    });
                }}
              >
                <RefreshCw aria-hidden="true" />
                {t("authRefresh")}
              </Button>
            )}
            {ready && !installation && (
              <Button
                type="button"
                variant="ghost"
                disabled={pending || !context || activeUses.length > 0}
                onClick={() => setConfirmLogout(true)}
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
      {confirmLogout && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!logout.isPending) setConfirmLogout(open);
          }}
        >
          <DialogContent title={t("logout")} description={t("logoutHelp")}>
            <DialogFooter>
              <Button
                variant="secondary"
                disabled={logout.isPending}
                onClick={() => setConfirmLogout(false)}
              >
                {t("cancel")}
              </Button>
              <Button
                variant="danger"
                disabled={logout.isPending || !context}
                onClick={() => {
                  if (context)
                    logout.mutate({
                      path: { pluginId },
                      query: { wakerId, versionId },
                      body: context,
                    });
                }}
              >
                {t("confirmLogout")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
      {(error || openError) && (
        <p role="alert" className="type-body text-destructive">
          {openError || localApiErrorMessage(error, t("failed"))}
        </p>
      )}
    </section>
  );
}
