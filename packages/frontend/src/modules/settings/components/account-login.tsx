import { useMutation, useQueryClient } from "@tanstack/react-query";
import { nativeModelCatalogQueryKey } from "@/shared/api/runtime-queries";
import { refreshRuntime } from "@/shared/api/generated/sdk.gen";
import {
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  cancelAiAccountLoginMutation,
  startAiAccountLoginMutation,
  listAiAccountsQueryKey,
  listRuntimesQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { GithubComJieWaZiNeuxBotServerServicesAiaccountLogin as Login } from "@/shared/api/generated/types.gen";
import { openExternalURL } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";

import { useAccountLoginQuery } from "../lib/account-login-query";
import {
  LoginAuthorizationDetails,
  LoginStatusPanel,
} from "./login-status-panel";

export function AccountLogin({
  accountId,
  browserAuthorization = false,
  runtimeId,
  onClose,
}: {
  accountId: string;
  browserAuthorization?: boolean;
  runtimeId?: string;
  onClose: () => void;
}) {
  const { t } = useTranslation(["aiAccounts", "common"]);
  const queryClient = useQueryClient();
  const start = useMutation(
    withLocalErrorMutation(startAiAccountLoginMutation()),
  );
  const cancel = useMutation(
    withLocalErrorMutation(cancelAiAccountLoginMutation()),
  );
  const [startedLogin, setLogin] = useState<Login>();
  const [closing, setClosing] = useState(false);
  const closed = useRef(false);
  const [runtimeRefreshError, setRuntimeRefreshError] = useState<unknown>();
  const [authorizationOpenFailed, setAuthorizationOpenFailed] = useState(false);
  const { query: poll, queryKey } = useAccountLoginQuery(
    accountId,
    startedLogin,
    !closing,
  );
  const login = poll.data ?? startedLogin;
  const startLogin = async () => {
    setRuntimeRefreshError(undefined);
    const result = await start.mutateAsync({ path: { accountId } });
    if (closed.current) {
      if (result.id && !result.complete)
        await cancel.mutateAsync({ path: { accountId, loginId: result.id } });
      return;
    }
    setLogin(result);
    if (!result.verificationUrl) {
      setAuthorizationOpenFailed(!browserAuthorization);
      return;
    }
    try {
      await openExternalURL(result.verificationUrl);
    } catch {
      setAuthorizationOpenFailed(true);
    }
  };
  const openAuthorization = async () => {
    if (!login?.verificationUrl) return;
    try {
      await openExternalURL(login.verificationUrl);
      setAuthorizationOpenFailed(false);
    } catch {
      setAuthorizationOpenFailed(true);
    }
  };
  useEffect(() => {
    if (!login?.complete) return;
    void (async () => {
      await queryClient.invalidateQueries({
        queryKey: listAiAccountsQueryKey(),
      });
      await queryClient.invalidateQueries({
        queryKey: nativeModelCatalogQueryKey({ accountId }),
      });
      if (runtimeId) {
        await refreshRuntime(
          withLocalErrorSurface({ path: { runtimeId }, throwOnError: true }),
        );
        await queryClient.invalidateQueries({
          queryKey: listRuntimesQueryKey(),
        });
      }
    })().catch((error: unknown) => setRuntimeRefreshError(error));
  }, [accountId, runtimeId, login?.complete, queryClient]);
  const close = () => {
    closed.current = true;
    setClosing(true);
    void queryClient.cancelQueries({ queryKey });
    if (login?.id && !login.complete)
      void cancel
        .mutateAsync({ path: { accountId, loginId: login.id } })
        .catch(() => undefined);
    onClose();
  };
  const error = start.isPending
    ? undefined
    : (start.error ??
      poll.error ??
      runtimeRefreshError ??
      (login?.errorCode
        ? new Error(t("loginStatus.failedDescription"))
        : undefined));
  const phase = start.isPending
    ? "pending"
    : login?.complete
      ? "success"
      : login?.errorCode || start.isError
        ? "error"
        : login
          ? "pending"
          : "ready";
  const statusTitle = start.isPending
    ? t("agents.authentication.preparing")
    : login?.complete
      ? t("loginStatus.connected")
      : login?.errorCode || start.isError
        ? t("loginStatus.failed")
        : login
          ? t("waiting")
          : t("loginStatus.ready");
  const statusDescription = start.isPending
    ? undefined
    : login?.complete
      ? t("loginComplete")
      : login
        ? t(login.userCode ? "deviceInstruction" : "loginStatus.browserHint")
        : t("subscriptionDescription");
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent title={t("login")} size="sm">
        <div className="flex min-h-0 flex-col gap-4 overflow-y-auto px-6 pb-6">
          <LoginStatusPanel
            state={phase}
            title={statusTitle}
            description={statusDescription}
          >
            {login &&
            !start.isPending &&
            !login.complete &&
            !login.errorCode ? (
              <LoginAuthorizationDetails
                userCode={login.userCode}
                verificationUrl={login.verificationUrl}
                onOpen={() => void openAuthorization()}
              />
            ) : null}
          </LoginStatusPanel>
          {!login ? (
            <Button
              size="settings"
              disabled={start.isPending}
              onClick={() => void startLogin().catch(() => undefined)}
            >
              {t(browserAuthorization ? "startBrowserLogin" : "startLogin")}
            </Button>
          ) : null}
          {login && !login.complete ? (
            <>
              {authorizationOpenFailed ? (
                <div
                  role="alert"
                  className="type-body text-destructive break-all"
                >
                  <p>
                    {t(
                      login.verificationUrl
                        ? "authorizationOpenFailed"
                        : "authorizationUrlMissing",
                    )}
                  </p>
                  {login.verificationUrl ? (
                    <code className="select-all">{login.verificationUrl}</code>
                  ) : null}
                </div>
              ) : null}
              {poll.isError ? (
                <Button
                  size="settings"
                  variant="settings-outline"
                  disabled={poll.isFetching}
                  onClick={() => void poll.refetch()}
                >
                  {t("common:retry")}
                </Button>
              ) : login.errorCode ? (
                <Button
                  size="settings"
                  disabled={start.isPending}
                  onClick={() => void startLogin().catch(() => undefined)}
                >
                  {t("common:retry")}
                </Button>
              ) : null}
            </>
          ) : null}
          {error ? (
            <p role="alert" className="type-body text-destructive">
              {error instanceof Error
                ? error.message
                : t("common:unknownError")}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            size="settings"
            variant={login?.complete ? "primary" : "settings-outline"}
            onClick={close}
          >
            {t(login?.complete ? "confirm" : "common:cancel")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
