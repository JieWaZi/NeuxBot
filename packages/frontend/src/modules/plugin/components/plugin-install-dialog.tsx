import {
  hashKey,
  isCancelledError,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  useState,
  useCallback,
  useEffect,
  useEffectEvent,
  useRef,
} from "react";
import { Plus } from "lucide-react";
import type {
  GetPluginAuthorizationResponse,
  StartPluginAuthorizationResponse,
} from "@/shared/api/generated/types.gen";
import { useTranslation } from "react-i18next";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { StatusNotice } from "@/shared/ui/status-notice";
import { Button } from "@/shared/ui/button";
import { QueryState } from "@/shared/ui/query-state";
import { useQueryEvents } from "@/shared/api/use-query-events";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import {
  getPluginOptions,
  prepareCatalogPluginMutation,
  installPluginMutation,
  startPluginAuthorizationMutation,
  cancelPluginAuthorizationMutation,
  getPluginAuthorizationOptions,
  invalidatePluginQueries,
} from "../api/plugin-queries";
import {
  pluginDisplayName,
  pluginSummary,
  pluginPreferredAuthAction,
  pluginHasAuthorization,
  type PluginDetail,
  type PluginCatalogEntry,
} from "../model/plugin-view";
import { PartnerPicker } from "./partner-picker";
import { PluginIcon } from "./plugin-icon";
import { PluginAuthPanel } from "./plugin-auth-panel";
import { PluginConfigurationEditor } from "./plugin-configuration-editor";

