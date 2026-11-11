import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  authenticateRuntimeMutation,
  cancelRuntimeAuthenticationMutation,
  getRuntimeAuthenticationOptions,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { GithubComJieWaZiNeuxBotServerApisRuntimeOptionsRuntimeOption as RuntimeOption } from "@/shared/api/generated/types.gen";
import { getRuntimeAuthentication } from "@/shared/api/generated/sdk.gen";
import { applicationErrorMessage } from "@/shared/api/error-presentation";
import { registerCoreConnectionRecovery } from "@/shared/api/global-events";
import {
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import {
  refreshQueryFromEvent,
  useQueryEvents,
} from "@/shared/api/use-query-events";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Skeleton } from "@/shared/ui/skeleton";
import { openExternalURL } from "@/shared/platform";
import { cliInstallation } from "@/shared/lib/cli-installation";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import {
  LoginAuthorizationDetails,
  LoginStatusPanel,
} from "./login-status-panel";

/** 内置账号认证使用独立缓存，不读写受管账号的登录状态。 */
export function runtimeAuthenticationOptions(runtimeId: string) {
  return queryOptions({
    ...getRuntimeAuthenticationOptions(
      withLocalErrorSurface({ path: { runtimeId } }),
    ),
    staleTime: Infinity,
    retry: false,
    refetchOnMount: "always",
    structuralSharing: (previous: unknown, next: unknown) => {
      if (
        previous &&
        typeof previous === "object" &&
        next &&
        typeof next === "object" &&
        "revision" in previous &&
        "revision" in next &&
        typeof previous.revision === "number" &&
        typeof next.revision === "number" &&
        previous.revision > next.revision
      )
        return previous;
      return next;
    },
  });
}

