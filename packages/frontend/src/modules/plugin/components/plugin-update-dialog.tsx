import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useEffectEvent, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError } from "@/shared/api/errors";
import {
  applicationErrorKey,
  applicationErrorMessage,
} from "@/shared/api/error-presentation";
import type {
  GetPluginAuthorizationResponse,
  StartPluginAuthorizationResponse,
  CheckPluginAccountData,
  GetPluginConfigurationResponse,
  UpdatePluginInstallationsData,
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
  updatePluginInstallationsMutation,
  getPluginConfigurationOptions,
} from "../api/plugin-queries";
import {
  pluginDisplayName,
  pluginHasAuthorization,
  pluginHasUpdate,
  pluginLocale,
  pluginPreferredAuthAction,
  pluginSummary,
  type PluginDetail,
} from "../model/plugin-view";
import { pluginAccountContext } from "../model/plugin-account-context";
import { usePluginConfiguration } from "../api/use-plugin-configuration";
import { PluginAuthPanel } from "./plugin-auth-panel";
import {
  PluginConfigurationFields,
  usePluginConfigurationDraft,
} from "./plugin-configuration-editor";

/** 准备固定内容与切换安装分开；只有最终确认才原子更新全部伙伴。 */
export function PluginUpdateDialog({
  detail: initial,
  wakerId = "",
  rollback = false,
  onClose,
}: {
  detail: PluginDetail;
  wakerId?: string;
  rollback?: boolean;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation("plugin");
  const client = useQueryClient();
  const dialogId = useId();
  const locale = pluginLocale(i18n.resolvedLanguage ?? i18n.language);
  const [snapshot, setSnapshot] = useState(initial);
  const [prepared, setPrepared] = useState<PluginDetail>();
  const [reloadError, setReloadError] = useState<unknown>();
  const alive = useRef(true);
  const committing = useRef(false);
  const ownedSession = useRef<{
    session: StartPluginAuthorizationResponse;
    versionId: string;
    context: CheckPluginAccountData["body"] & { ownershipNonce: string };
  } | null>(null);
  const cancelled = useRef(new Set<string>());
  const pluginId = snapshot.plugin?.id ?? "";
  const update =
    pluginHasUpdate(snapshot, wakerId) &&
    (!wakerId || snapshot.packageStatus !== "unsupported")
      ? snapshot.update
      : undefined;
  const catalogKey = update?.catalogKey;
  const archiveSHA256 = update?.archiveSHA256;
  const rollbackVersionId = snapshot.rollback?.available
    ? snapshot.rollback.versionId
    : undefined;
  const canPrepare = Boolean(
    snapshot.installations?.length &&
    (rollback ? rollbackVersionId : catalogKey && archiveSHA256),
  );
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
        query: { wakerId, versionId: owned.versionId },
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
        body: owned.context,
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
  async function prepareTarget(value: PluginDetail) {
    if (!alive.current) return;
    await client.fetchQuery({
      ...getPluginConfigurationOptions(
        withLocalErrorSurface({
          path: { pluginId },
          query: { wakerId, versionId: value.resolvedVersionId ?? "" },
        }),
      ),
      staleTime: 0,
    });
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
  }
  const prepare = useMutation({
    ...withLocalErrorMutation(prepareCatalogPluginMutation()),
    onSuccess: prepareTarget,
  });
  function readVersionChangeDetail(versionId?: string) {
    const options = getPluginOptions(
      withLocalErrorSurface({
        path: { pluginId },
        query: { wakerId, versionId, locale },
      }),
    );
    return client.fetchQuery({
      ...options,
      queryKey: [
        { ...options.queryKey[0], _id: `pluginVersionChange:${dialogId}` },
      ],
      staleTime: 0,
      gcTime: 0,
    });
  }
  const prepareRollback = useMutation({
    mutationFn: async (versionId: string) => {
      const detail = await readVersionChangeDetail(versionId);
      if (
        !detail.rollback?.available ||
        detail.rollback.versionId !== versionId ||
        detail.resolvedVersionId !== versionId
      )
        throw new ApiError(t("rollbackUnavailable"));
      return detail;
    },
    onSuccess: prepareTarget,
  });
  const preparation = rollback ? prepareRollback : prepare;
  const reload = useMutation({
    mutationFn: async () => {
      await cancelOwnedSession();
      return readVersionChangeDetail();
    },
    onSuccess: (value) => {
      if (!alive.current) return;
      setSnapshot(value);
      setPrepared(undefined);
      setReloadError(undefined);
      prepare.reset();
      prepareRollback.reset();
      save.reset();
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
  const save = useMutation({
    ...withLocalErrorMutation(updatePluginInstallationsMutation()),
    onSuccess: async () => {
      await invalidatePluginQueries(client);
      close();
    },
    onError: async (error, request) => {
      const key =
        error instanceof ApiError ? applicationErrorKey(error.code) : undefined;
      if (
        key === "PluginConfigurationConflict" ||
        key === "PluginAccountConflict"
      ) {
        const configuration = getPluginConfigurationOptions(
          withLocalErrorSurface({
            path: { pluginId },
            query: {
              wakerId,
              versionId: request.body.switches?.[0]?.versionId ?? "",
            },
          }),
        );
        await client.refetchQueries({
          queryKey: configuration.queryKey,
          exact: true,
        });
      }
    },
    onSettled: () => {
      committing.current = false;
    },
  });
  function submit(body: UpdatePluginInstallationsData["body"]) {
    if (committing.current) return;
    committing.current = true;
    save.mutate({ path: { pluginId }, query: { wakerId }, body });
  }
  function dismiss() {
    if (!committing.current) close();
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) dismiss();
      }}
    >
      <DialogContent
        title={t(rollback ? "rollbackNamed" : "updateNamed", {
          name: pluginDisplayName(pkg, snapshot.plugin?.name),
        })}
        description={
          canPrepare ? t(rollback ? "rollbackHelp" : "updateHelp") : undefined
        }
        closeDisabled={save.isPending}
        size="lg"
      >
        {current && versionId ? (
          <PreparedPluginUpdate
            key={versionId}
            detail={current}
            snapshot={snapshot}
            rollback={rollback}
            versionId={versionId}
            wakerId={wakerId}
            queryError={target.error}
            fetching={target.isFetching}
            onRetry={() => void target.refetch()}
            onAuthorizationStarted={(session, context) => {
              ownedSession.current = { session, versionId, context };
              if (!alive.current) void cancelOwnedSession();
            }}
            onAuthorizationReady={() => {
              ownedSession.current = null;
              void target.refetch();
            }}
            onReload={() => reload.mutate()}
            reloading={reload.isPending}
            onClose={dismiss}
            saving={save.isPending}
            saveError={save.error}
            lastSubmitted={save.variables?.body}
            onSave={submit}
          />
        ) : canPrepare ? (
          <>
            <div className="min-h-0 space-y-4 overflow-y-auto px-6 pb-6">
              <UpdatePartners detail={snapshot} rollback={rollback} />
              <p className="type-body text-content-secondary">
                {t(rollback ? "prepareRollbackHelp" : "prepareUpdateHelp")}
              </p>
              {preparation.error && (
                <div className="space-y-3">
                  <p role="alert" className="type-body text-destructive">
                    {localApiErrorMessage(preparation.error, t("failed"))}
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
                disabled={preparation.isPending || reload.isPending}
                onClick={() => {
                  if (rollback) {
                    if (rollbackVersionId)
                      prepareRollback.mutate(rollbackVersionId);
                  } else
                    prepare.mutate({
                      path: { key: catalogKey ?? "" },
                      body: {
                        expectedArchiveSHA256: archiveSHA256,
                        trustConfirmed: true,
                      },
                    });
                }}
              >
                {t(
                  preparation.isPending
                    ? "busy"
                    : rollback
                      ? "prepareRollback"
                      : "prepareUpdate",
                )}
              </Button>
            </DialogFooter>
          </>
        ) : (
          <p className="type-body text-content-secondary px-6 pb-6">
            {t(rollback ? "rollbackUnavailable" : "updateUnavailable")}
          </p>
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

type PreparedUpdateProps = {
  detail: PluginDetail;
  snapshot: PluginDetail;
  rollback: boolean;
  versionId: string;
  wakerId: string;
  queryError: unknown;
  fetching: boolean;
  onRetry: () => void;
  onAuthorizationStarted: (
    session: StartPluginAuthorizationResponse,
    context: CheckPluginAccountData["body"] & { ownershipNonce: string },
  ) => void;
  onAuthorizationReady: () => void;
  onReload: () => void;
  reloading: boolean;
  onClose: () => void;
  saving: boolean;
  saveError: unknown;
  lastSubmitted?: UpdatePluginInstallationsData["body"];
  onSave: (body: UpdatePluginInstallationsData["body"]) => void;
};
function PreparedPluginUpdate(props: PreparedUpdateProps) {
  const configuration = usePluginConfiguration(
    props.detail.plugin?.id ?? "",
    props.versionId,
    props.wakerId,
  );
  return configuration.data ? (
    <PreparedPluginUpdateForm
      {...props}
      configuration={configuration.data}
      fetching={props.fetching || configuration.isFetching}
      queryError={props.queryError ?? configuration.error}
      onRetry={() => {
        props.onRetry();
        void configuration.refetch();
      }}
    />
  ) : (
    <div className="px-6 pb-6">
      <QueryState
        loadingLayout="content"
        pending={configuration.isPending}
        error={configuration.error}
        onRetry={() => void configuration.refetch()}
      />
    </div>
  );
}
function PreparedPluginUpdateForm({
  detail,
  snapshot,
  rollback,
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
  saving,
  saveError,
  lastSubmitted,
  onSave,
  configuration,
}: PreparedUpdateProps & { configuration: GetPluginConfigurationResponse }) {
  const { t } = useTranslation("plugin");
  const draft = usePluginConfigurationDraft(configuration);
  const pkg = detail.versions?.find(
    (version) => version.id === versionId,
  )?.package;
  const hasAuth = pluginHasAuthorization(pkg);
  const [authenticated, setAuthenticated] = useState(false);
  const pending = saving || reloading;
  const accountContext = pluginAccountContext(configuration);
  const errorKey =
    saveError instanceof ApiError
      ? applicationErrorKey(saveError.code)
      : undefined;
  const staleInstallation = errorKey === "PluginVersionConflict";
  const staleConfiguration =
    draft.body.expectedRevision !== configuration.configurationRevision;
  const reconcileAccount =
    errorKey === "PluginAccountConflict" &&
    lastSubmitted?.expectedAccountGeneration ===
      accountContext?.expectedAccountGeneration;
  const needsReload =
    staleInstallation || staleConfiguration || reconcileAccount;
  const unchangedRejectedConfiguration =
    errorKey === "PluginConfigurationConflict" &&
    JSON.stringify(draft.body) ===
      JSON.stringify({
        expectedRevision: lastSubmitted?.expectedRevision,
        values: lastSubmitted?.values,
        secrets: lastSubmitted?.secrets,
      });
  const hasConfiguration = Boolean(
    configuration.fields?.length ||
    Object.keys(configuration.values ?? {}).length ||
    configuration.configuredSecrets?.length,
  );
  return (
    <>
      <div className="min-h-0 space-y-5 overflow-y-auto px-6 pb-6">
        <UpdatePartners detail={snapshot} rollback={rollback} />
        <p className="type-body text-content-secondary">{pluginSummary(pkg)}</p>
        <p className="type-body text-content-secondary">
          {t(rollback ? "rollbackReadyHelp" : "updateReadyHelp")}
        </p>
        {queryError ? (
          <QueryState error={queryError} onRetry={onRetry} />
        ) : null}
        <fieldset disabled={pending} className="min-w-0 space-y-5">
          {hasConfiguration && (
            <PluginConfigurationFields
              configuration={configuration}
              draft={draft}
            />
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
        {(!!saveError || staleConfiguration) && (
          <div className="space-y-2">
            <p role="alert" className="type-body text-destructive">
              {staleConfiguration
                ? applicationErrorMessage({
                    code: "PluginConfigurationConflict",
                  })
                : localApiErrorMessage(saveError, t("failed"))}
            </p>
            {needsReload && (
              <>
                <p className="type-body text-content-secondary">
                  {t(rollback ? "rollbackRetryHelp" : "updateRetryHelp")}
                </p>
                <Button
                  variant="secondary"
                  disabled={pending}
                  onClick={onReload}
                >
                  {t("reloadUpdate")}
                </Button>
              </>
            )}
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
            needsReload ||
            unchangedRejectedConfiguration ||
            (hasAuth && !authenticated) ||
            !accountContext
          }
          onClick={() =>
            onSave({
              ...draft.body,
              expectedAccountGeneration:
                accountContext?.expectedAccountGeneration,
              ...(rollback ? { rollback: true } : {}),
              switches: (snapshot.installations ?? []).map((relation) => ({
                wakerId: relation.wakerId ?? "",
                expectedVersionId: relation.versionId ?? "",
                versionId,
              })),
            })
          }
        >
          {t(pending ? "busy" : rollback ? "rollbackAll" : "update")}
        </Button>
      </DialogFooter>
    </>
  );
}

function UpdatePartners({
  detail,
  rollback,
}: {
  detail: PluginDetail;
  rollback: boolean;
}) {
  const { t } = useTranslation("plugin");
  const versions = new Map(
    detail.versions?.map(({ id, version }) => [id, version]),
  );
  const target = rollback ? detail.rollback?.version : detail.update?.version;
  return (
    <section className="space-y-3">
      <h3 className="type-label font-medium">
        {t(rollback ? "rollbackPartners" : "updatePartners", {
          count: detail.installations?.length ?? 0,
        })}
      </h3>
      <div className="flex flex-wrap gap-3">
        {(detail.installations ?? []).map((relation) => {
          const id = relation.wakerId ?? "";
          const name = detail.partnerNames?.[id] || t("unknown");
          const current = versions.get(relation.versionId);
          const version =
            target && (rollback || detail.update?.wakerIds?.includes(id))
              ? t("updateVersionChange", {
                  current: current || t("unknown"),
                  target,
                })
              : current;
          return (
            <span key={id} className="type-body flex items-center gap-2">
              <ResourcePartnerAvatar
                name={name}
                avatar={detail.partnerAvatars?.[id]}
                size="xs"
              />
              {name}
              {version && (
                <span className="text-content-secondary">{version}</span>
              )}
            </span>
          );
        })}
      </div>
    </section>
  );
}
