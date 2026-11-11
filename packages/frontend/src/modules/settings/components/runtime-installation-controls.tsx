import {
  replaceEqualDeep,
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { ExternalLink, LoaderCircle } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  cancelRuntimeInstallationMutation,
  checkRuntimeUpdatesMutation,
  getRuntimeInstallationOptions,
  installRuntimeMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { registerCoreConnectionRecovery } from "@/shared/api/global-events";
import { applicationErrorMessage } from "@/shared/api/error-presentation";
import {
  withLocalErrorMutation,
  withLocalErrorSurface,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { Button } from "@/shared/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { openExternalURL } from "@/shared/platform";
import { Skeleton } from "@/shared/ui/skeleton";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { cliInstallation } from "@/shared/lib/cli-installation";

export type RuntimeInstallationControlsProps = {
  runtimeId: string;
  baseRuntimeId?: string;
  installed: boolean;
  disabled?: boolean;
  version?: string;
  onRuntimeChanged?: () => Promise<void>;
};

/** The cache owns installation facts; an older read cannot undo a newer revision. */
function shareSnapshot(previous: unknown, next: unknown): unknown {
  if (
    previous &&
    next &&
    typeof previous === "object" &&
    typeof next === "object" &&
    "revision" in previous &&
    "revision" in next &&
    typeof previous.revision === "number" &&
    typeof next.revision === "number" &&
    next.revision < previous.revision
  )
    return previous;
  return replaceEqualDeep(previous, next);
}

/** All observers use the generated key and the same revision-aware cache merge. */
export function runtimeInstallationOptions(runtimeId: string) {
  return queryOptions({
    ...getRuntimeInstallationOptions(
      withLocalErrorSurface({ path: { runtimeId } }),
    ),
    staleTime: Infinity,
    retry: false,
    refetchOnMount: "always",
    structuralSharing: shareSnapshot,
  });
}

const phaseKeys = {
  resolving: "agents.installation.preparing",
  downloading: "agents.installation.downloading",
  extracting: "agents.installation.extracting",
  dependencies: "agents.installation.dependencies",
  verifying: "agents.installation.verifying",
  publishing: "agents.installation.publishing",
  detecting: "agents.installation.detecting",
  uninstalling: "agents.removal.pending",
} as const;

function phaseKey(phase?: string) {
  return phase && Object.hasOwn(phaseKeys, phase)
    ? phaseKeys[phase as keyof typeof phaseKeys]
    : "agents.installation.pending";
}

/** Version-row actions backed by the managed installer and the shared event stream. */
export function RuntimeInstallationControls({
  runtimeId,
  baseRuntimeId,
  installed,
  disabled = false,
  onRuntimeChanged,
}: RuntimeInstallationControlsProps) {
  const { t } = useTranslation("aiAccounts");
  const active = useRouteSceneActive();
  const client = useQueryClient();
  const options = useMemo(
    () => runtimeInstallationOptions(runtimeId),
    [runtimeId],
  );
  const delivering = useRef("");
  const [officialOpenFailed, setOfficialOpenFailed] = useState(false);
  const official = cliInstallation(baseRuntimeId || runtimeId, "unknown");
  const snapshot = useQuery({
    ...options,
    enabled: active && Boolean(runtimeId),
  });
  useQueryEvents({
    topic: "runtimes",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    active,
    eventTypes: ["runtime.installation.changed"],
    eventFilter: ({ resource }) =>
      resource.kind === "global" ||
      (resource.kind === "runtime" &&
        (resource.id === runtimeId || resource.id === baseRuntimeId)),
  });

  // Revisions belong to the Core process. Reset this read on its existing recovery
  // notification so a restarted Core can publish a fresh revision zero.
  useEffect(() => {
    const queryKey = options.queryKey;
    return registerCoreConnectionRecovery(() => {
      delivering.current = "";
      void client.resetQueries({ queryKey, exact: true });
    });
  }, [client, options.queryKey]);

  const reconcile = () =>
    client.invalidateQueries({ queryKey: options.queryKey, exact: true });
  const install = useMutation({
    ...withLocalErrorMutation(installRuntimeMutation()),
    onSuccess: reconcile,
  });
  const cancel = useMutation({
    ...withLocalErrorMutation(cancelRuntimeInstallationMutation()),
    onSuccess: reconcile,
  });
  const check = useMutation({
    ...withLocalErrorMutation(checkRuntimeUpdatesMutation()),
    onSuccess: reconcile,
  });

  const data = snapshot.data;
  useEffect(() => {
    if (
      !active ||
      !data ||
      !data.revision ||
      !(
        ["installed", "failed", "cancelled"].includes(data.state ?? "") ||
        (data.state === "idle" && data.operation === "uninstall")
      )
    )
      return;
    const terminal = `${runtimeId}:${data.revision}:${data.state}`;
    if (delivering.current === terminal) return;
    delivering.current = terminal;
    // Notify the owner to reconcile its runtime rows; no second installation state.
    void onRuntimeChanged?.().catch(() => undefined);
  }, [active, data, onRuntimeChanged, runtimeId]);

  const pending =
    disabled || install.isPending || cancel.isPending || check.isPending;
  const installing = data?.state === "installing";
  const hasInstallation = data?.installed ?? installed;
  const progress =
    installing && data.phase === "downloading" && (data.totalBytes ?? 0) > 0
      ? Math.max(0, Math.min(100, data.progress ?? 0))
      : undefined;
  const checking = check.isPending || data?.checkState === "checking";
  const repairable =
    data?.canUpdate === true && data.maintenanceReason === "repairable";
  const updatable = data?.updateAvailable === true && data.canUpdate === true;
  const removing = data?.operation === "uninstall";
  const officialUrl = data?.officialUrl || official?.url;
  const actionError = install.isError || cancel.isError;
  const reasons = [
    "tls",
    "proxy",
    "network",
    "http_status",
    "checksum",
    "archive",
    "prerequisite",
    "dependencies",
    "version",
    "manifest",
    "source",
    "filesystem",
  ];
  const failureMessage = removing
    ? t("agents.removal.failed")
    : repairable
      ? t("agents.installation.repairRequired")
      : data?.errorCode === "OperationTimeout" && data.phase === "verifying"
        ? t("agents.installation.errors.verifyTimeout")
        : data?.errorReason && reasons.includes(data.errorReason)
          ? t(`agents.installation.errors.${data.errorReason}`, {
              status: data.errorStatus,
            })
          : applicationErrorMessage({
              code: data?.errorCode,
              fallback: t("agents.installation.failed"),
            });
  const start = () => {
    cancel.reset();
    check.reset();
    install.mutate({ path: { runtimeId } });
  };
  const stop = () => {
    install.reset();
    cancel.mutate({ path: { runtimeId } });
  };

  return (
    <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
      {snapshot.isPending && (
        <div
          role="status"
          aria-busy="true"
          aria-label={t("agents.installation.loading")}
        >
          <Skeleton className="h-button-md rounded-control w-14" />
        </div>
      )}
      {snapshot.isError && (
        <>
          <span
            role="alert"
            className="text-destructive type-caption break-words"
          >
            {t("agents.installation.loadFailed")}
          </span>
          <Button
            size="settings"
            variant="settings-outline"
            disabled={snapshot.isFetching}
            onClick={() => void snapshot.refetch()}
          >
            {t("agents.installation.retry")}
          </Button>
        </>
      )}
      {data && !hasInstallation && !data.canInstall && !data.supported && (
        <span className="text-resource-muted type-caption break-words">
          {t("agents.installation.unsupported")}
        </span>
      )}
      {data && installing && (
        <>
          <span role="status" className="text-resource-muted type-caption">
            {cancel.isPending
              ? t("agents.installation.canceling")
              : t(phaseKey(data.phase))}
            {progress !== undefined && ` ${progress}%`}
          </span>
          <span
            role="progressbar"
            aria-label={t("agents.installation.progress")}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={progress}
            className="bg-resource-hover h-1 w-20 overflow-hidden rounded-full"
          >
            <span
              className={`bg-resource-primary block h-full rounded-full ${progress === undefined ? "w-1/3 animate-pulse" : ""}`}
              style={
                progress === undefined ? undefined : { width: `${progress}%` }
              }
            />
          </span>
          {data.cancelable && (
            <Button
              size="settings"
              variant="settings-outline"
              disabled={pending}
              onClick={stop}
            >
              {t(
                removing
                  ? "agents.removal.cancel"
                  : "agents.installation.cancel",
              )}
            </Button>
          )}
        </>
      )}
      {data &&
        !installing &&
        (hasInstallation || data.canInstall || repairable) && (
          <>
            {data.state === "failed" && (
              <span
                role="alert"
                className="text-destructive type-caption break-words"
              >
                {failureMessage}
              </span>
            )}
            {data.state === "cancelled" && (
              <span role="status" className="text-resource-muted type-caption">
                {t(
                  removing
                    ? "agents.removal.cancelled"
                    : "agents.installation.cancelled",
                )}
              </span>
            )}
            <Button
              size="settings"
              variant={hasInstallation ? "settings-outline" : "primary"}
              disabled={pending || checking}
              onClick={() => {
                if (hasInstallation && !updatable && !repairable) {
                  install.reset();
                  cancel.reset();
                  check.mutate({ path: { runtimeId } });
                } else start();
              }}
            >
              {(install.isPending || checking) && (
                <LoaderCircle className="animate-spin" />
              )}
              {t(
                install.isPending
                  ? "agents.installation.preparing"
                  : checking
                    ? "agents.installation.checking"
                    : repairable
                      ? "agents.installation.reinstall"
                      : data.state === "failed" && !hasInstallation
                        ? "agents.installation.retry"
                        : hasInstallation
                          ? updatable
                            ? "agents.installation.update"
                            : "agents.installation.check"
                          : "agents.installation.install",
              )}
            </Button>
          </>
        )}
      {hasInstallation &&
      !repairable &&
      !installing &&
      !checking &&
      data?.checkState === "checked" &&
      data.updateAvailable === false ? (
        <span role="status" className="text-resource-muted type-caption">
          {t("agents.installation.current")}
        </span>
      ) : null}
      {(check.isError || data?.checkState === "failed") &&
      !checking &&
      !installing ? (
        <span role="alert" className="text-destructive type-caption">
          {t("agents.installation.checkFailed")}
        </span>
      ) : null}
      {officialUrl &&
      !installing &&
      (!hasInstallation ||
        data?.state === "failed" ||
        snapshot.isError ||
        data?.supported === false ||
        (hasInstallation && data?.canUpdate === false)) ? (
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                size="icon"
                variant="plain"
                aria-label={t("agents.installation.officialDownload")}
                onClick={() => {
                  setOfficialOpenFailed(false);
                  void openExternalURL(officialUrl).catch(() =>
                    setOfficialOpenFailed(true),
                  );
                }}
              >
                <ExternalLink aria-hidden="true" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              {t("agents.installation.officialDownload")}
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      ) : null}
      {officialOpenFailed ? (
        <span role="alert" className="text-destructive type-caption">
          {t("agents.installation.officialOpenFailed")}
        </span>
      ) : null}
      {actionError && (
        <span
          role="alert"
          className="text-destructive type-caption break-words"
        >
          {localApiErrorMessage(
            install.error ?? cancel.error,
            t(
              removing
                ? "agents.removal.failed"
                : "agents.installation.actionFailed",
            ),
          )}
        </span>
      )}
    </div>
  );
}