type Props = {
  catalog?: PluginCatalogEntry;
  detail?: PluginDetail;
  wakerId?: string;
  onClose: () => void;
  authorizeOnly?: boolean;
};
export function PluginInstallDialog({
  catalog,
  detail: initial,
  wakerId = "",
  onClose,
  authorizeOnly = false,
}: Props) {
  const { t } = useTranslation("plugin");
  const client = useQueryClient();
  const [prepared, setPrepared] = useState(initial);
  const [selected, setSelected] = useState<string[]>(
    wakerId
      ? [wakerId]
      : authorizeOnly
        ? (initial?.installations ?? []).flatMap((item) =>
            item.wakerId ? [item.wakerId] : [],
          )
        : [],
  );
  const [phase, setPhase] = useState<
    "partners" | "authorization" | "configuration"
  >("partners");
  const [partnerNames, setPartnerNames] = useState<Record<string, string>>({});
  const [resumeError, setResumeError] = useState<unknown>();
  const intent = useRef<string[] | null>(null);
  const locked = useRef(false);
  const changedWhileLocked = useRef(false);
  const alive = useRef(true);
  const attempt = useRef<{
    pluginId: string;
    versionId?: string;
    session: StartPluginAuthorizationResponse;
  } | null>(null);
  const cancelled = useRef(new Map<string, Promise<void>>());
  const onUnmount = useEffectEvent(() => abandonAuthorization());
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      intent.current = null;
      onUnmount();
    };
  }, []);
  const rememberNames = useCallback(
    (next: Record<string, string>) =>
      setPartnerNames((previous) => ({ ...previous, ...next })),
    [],
  );
  const pluginId = prepared?.plugin?.id ?? "";
  const versionId = prepared?.resolvedVersionId ?? prepared?.versions?.[0]?.id;
  const options = getPluginOptions(
    withLocalErrorSurface({
      path: { pluginId },
      query: { wakerId, versionId },
    }),
  );
  const query = useQuery({ ...options, enabled: Boolean(pluginId) });
  useQueryEvents({
    topic: "plugins",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    enabled: Boolean(pluginId),
  });
  const detail = query.data ?? prepared;
  const version =
    detail?.versions?.find((item) => item.id === detail.resolvedVersionId) ??
    detail?.versions?.[0];
  const pkg = version?.package ?? catalog?.package;
  const prepare = useMutation({
    ...withLocalErrorMutation(prepareCatalogPluginMutation()),
  });
  const authorization = useMutation({
    ...withLocalErrorMutation(startPluginAuthorizationMutation()),
  });
  const cancellation = useMutation(
    withLocalErrorMutation(cancelPluginAuthorizationMutation()),
  );
  async function cancelAttempt() {
    const current = attempt.current;
    if (!current?.session.id || current.session.state !== "waiting") return;
    const options = getPluginAuthorizationOptions(
      withLocalErrorSurface({
        path: { pluginId: current.pluginId },
        query: { wakerId, versionId: current.versionId, verify: true },
      }),
    );
    const latest = client.getQueryData<GetPluginAuthorizationResponse>(
      options.queryKey,
    )?.session;
    // 只取消本弹窗发起且仍等待的会话；完成登录和共享账号绝不调用登出。
    if (latest?.id === current.session.id && latest.state !== "waiting") return;
    const key = `${current.pluginId}:${current.session.id}`;
    const previous = cancelled.current.get(key);
    if (previous) return previous;
    const request = cancellation
      .mutateAsync({
        path: { pluginId: current.pluginId, sessionId: current.session.id },
        query: { wakerId, versionId: current.versionId },
      })
      .then(() => undefined)
      .catch((error: unknown) => {
        cancelled.current.delete(key);
        throw error;
      });
    cancelled.current.set(key, request);
    return request;
  }
  function abandonAuthorization() {
    alive.current = false;
    intent.current = null;
    // 关闭立即生效；取消请求独立完成，晚到的 start 回执也走同一清理入口。
    void cancelAttempt().catch(() => undefined);
  }
  function close() {
    abandonAuthorization();
    onClose();
  }
  async function startAuthorization(
    current: PluginDetail,
    additionalScopes: string[] = [],
  ) {
    const id = current.plugin?.id ?? "";
    const selectedVersion =
      current.resolvedVersionId ?? current.versions?.[0]?.id;
    const session = await authorization.mutateAsync({
      path: { pluginId: id },
      query: { wakerId, versionId: selectedVersion },
      body: { additionalScopes },
    });
    attempt.current = { pluginId: id, versionId: selectedVersion, session };
    if (!alive.current) await cancelAttempt();
  }
  async function restartAuthorization(additionalScopes: string[]) {
    await cancelAttempt();
    if (!alive.current || !detail) return;
    await startAuthorization(detail, additionalScopes);
    if (alive.current) await invalidatePluginQueries(client);
  }
  const install = useMutation({
    ...withLocalErrorMutation(installPluginMutation()),
    onSuccess: async (value) => {
      intent.current = null;
      attempt.current = null;
      if (alive.current)
        setSelected(
          (value.results ?? [])
            .filter((item) => !item.installed)
            .map((item) => item.wakerId ?? "")
            .filter(Boolean),
        );
      await invalidatePluginQueries(client);
      if (
        alive.current &&
        value.results?.length &&
        value.results.every((item) => item.installed)
      )
        onClose();
    },
  });
  async function installReady(current: PluginDetail) {
    if (!alive.current || !intent.current) return;
    if (authorizeOnly) {
      if (current.account?.available) {
        intent.current = null;
        attempt.current = null;
        await invalidatePluginQueries(client);
        if (alive.current) onClose();
      }
      return;
    }
    if (!current.ready) return;
    const targets = intent.current.filter(
      (id) => !current.installations?.some((item) => item.wakerId === id),
    );
    if (!targets.length) {
      intent.current = null;
      return;
    }
    await install.mutateAsync({
      path: { pluginId: current.plugin?.id ?? "" },
      body: {
        versionId: current.resolvedVersionId ?? current.versions?.[0]?.id ?? "",
        wakerIds: targets,
        expectedConfigurationRevision: current.configurationRevision ?? 0,
      },
    });
  }
  async function resume() {
    if (!intent.current || !pluginId || !alive.current) return;
    try {
      await client.fetchQuery({ ...options, staleTime: 0 });
      await continueInstallation();
    } catch (error) {
      if (alive.current && !isCancelledError(error)) setResumeError(error);
    }
  }
  async function continueInstallation() {
    if (!intent.current || !pluginId || !alive.current) return;
    if (locked.current) {
      changedWhileLocked.current = true;
      return;
    }
    const current = client.getQueryData<PluginDetail>(options.queryKey);
    if (!current) return;
    locked.current = true;
    try {
      await installReady(current);
      if (alive.current) setResumeError(undefined);
    } catch (error) {
      if (alive.current && !isCancelledError(error)) setResumeError(error);
    } finally {
      locked.current = false;
      if (changedWhileLocked.current && intent.current && alive.current) {
        changedWhileLocked.current = false;
        void continueInstallation();
      }
    }
  }
  const onPluginSnapshot = useEffectEvent(() => {
    void continueInstallation();
  });
  const queryHash = hashKey(options.queryKey);
  useEffect(() => {
    // 领域事件只由 useQueryEvents 刷新一次；读取成功后继续原安装意图，避免并行重读互相取消。
    return client.getQueryCache().subscribe((event) => {
      if (
        event.type === "updated" &&
        event.action.type === "success" &&
        event.query.queryHash === queryHash
      )
        onPluginSnapshot();
    });
  }, [client, queryHash]);
  async function submit() {
    if (locked.current) return;
    locked.current = true;
    intent.current = selected;
    setResumeError(undefined);
    try {
      let current = detail;
      if (!current && catalog?.key) {
        current = await prepare.mutateAsync({
          path: { key: catalog.key },
          body: { ownerWakerId: "", trustConfirmed: true },
        });
        if (!alive.current) return;
        setPrepared(current);
      }
      if (!current) return;
      if (authorizeOnly && current.account?.available) {
        setPhase("authorization");
        await installReady(current);
      } else if (current.ready && !authorizeOnly) await installReady(current);
      else if (
        pluginHasAuthorization(
          current.versions?.find(
            (item) => item.id === current.resolvedVersionId,
          )?.package ?? current.versions?.[0]?.package,
        )
      ) {
        setPhase("authorization");
        await startAuthorization(current);
        if (!alive.current) return;
        const latest = await client.fetchQuery({
          ...getPluginOptions(
            withLocalErrorSurface({
              path: { pluginId: current.plugin?.id ?? "" },
              query: {
                wakerId,
                versionId:
                  current.resolvedVersionId ?? current.versions?.[0]?.id,
              },
            }),
          ),
          staleTime: 0,
        });
        await installReady(latest);
        await invalidatePluginQueries(client);
      } else setPhase("configuration");
    } catch (error) {
      if (alive.current && !isCancelledError(error)) setResumeError(error);
    } finally {
      locked.current = false;
      if (changedWhileLocked.current && intent.current && alive.current) {
        changedWhileLocked.current = false;
        void continueInstallation();
      }
    }
  }
  const results = install.data?.results;
  const installed = (detail?.installations ?? []).map(
    (item) => item.wakerId ?? "",
  );
  const pending =
    prepare.isPending ||
    install.isPending ||
    authorization.isPending ||
    cancellation.isPending;
  const complete = authorizeOnly
    ? phase === "authorization" && Boolean(detail?.account?.available)
    : Boolean(results?.length && results.every((item) => item.installed));
  const error =
    resumeError ??
    prepare.error ??
    authorization.error ??
    cancellation.error ??
    install.error;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent
        size="md"
        headingLayout="centered"
        title={t(
          authorizeOnly || (phase === "authorization" && !complete)
            ? "authorizeNamed"
            : "installNamed",
          { name: pluginDisplayName(pkg, detail?.plugin?.name) },
        )}
        description={pluginSummary(pkg)}
        visual={
          <div className="flex items-center gap-5 pb-2">
            <span className="bg-surface-subtle grid size-14 place-items-center rounded-lg">
              <img
                src="/ally-logo.svg"
                alt="NeuxBot"
                className="size-10 dark:hidden"
              />
              <img
                src="/ally-logo-dark.svg"
                alt="NeuxBot"
                className="hidden size-10 dark:block"
              />
            </span>
            <Plus className="size-icon-lg text-content-caption" />
            <PluginIcon pkg={pkg} hero />
          </div>
        }
      >
        <div className="min-h-0 space-y-4 overflow-y-auto px-6 pt-3 pb-6">
          {complete ? (
            <>
              <StatusNotice tone="success">
                {t(authorizeOnly ? "authComplete" : "succeeded")}
              </StatusNotice>
            </>
          ) : (
            <>
              {phase === "partners" || results ? (
                <div className="flex flex-wrap items-start gap-3">
                  <div className="min-w-0 flex-1">
                    <PartnerPicker
                      variant="select"
                      onNames={rememberNames}
                      selected={selected}
                      onChange={setSelected}
                      installed={installed}
                      onlyPartner={wakerId || undefined}
                      disabled={pending || authorizeOnly}
                    />
                  </div>
                  <Button
                    disabled={
                      pending ||
                      query.isError ||
                      query.isFetching ||
                      (authorizeOnly
                        ? selected.length === 0
                        : !selected.some((id) => !installed.includes(id)))
                    }
                    onClick={() => void submit()}
                  >
                    {pending
                      ? t("busy")
                      : authorizeOnly
                        ? t("authorize")
                        : results
                          ? t("retry")
                          : detail?.ready
                            ? t("install")
                            : t("continue")}
                  </Button>
                </div>
              ) : null}
              {query.isError && (
                <QueryState
                  error={query.error}
                  onRetry={() => void query.refetch()}
                />
              )}
              {phase === "authorization" && pluginId && !results && (
                <PluginAuthPanel
                  preferredAction={pluginPreferredAuthAction(pkg)}
                  installation
                  starting={authorization.isPending || cancellation.isPending}
                  pluginId={pluginId}
                  wakerId={wakerId}
                  versionId={version?.id}
                  onRestart={restartAuthorization}
                  onReady={() => {
                    attempt.current = null;
                    void resume();
                  }}
                />
              )}
              {phase === "configuration" && detail && (
                <PluginConfigurationEditor
                  detail={detail}
                  wakerId={wakerId}
                  onSaved={() => void resume()}
                />
              )}
            </>
          )}
          {results && !complete && (
            <section aria-live="polite" className="space-y-2">
              {results.map((item) => (
                <p key={item.wakerId} className="type-body">
                  {partnerNames[item.wakerId ?? ""] ||
                    detail?.partnerNames?.[item.wakerId ?? ""] ||
                    t("partners")}{" "}
                  · {t(item.installed ? "succeeded" : "failed")}
                </p>
              ))}
              <p className="type-caption text-content-secondary">
                {t("partialFailure")}
              </p>
            </section>
          )}
          {error && (
            <p role="alert" className="type-body text-destructive">
              {localApiErrorMessage(error, t("failed"))}
            </p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
