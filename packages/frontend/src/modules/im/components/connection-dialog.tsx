import { useDialogFocusRestore } from "./use-dialog-focus-restore";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useMutation, useQuery } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { QRCodeSVG } from "@/shared/ui/qr-code";
import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import {
  cancelImLogin,
  createImConnection,
  getImLogin,
  listImConnections,
  startImLogin,
  updateImConnection,
  verifyImLogin,
} from "@/shared/api/generated/sdk.gen";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { FieldLabel } from "@/shared/ui/field-label";
import { Skeleton } from "@/shared/ui/skeleton";
import {
  SettingsFieldGroup,
  SettingsFieldRow,
  SettingsGroup,
} from "@/modules/settings";
import { Input } from "@/shared/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { BindingForm } from "./binding-form";
import { BindingStatus, ConnectionStatus } from "./connection-status";
import { imQueryKeys, useIMBindings } from "../queries";
import type {
  ConnectionConfiguration,
  IMConnection,
  IMLogin,
  IMPlatform,
} from "../types";

export function ConnectionDialog({
  connection,
  platform = "weixin",
  onClose,
  onSaved,
  initialBindingId,
}: {
  connection?: IMConnection;
  platform?: IMPlatform;
  onClose: () => void;
  onSaved: () => void;
  initialBindingId?: string;
}) {
  const { t } = useTranslation("im");
  const dialogFocus = useDialogFocusRestore();
  const id = useId();
  const bindings = useIMBindings(connection?.id || "", Boolean(connection?.id));
  const [bindingId, setBindingId] = useState(initialBindingId || "");
  const binding =
    bindings.data?.find((item) => item.id === bindingId) ||
    bindings.data?.find((item) => item.state !== "approved") ||
    bindings.data?.[0];
  const [value, setValue] = useState<ConnectionConfiguration>({
    name: connection?.name || t(connection?.platform || platform),
    platform: connection?.platform || platform,
    enabled: connection?.enabled ?? true,
    keepAwake: connection?.keepAwake ?? true,
    appId: connection?.appId || "",
    appSecret: "",
    revision: connection?.revision || 0,
  });
  const isWecom = value.platform === "wecom";
  const supportsManual = value.platform === "feishu" || isWecom;
  const [saved, setSaved] = useState(connection);
  const [login, setLogin] = useState<IMLogin>();
  const [manual, setManual] = useState(
    Boolean(
      (connection?.platform === "feishu" || connection?.platform === "wecom") &&
      connection.appId,
    ),
  );
  const [code, setCode] = useState("");
  const closed = useRef(false);
  const started = useRef(false);
  const notified = useRef("");
  const progress = useQuery({
    queryKey: imQueryKeys.login(login?.id),
    queryFn: async ({ signal }) =>
      (
        await getImLogin(
          withLocalErrorSurface({
            path: { loginId: login?.id || "" },
            signal,
            throwOnError: true,
          }),
        )
      ).data,
    enabled: Boolean(login?.id),
  });
  useQueryEvents({
    topic: "im",
    resource: { kind: "login", id: login?.id || "" },
    queryKeys: [imQueryKeys.login(login?.id)],
    enabled: Boolean(login?.id),
  });
  const current = progress.data || login;
  const confirmed = current?.status === "confirmed";
  const start = useMutation({
    mutationFn: async () => {
      if (closed.current) return;
      let target = saved;
      if (!target?.id) {
        target = (
          await createImConnection(
            withLocalErrorSurface({
              body: { ...value, enabled: false, appId: "", appSecret: "" },
              throwOnError: true,
            }),
          )
        ).data;
        setSaved(target);
        onSaved();
      }
      if (!target?.id || closed.current) return;
      const next = (
        await startImLogin(
          withLocalErrorSurface({
            path: { connectionId: target.id },
            throwOnError: true,
          }),
        )
      ).data;
      if (closed.current && next?.id) {
        await cancelImLogin(
          withLocalErrorSurface({ path: { loginId: next.id } }),
        );
        return;
      }
      setLogin(next);
    },
  });
  const begin = start.mutate;
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    // 等严格模式完成订阅重挂载后再派发，避免异步完成失去 Mutation 观察者。
    if (
      !connection?.appId ||
      (connection.state === "login_required" && !supportsManual)
    ) {
      queueMicrotask(() => begin());
    }
  }, [begin, connection?.appId, connection?.state, supportsManual]);
  useEffect(() => {
    if (confirmed && current?.id !== notified.current) {
      notified.current = current?.id || "";
      setValue((previous) => ({ ...previous, enabled: true }));
      onSaved();
    }
  }, [confirmed, current?.id, onSaved]);
  const save = useMutation({
    mutationFn: async (credentials: boolean | undefined = true) => {
      // 扫码确认会更新版本；保存设置前读取当前连接，避免覆盖登录结果。
      const latest = saved?.id
        ? (
            await listImConnections(
              withLocalErrorSurface({ throwOnError: true }),
            )
          ).data?.resources?.find((item) => item.id === saved.id)
        : undefined;
      if (saved?.id && !latest) throw new Error(t("connectionNotFound"));
      const body = {
        ...value,
        name:
          value.name?.trim() === connection?.name
            ? latest?.name || value.name?.trim()
            : value.name?.trim(),
        keepAwake: latest?.keepAwake ?? value.keepAwake,
        enabled:
          manual && credentials
            ? true
            : (latest?.enabled ?? connection?.enabled ?? false),
        appId: manual && credentials ? value.appId : "",
        appSecret: manual && credentials ? value.appSecret : "",
      };
      const result = latest?.id
        ? await updateImConnection(
            withLocalErrorSurface({
              path: { connectionId: latest.id },
              body: { ...body, revision: latest.revision },
              throwOnError: true,
            }),
          )
        : await createImConnection(
            withLocalErrorSurface({ body, throwOnError: true }),
          );
      setSaved(result.data);
      onSaved();
      if (manual) setValue((previous) => ({ ...previous, appSecret: "" }));
    },
  });
  const verify = useMutation({
    mutationFn: () =>
      verifyImLogin(
        withLocalErrorSurface({
          path: { loginId: login?.id || "" },
          body: { code },
          throwOnError: true,
        }),
      ),
  });
  const finishClose = () => {
    closed.current = true;
    if (login?.id && !confirmed)
      void cancelImLogin(
        withLocalErrorSurface({ path: { loginId: login.id } }),
      );
    onSaved();
    onClose();
  };
  const close = () => {
    if (save.isPending) return;
    if (
      !connection &&
      saved?.id &&
      value.name?.trim() &&
      value.name.trim() !== saved.name
    ) {
      // 取消配置只保留名称，不提交尚未完成的手工凭据或提前启用连接。
      save.mutate(false, { onSuccess: finishClose });
      return;
    }
    finishClose();
  };
  const changeMode = () => {
    if (!manual && login?.id && !confirmed)
      void cancelImLogin(
        withLocalErrorSurface({ path: { loginId: login.id } }),
      );
    setManual(!manual);
    if (manual) begin();
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate(undefined, {
      onSuccess: () => {
        if (connection) close();
      },
    });
  };
  const canSave =
    value.name?.trim() &&
    (connection?.appId ||
      confirmed ||
      (manual && value.appId && (value.appSecret || saved?.appId)));
  const error =
    (!manual && start.error) ||
    save.error ||
    (!manual && progress.error) ||
    verify.error;
  const loginFields = (
    <SettingsGroup
      title={t("connectPlatform", { platform: t(value.platform || "weixin") })}
      trailing={
        supportsManual ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={changeMode}
            disabled={start.isPending || save.isPending}
          >
            {t(manual ? "returnQR" : isWecom ? "manualWecom" : "manualFeishu")}
          </Button>
        ) : undefined
      }
    >
      {manual ? (
        <SettingsFieldGroup
          description={
            t(isWecom ? "manualWecomHint" : "manualHint") +
            (saved?.appId
              ? " " + t(isWecom ? "existingWecomHint" : "existingFeishuHint")
              : "")
          }
        >
          <SettingsFieldRow
            label={
              <FieldLabel htmlFor={id + "-app"} required>
                {t(isWecom ? "wecomBotId" : "appId")}
              </FieldLabel>
            }
          >
            <Input
              id={id + "-app"}
              placeholder={t(
                isWecom ? "wecomBotIdPlaceholder" : "appIdPlaceholder",
              )}
              value={value.appId}
              required
              disabled={Boolean(saved?.appId) || save.isPending}
              onChange={(e) => setValue({ ...value, appId: e.target.value })}
            />
          </SettingsFieldRow>
          <SettingsFieldRow
            label={
              <FieldLabel htmlFor={id + "-secret"} required={!saved?.appId}>
                {t(isWecom ? "wecomBotSecret" : "appSecret")}
              </FieldLabel>
            }
          >
            <Input
              id={id + "-secret"}
              type="password"
              autoComplete="new-password"
              disabled={save.isPending}
              value={value.appSecret}
              required={!saved?.appId}
              placeholder={t(
                isWecom && !saved?.appId
                  ? "wecomBotSecretPlaceholder"
                  : "secretHint",
              )}
              onChange={(e) =>
                setValue({ ...value, appSecret: e.target.value })
              }
            />
          </SettingsFieldRow>
        </SettingsFieldGroup>
      ) : current || start.isPending || !connection?.appId ? (
        <div
          className="bg-surface-tertiary grid justify-items-center gap-3 rounded-lg p-4 text-center"
          aria-live="polite"
        >
          {isWecom && (!confirmed || !connection) ? (
            <p className="text-content-secondary type-body">
              {t(
                confirmed
                  ? "wecomFirstMessage"
                  : saved?.appId
                    ? "wecomReauthorizeHint"
                    : "wecomScanHint",
              )}
            </p>
          ) : null}
          {current?.qrContent ? (
            <QRCodeSVG
              className="h-auto max-w-full"
              value={current.qrContent}
              size={208}
              marginSize={4}
              title={t("scan")}
            />
          ) : null}
          <p
            className={
              confirmed
                ? "text-success type-body"
                : "text-content-secondary type-body"
            }
          >
            {t("loginStatus." + (current?.status || "preparing"), {
              defaultValue: t("loginStatus.wait"),
            })}
          </p>
          {current?.errorCode ? (
            <p className="text-destructive type-caption">
              {t("errors." + current.errorCode, {
                defaultValue: t("operationFailed"),
              })}
            </p>
          ) : null}
          {!confirmed ? (
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              aria-label={t("refreshQR")}
              title={t("refreshQR")}
              disabled={start.isPending}
              onClick={() => begin()}
            >
              <RefreshCw />
            </Button>
          ) : null}
          {current?.status === "need_verifycode" ? (
            <div className="w-full text-left">
              <SettingsFieldGroup>
                <SettingsFieldRow
                  label={
                    <FieldLabel htmlFor={id + "-code"}>{t("code")}</FieldLabel>
                  }
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <Input
                      id={id + "-code"}
                      placeholder={t("codePlaceholder")}
                      value={code}
                      onChange={(e) => setCode(e.target.value)}
                    />
                    <Button
                      type="button"
                      disabled={!code || verify.isPending}
                      onClick={() => verify.mutate()}
                    >
                      {t("verify")}
                    </Button>
                  </div>
                </SettingsFieldRow>
              </SettingsFieldGroup>
            </div>
          ) : null}
        </div>
      ) : (
        <SettingsFieldGroup>
          <SettingsFieldRow label={t("state")}>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <ConnectionStatus connection={connection} />
              <Button
                type="button"
                size="sm"
                variant="secondary"
                disabled={start.isPending}
                onClick={() => begin()}
              >
                {t("reconnect")}
              </Button>
            </div>
          </SettingsFieldRow>
        </SettingsFieldGroup>
      )}

      {error ? (
        <p role="alert" className="text-destructive type-caption">
          {localApiErrorMessage(error, t("operationFailed"))}
        </p>
      ) : null}
    </SettingsGroup>
  );
  const nameChanged = Boolean(connection && value.name?.trim() !== saved?.name);
  const needsCredentialRecovery =
    isWecom &&
    manual &&
    (connection?.state === "login_required" ||
      connection?.errorCode === "IMConnectionReplaced");
  const nameField = (
    <SettingsFieldGroup title={t("connectionSettings")}>
      <SettingsFieldRow
        label={
          <FieldLabel htmlFor={id + "-name"} required>
            {t("name")}
          </FieldLabel>
        }
      >
        <div className="grid gap-2">
          <Input
            id={id + "-name"}
            value={value.name}
            placeholder={t("namePlaceholder")}
            maxLength={120}
            disabled={save.isPending}
            required
            onChange={(event) =>
              setValue({ ...value, name: event.target.value })
            }
          />
          <p className="text-content-secondary type-caption">{t("nameHint")}</p>
        </div>
      </SettingsFieldRow>
    </SettingsFieldGroup>
  );
  const busy =
    save.isPending ||
    start.isPending ||
    Boolean(login && !confirmed && !manual);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent
        {...dialogFocus}
        size="lg"
        title={t(connection ? "editConnection" : "connectPlatform", {
          platform: t(value.platform || "weixin"),
        })}
      >
        {connection && binding ? (
          <BindingForm
            key={binding.id}
            binding={binding}
            onClose={close}
            onSaved={onSaved}
            disabled={busy || !value.name?.trim()}
            onBeforeSave={
              nameChanged ||
              (manual && value.appSecret) ||
              needsCredentialRecovery
                ? () => save.mutateAsync(undefined)
                : undefined
            }
            beforeFields={
              <>
                {nameField}
                {loginFields}
                <SettingsFieldGroup title={t("chatAccess")}>
                  <SettingsFieldRow label={t("name")}>
                    {(bindings.data?.length || 0) > 1 ? (
                      <Select value={binding.id} onValueChange={setBindingId}>
                        <SelectTrigger aria-label={t("chatAccess")}>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {bindings.data?.map((item) => (
                            <SelectItem key={item.id} value={item.id || ""}>
                              {item.name}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    ) : (
                      <p className="type-body break-all">{binding.name}</p>
                    )}
                  </SettingsFieldRow>
                  <SettingsFieldRow label={t("state")}>
                    <BindingStatus binding={binding} />
                  </SettingsFieldRow>
                </SettingsFieldGroup>
              </>
            }
          />
        ) : (
          <form onSubmit={submit} className="contents">
            <div className="grid min-h-0 gap-6 overflow-y-auto px-6 pb-6">
              {nameField}
              {loginFields}
              {connection && bindings.isPending ? (
                <div role="status" aria-label={t("loading")} aria-busy="true">
                  <Skeleton className="h-row-default w-full" />
                </div>
              ) : null}
              {connection && !bindings.isPending && !binding ? (
                <p className="text-content-secondary type-body">
                  {t(isWecom ? "wecomFirstMessage" : "waitingForChat")}
                </p>
              ) : null}
              {bindings.error && connection ? (
                <p role="alert" className="text-destructive type-body">
                  {localApiErrorMessage(bindings.error, t("operationFailed"))}
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => void bindings.refetch()}
                  >
                    {t("reload")}
                  </Button>
                </p>
              ) : null}
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="secondary"
                disabled={save.isPending || (confirmed && !value.name?.trim())}
                onClick={close}
              >
                {t(confirmed || connection ? "done" : "cancel")}
              </Button>
              {manual || connection ? (
                <Button type="submit" disabled={!canSave || busy}>
                  {t("save")}
                </Button>
              ) : null}
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