export function RuntimeSystemAccountActions({
  runtime,
  disabled,
}: {
  runtime: RuntimeOption;
  disabled?: boolean;
}) {
  const { t } = useTranslation(["aiAccounts", "common"]);
  const active = useRouteSceneActive();
  const client = useQueryClient();
  const runtimeId = runtime.id ?? "";
  const options = useMemo(
    () => runtimeAuthenticationOptions(runtimeId),
    [runtimeId],
  );
  const query = useQuery({
    ...options,
    enabled: active && Boolean(runtime.installed && runtimeId),
  });
  useQueryEvents({
    topic: "runtimes",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    active,
    eventTypes: ["runtime.authentication.changed"],
    eventFilter: ({ resource }) =>
      resource.kind === "global" ||
      (resource.kind === "runtime" && resource.id === runtimeId),
  });
  const [open, setOpen] = useState(false);
  const [jobId, setJobId] = useState<string>();
  const [configurationOpenFailed, setConfigurationOpenFailed] = useState(false);
  const [authorizationOpenFailed, setAuthorizationOpenFailed] = useState(false);
  const closeRequested = useRef(false);
  const epoch = useRef(0);
  const reconcile = () =>
    client.invalidateQueries({ queryKey: options.queryKey, exact: true });
  const start = useMutation({
    ...withLocalErrorMutation(authenticateRuntimeMutation()),
    onMutate: () => ({ epoch: epoch.current }),
    onSuccess: (data, _variables, context) => {
      if (context?.epoch !== epoch.current) return;
      client.setQueryData(options.queryKey, data);
      setJobId(data.jobId);
      if (closeRequested.current && data.jobId && data.cancelable)
        cancel.mutate({ path: { runtimeId }, query: { jobId: data.jobId } });
    },
  });
  const cancel = useMutation({
    ...withLocalErrorMutation(cancelRuntimeAuthenticationMutation()),
    onMutate: () => ({ epoch: epoch.current }),
    onSuccess: (_data, _variables, context) => {
      if (context?.epoch === epoch.current) return reconcile();
    },
  });
  const retry = useMutation({
    mutationFn: () => {
      const requestEpoch = epoch.current;
      return refreshQueryFromEvent(
        client,
        options.queryKey,
        async () => {
          if (requestEpoch !== epoch.current) return;
          const { data } = await getRuntimeAuthentication(
            withLocalErrorSurface({
              path: { runtimeId },
              query: { refresh: true },
              throwOnError: true,
            }),
          );
          if (requestEpoch === epoch.current)
            client.setQueryData(options.queryKey, data);
        },
        1,
      );
    },
  });
  const resetStart = start.reset;
  const resetCancel = cancel.reset;
  const resetRetry = retry.reset;
  useEffect(
    () =>
      registerCoreConnectionRecovery(() => {
        epoch.current++;
        setJobId(undefined);
        setOpen(false);
        resetStart();
        resetCancel();
        resetRetry();
        void client.resetQueries({ queryKey: options.queryKey, exact: true });
      }),
    [client, options.queryKey, resetStart, resetCancel, resetRetry],
  );
  const data = query.data;
  const capabilityFailed = data?.state === "failed" && !data.jobId;
  const methods =
    data?.methods?.filter((method) => method.kind === "agent") ?? [];
  const authenticating = data?.state === "authenticating";
  const inProgress =
    Boolean(data?.jobId) && (data?.state === "checking" || authenticating);
  const pending = start.isPending || cancel.isPending;
  const reconnect = !runtime.defaultAccountId && runtime.auth === "ok";
  const configuration = cliInstallation(
    runtime.baseRuntimeId || runtimeId,
    "unknown",
  );
  const verifyConnection =
    reconnect && (runtime.baseRuntimeId || runtimeId) === "codex";
  const begin = (methodId?: string) => {
    cancel.reset();
    setJobId(undefined);
    setAuthorizationOpenFailed(false);
    start.mutate({ path: { runtimeId }, body: { methodId } });
  };
  const close = () => {
    closeRequested.current = true;
    setOpen(false);
    if (jobId && data?.cancelable && data.jobId === jobId)
      cancel.mutate({ path: { runtimeId }, query: { jobId } });
  };
  const error = start.error ?? cancel.error;
  const ownJob = Boolean(jobId && data?.jobId === jobId);
  const succeeded = !start.isPending && ownJob && data?.state === "succeeded";
  const failed = !start.isPending && ownJob && data?.state === "failed";
  const cancelled = ownJob && data?.state === "cancelled";
  const connecting = inProgress || start.isPending;
  const operationTitle = verifyConnection
    ? "loginStatus.verifyConnection"
    : reconnect
      ? "agents.authentication.reconnect"
      : "login";
  const resultTitle =
    data?.result === "existing_login_verified" ||
    data?.result === "connection_verified"
      ? t("loginStatus.connectionVerified")
      : data?.result === "authentication_completed"
        ? t("agents.authentication.complete")
        : t("loginStatus.connected");
  const resultDescription =
    data?.result === "existing_login_verified"
      ? t("loginStatus.existingAccount")
      : t("loginStatus.connectedDescription");
  const openAuthorization = () => {
    if (!ownJob || !data?.verificationUrl) return;
    setAuthorizationOpenFailed(false);
    void openExternalURL(data.verificationUrl).catch(() =>
      setAuthorizationOpenFailed(true),
    );
  };
  return (
    <>
      {query.isPending || (data?.state === "checking" && !data.jobId) ? (
        <div
          role="status"
          aria-busy="true"
          aria-label={t("agents.authentication.loading")}
        >
          <Skeleton className="h-button-md rounded-control w-16" />
        </div>
      ) : (data?.canAuthenticate && methods.length) || inProgress ? (
        <Button
          size="settings"
          variant="settings-outline"
          disabled={(disabled && !inProgress) || pending}
          onClick={() => {
            closeRequested.current = false;
            setOpen(true);
            if (inProgress) setJobId(data?.jobId);
            else if (methods.length === 1) begin(methods[0]?.id);
          }}
        >
          {t(inProgress ? "agents.authentication.continue" : operationTitle)}
        </Button>
      ) : !runtime.defaultAccountId &&
        runtime.auth === "missing" &&
        data?.configurationReason &&
        configuration ? (
        <>
          <span className="text-content-secondary type-ui">
            {t("agents.authentication.localConfiguration")}
          </span>
          <Button
            size="settings"
            variant="settings-outline"
            disabled={disabled}
            onClick={() => {
              setConfigurationOpenFailed(false);
              void openExternalURL(configuration.url).catch(() =>
                setConfigurationOpenFailed(true),
              );
            }}
          >
            {t("agents.authentication.officialConfiguration")}
          </Button>
        </>
      ) : null}
      {configurationOpenFailed ? (
        <span role="alert" className="text-destructive type-caption">
          {t("agents.installation.officialOpenFailed")}
        </span>
      ) : null}
      {query.isError || capabilityFailed ? (
        <span role="alert" className="text-destructive type-caption">
          {retry.error?.message ??
            query.error?.message ??
            applicationErrorMessage({
              code: data?.errorCode,
              fallback: t("agents.authentication.loadFailed"),
            })}
        </span>
      ) : null}
      {query.isError || capabilityFailed ? (
        <Button
          size="settings"
          variant="settings-outline"
          disabled={disabled || query.isFetching || retry.isPending}
          onClick={() => retry.mutate()}
        >
          {t("common:retry")}
        </Button>
      ) : null}
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!value) close();
        }}
      >
        <DialogContent title={t(operationTitle)} size="sm">
          <div className="flex min-h-0 flex-col gap-4 overflow-y-auto px-6 pb-6">
            <LoginStatusPanel
              state={
                succeeded
                  ? "success"
                  : connecting
                    ? "pending"
                    : failed || error
                      ? "error"
                      : "ready"
              }
              title={
                succeeded
                  ? resultTitle
                  : connecting
                    ? t(
                        verifyConnection
                          ? "loginStatus.verifyingConnection"
                          : authenticating
                            ? "loginStatus.connecting"
                            : "agents.authentication.preparing",
                      )
                    : failed || error
                      ? t("loginStatus.failed")
                      : cancelled
                        ? t("loginStatus.cancelled")
                        : t("loginStatus.chooseMethod")
              }
              description={
                succeeded
                  ? resultDescription
                  : connecting
                    ? t(
                        verifyConnection
                          ? "loginStatus.verifyingDescription"
                          : "loginStatus.browserHint",
                      )
                    : undefined
              }
            >
              {ownJob &&
              !succeeded &&
              (data?.verificationUrl || data?.userCode) ? (
                <LoginAuthorizationDetails
                  verificationUrl={data.verificationUrl}
                  userCode={data.userCode}
                  onOpen={openAuthorization}
                />
              ) : null}
            </LoginStatusPanel>
            {!succeeded && !connecting
              ? methods.map((method) => (
                  <Button
                    key={method.id}
                    size="settings"
                    disabled={pending}
                    onClick={() => begin(method.id)}
                  >
                    {methods.length === 1
                      ? t(failed || error ? "common:retry" : operationTitle)
                      : method.name}
                  </Button>
                ))
              : null}
            {!succeeded && (error || (ownJob && data?.errorCode)) ? (
              <p role="alert" className="text-destructive type-body">
                {error?.message ??
                  applicationErrorMessage({
                    code: data?.errorCode,
                    fallback: t("loginStatus.failedDescription"),
                  })}
              </p>
            ) : null}
            {!succeeded && authorizationOpenFailed ? (
              <p role="alert" className="text-destructive type-body break-all">
                {t("authorizationOpenFailed")}
                <br />
                {data?.verificationUrl}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button
              size="settings"
              variant={succeeded ? "primary" : "settings-outline"}
              disabled={cancel.isPending}
              onClick={close}
            >
              {t(succeeded ? "confirm" : "common:cancel")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
