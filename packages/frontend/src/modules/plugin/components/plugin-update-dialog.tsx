import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useEffectEvent, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
  GetPluginAuthorizationResponse,
  StartPluginAuthorizationResponse,
} from "@/shared/api/generated/types.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { QueryState } from "@/shared/ui/query-state";
import { ResourcePartnerAvatar } from "@/shared/ui/resource-management-card";
import {
  cancelPluginAuthorizationMutation,
  getPluginAuthorizationOptions,
  getPluginOptions,
  invalidatePluginQueries,
  prepareCatalogPluginMutation,
  updatePluginConfigurationMutation,
} from "../api/plugin-queries";
import {
  pluginDisplayName,
  pluginHasAuthorization,
  pluginPreferredAuthAction,
  pluginSummary,
  type PluginDetail,
} from "../model/plugin-view";
import { PluginAuthPanel } from "./plugin-auth-panel";
import {
  PluginConfigurationFields,
  usePluginConfigurationDraft,
} from "./plugin-configuration-editor";

/** 准备固定内容与切换安装分开；只有最终确认才原子更新全部伙伴。 */
export function PluginUpdateDialog({
  detail: initial,
  wakerId = "",
  onClose,
}: {
  detail: PluginDetail;
  wakerId?: string;
  onClose: () => void;
}) {
  const { t } = useTranslation("plugin");
  const client = useQueryClient();
  const [snapshot, setSnapshot] = useState(initial);
  const [prepared, setPrepared] = useState<PluginDetail>();
  const [reloadError, setReloadError] = useState<unknown>();
  const alive = useRef(true);
  const ownedSession = useRef<{
    session: StartPluginAuthorizationResponse;
    versionId: string;
  } | null>(null);
  const cancelled = useRef(new Set<string>());
  const pluginId = snapshot.plugin?.id ?? "";
  const versionId = prepared?.resolvedVersionId ?? "";
  const options = getPluginOptions(
    withLocalErrorSurface({
      path: { pluginId },
      query: { wakerId, versionId },
    }),
  );
  const target = useQuery({
    ...options,
    enabled: Boolean(versionId),
    initialData: prepared,
  });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    enabled: Boolean(versionId),
  });
  const cancellation = useMutation(
    withLocalErrorMutation(cancelPluginAuthorizationMutation()),
  );
  async function cancelOwnedSession() {
    const owned = ownedSession.current;
    if (
      !owned?.session.id ||
      owned.session.state !== "waiting" ||
      cancelled.current.has(owned.session.id)
    )
      return;
    const statusKey = getPluginAuthorizationOptions(
      withLocalErrorSurface({
        path: { pluginId },
        query: { wakerId, versionId: owned.versionId, verify: true },
      }),
    ).queryKey;
    const latest =
      client.getQueryData<GetPluginAuthorizationResponse>(statusKey)?.session;
    // 仅回收本弹窗发起且仍等待的授权；不退出共享账号或取消其他会话。
    if (latest?.id === owned.session.id && latest.state !== "waiting") return;
    cancelled.current.add(owned.session.id);
    try {
      await cancellation.mutateAsync({
        path: { pluginId, sessionId: owned.session.id },
        query: { wakerId, versionId: owned.versionId },
      });
    } catch {
      cancelled.current.delete(owned.session.id);
    }
  }
  const abandon = useEffectEvent(() => {
    void cancelOwnedSession();
  });
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      abandon();
    };
  }, []);
  const prepare = useMutation({
    ...withLocalErrorMutation(prepareCatalogPluginMutation()),
    onSuccess: (value) => {
      if (!alive.current) return;
      // 重新准备同一版本时，用本次权威配置替换旧缓存，避免草稿冻结过期修订。
      client.setQueryData(
        getPluginOptions(
          withLocalErrorSurface({
            path: { pluginId },
            query: { wakerId, versionId: value.resolvedVersionId ?? "" },
          }),
        ).queryKey,
        value,
      );
      setPrepared(value);
    },
  });
  const reload = useMutation({
    mutationFn: async () => {
      await cancelOwnedSession();
      return client.fetchQuery({
        ...getPluginOptions(
          withLocalErrorSurface({ path: { pluginId }, query: { wakerId } }),
        ),
        staleTime: 0,
      });
    },
    onSuccess: (value) => {
      if (!alive.current) return;
      setSnapshot(value);
      setPrepared(undefined);
      setReloadError(undefined);
      prepare.reset();
    },
    onError: setReloadError,
  });
  const current = target.data ?? prepared;
  const pkg = (current ?? snapshot).versions?.find(
    (version) =>
      version.id === (current?.resolvedVersionId ?? snapshot.resolvedVersionId),
  )?.package;
  function close() {
    if (!alive.current) return;
    alive.current = false;
    void cancelOwnedSession();
    onClose();
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent
        title={t("updateNamed", {
          name: pluginDisplayName(pkg, snapshot.plugin?.name),
        })}
        description={t("updateHelp")}
        size="lg"
      >
        {current && versionId ? (
          <PreparedPluginUpdate
            key={versionId}
            detail={current}
            snapshot={snapshot}
            versionId={versionId}
            wakerId={wakerId}
            queryError={target.error}
            fetching={target.isFetching}
            onRetry={() => void target.refetch()}
            onAuthorizationStarted={(session) => {
              ownedSession.current = { session, versionId };
              if (!alive.current) void cancelOwnedSession();
            }}
            onAuthorizationReady={() => {
              ownedSession.current = null;
              void target.refetch();
            }}
            onReload={() => reload.mutate()}
            reloading={reload.isPending}
            onClose={close}
          />
        ) : (
          <>
            <div className="min-h-0 space-y-4 overflow-y-auto px-6 pb-6">
              <UpdatePartners detail={snapshot} />
              <p className="type-body text-content-secondary">
                {t("prepareUpdateHelp")}
              </p>
              {prepare.error && (
                <div className="space-y-3">
                  <p role="alert" className="type-body text-destructive">
                    {localApiErrorMessage(prepare.error, t("failed"))}
                  </p>
                  <Button
                    variant="secondary"
                    disabled={reload.isPending}
                    onClick={() => reload.mutate()}
                  >
                    {t("reloadUpdate")}
                  </Button>
                </div>
              )}
            </div>
            <DialogFooter>
              <Button variant="secondary" onClick={close}>
                {t("cancel")}
              </Button>
              <Button
                disabled={
                  prepare.isPending ||
                  reload.isPending ||
                  !snapshot.update?.available ||
                  !snapshot.update.catalogKey ||
                  !snapshot.update.archiveSHA256 ||
                  !snapshot.installations?.length
                }
                onClick={() =>
                  prepare.mutate({
                    path: { key: snapshot.update?.catalogKey ?? "" },
                    body: {
                      ownerWakerId: snapshot.plugin?.ownerWakerId ?? "",
                      expectedArchiveSHA256: snapshot.update?.archiveSHA256,
                      trustConfirmed: true,
                    },
                  })
                }
              >
                {t(prepare.isPending ? "busy" : "prepareUpdate")}
              </Button>
            </DialogFooter>
          </>
        )}
        {Boolean(reloadError) && (
          <p role="alert" className="type-body text-destructive px-6 pb-4">
            {localApiErrorMessage(reloadError, t("failed"))}
          </p>
        )}
      </DialogContent>
    </Dialog>
  );
}

