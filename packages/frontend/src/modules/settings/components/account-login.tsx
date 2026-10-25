import { useMutation, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  cancelAiAccountLoginMutation,
  startAiAccountLoginMutation,
  listAiAccountsQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { GithubComJieWaZiNeuxBotServerServicesAiaccountLogin as Login } from "@/shared/api/generated/types.gen";
import { openExternalURL } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";

import { useAccountLoginQuery } from "../lib/account-login-query";

export function AccountLogin({
  accountId,
  onClose,
}: {
  accountId: string;
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
  const [authorizationOpenFailed, setAuthorizationOpenFailed] = useState(false);
  const { query: poll, queryKey } = useAccountLoginQuery(
    accountId,
    startedLogin,
    !closing,
  );
  const login = poll.data ?? startedLogin;
  const startLogin = async () => {
    const result = await start.mutateAsync({ path: { accountId } });
    setLogin(result);
    if (!result.verificationUrl) {
      setAuthorizationOpenFailed(true);
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
    if (login?.complete)
      void queryClient.invalidateQueries({
        queryKey: listAiAccountsQueryKey(),
      });
  }, [login?.complete, queryClient]);
  const close = () => {
    setClosing(true);
    void queryClient.cancelQueries({ queryKey });
    if (login?.id && !login.complete)
      void cancel
        .mutateAsync({ path: { accountId, loginId: login.id } })
        .catch(() => undefined);
    onClose();
  };
  const error =
    start.error ??
    poll.error ??
    (login?.errorCode ? new Error(t("loginFailed")) : undefined);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent
        title={t("login")}
        description={t("subscriptionDescription")}
      >
        <div className="flex flex-col gap-4 px-6 pb-6">
          {login?.complete ? (
            <p className="type-body">{t("loginComplete")}</p>
          ) : login ? (
            <>
              <p className="type-body">{t("deviceInstruction")}</p>
              <code className="bg-muted type-title rounded-lg p-4 text-center select-all">
                {login.userCode}
              </code>
              {login.verificationUrl ? (
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  onClick={() => void openAuthorization()}
                >
                  {t("openAuthorization")}
                </Button>
              ) : null}
              {authorizationOpenFailed ? (
                <div role="alert" className="type-body text-destructive">
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
                  size="sm"
                  variant="secondary"
                  disabled={poll.isFetching}
                  onClick={() => void poll.refetch()}
                >
                  {t("common:retry")}
                </Button>
              ) : login.errorCode ? (
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => void startLogin().catch(() => undefined)}
                >
                  {t("startLogin")}
                </Button>
              ) : (
                <p className="type-caption text-content-secondary">
                  {t("waiting")}
                </p>
              )}
            </>
          ) : (
            <Button
              disabled={start.isPending}
              onClick={() => void startLogin().catch(() => undefined)}
            >
              {t("startLogin")}
            </Button>
          )}
          {error ? (
            <p role="alert" className="type-body text-destructive">
              {error instanceof Error
                ? error.message
                : t("common:unknownError")}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="secondary" onClick={close}>
            {t(login?.complete ? "confirm" : "common:cancel")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