function PreparedPluginUpdate({
  detail,
  snapshot,
  versionId,
  wakerId,
  queryError,
  fetching,
  onRetry,
  onAuthorizationStarted,
  onAuthorizationReady,
  onReload,
  reloading,
  onClose,
}: {
  detail: PluginDetail;
  snapshot: PluginDetail;
  versionId: string;
  wakerId: string;
  queryError: unknown;
  fetching: boolean;
  onRetry: () => void;
  onAuthorizationStarted: (session: StartPluginAuthorizationResponse) => void;
  onAuthorizationReady: () => void;
  onReload: () => void;
  reloading: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation("plugin");
  const client = useQueryClient();
  const draft = usePluginConfigurationDraft(detail);
  const pkg = detail.versions?.find(
    (version) => version.id === versionId,
  )?.package;
  const hasAuth = pluginHasAuthorization(pkg);
  const [authenticated, setAuthenticated] = useState(false);
  const save = useMutation({
    ...withLocalErrorMutation(updatePluginConfigurationMutation()),
    onSuccess: async () => {
      await invalidatePluginQueries(client);
      onClose();
    },
  });
  const pending = save.isPending || reloading;
  const hasConfiguration = Boolean(
    detail.fields?.length ||
    Object.keys(detail.values ?? {}).length ||
    detail.configuredSecrets?.length,
  );
  return (
    <>
      <div className="min-h-0 space-y-5 overflow-y-auto px-6 pb-6">
        <UpdatePartners detail={snapshot} />
        <p className="type-body text-content-secondary">{pluginSummary(pkg)}</p>
        {(pkg?.upstream?.tag || pkg?.upstream?.version) && (
          <p className="type-body">
            {t("packageVersion")} · {pkg.upstream.tag || pkg.upstream.version}
          </p>
        )}
        <p className="type-body text-content-secondary">
          {t("updateReadyHelp")}
        </p>
        {queryError ? (
          <QueryState error={queryError} onRetry={onRetry} />
        ) : null}
        <fieldset disabled={pending} className="min-w-0 space-y-5">
          {hasConfiguration && (
            <PluginConfigurationFields detail={detail} draft={draft} />
          )}
          {hasAuth && (
            <PluginAuthPanel
              pluginId={detail.plugin?.id ?? ""}
              wakerId={wakerId}
              versionId={versionId}
              preferredAction={pluginPreferredAuthAction(pkg)}
              onSessionStarted={onAuthorizationStarted}
              onAvailabilityChange={setAuthenticated}
              onReady={onAuthorizationReady}
            />
          )}
        </fieldset>
        {save.error && (
          <div className="space-y-2">
            <p role="alert" className="type-body text-destructive">
              {localApiErrorMessage(save.error, t("failed"))}
            </p>
            <p className="type-body text-content-secondary">
              {t("updateRetryHelp")}
            </p>
            <Button variant="secondary" disabled={pending} onClick={onReload}>
              {t("reloadUpdate")}
            </Button>
          </div>
        )}
      </div>
      <DialogFooter>
        <Button variant="secondary" disabled={pending} onClick={onClose}>
          {t("cancel")}
        </Button>
        <Button
          disabled={
            pending ||
            fetching ||
            Boolean(queryError) ||
            (hasAuth && !authenticated)
          }
          onClick={() =>
            save.mutate({
              path: { pluginId: detail.plugin?.id ?? "" },
              query: { wakerId },
              body: {
                ...draft.body,
                switches: (snapshot.installations ?? []).map((relation) => ({
                  wakerId: relation.wakerId ?? "",
                  expectedVersionId: relation.versionId ?? "",
                  versionId,
                })),
              },
            })
          }
        >
          {t(pending ? "busy" : "update")}
        </Button>
      </DialogFooter>
    </>
  );
}

function UpdatePartners({ detail }: { detail: PluginDetail }) {
  const { t } = useTranslation("plugin");
  return (
    <section className="space-y-3">
      <h3 className="type-label font-medium">
        {t("updatePartners", { count: detail.installations?.length ?? 0 })}
      </h3>
      <div className="flex flex-wrap gap-3">
        {(detail.installations ?? []).map((relation) => {
          const id = relation.wakerId ?? "";
          const name = detail.partnerNames?.[id] || t("unknown");
          return (
            <span key={id} className="type-body flex items-center gap-2">
              <ResourcePartnerAvatar
                name={name}
                avatar={detail.partnerAvatars?.[id]}
                size="xs"
              />
              {name}
            </span>
          );
        })}
      </div>
    </section>
  );
}
