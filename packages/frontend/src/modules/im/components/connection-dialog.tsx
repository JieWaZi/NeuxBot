import { useDialogFocusRestore } from "./use-dialog-focus-restore";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useMutation, useQuery } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { QRCodeSVG } from "@/shared/ui/qr-code";
import {
  useEffect,
  useId,
  useRef,
  useState,
  type RefObject,
  type FormEvent,
} from "react";
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
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { FieldLabel } from "@/shared/ui/field-label";
import { Skeleton } from "@/shared/ui/skeleton";
import {
  useSettingsDraftGuard,
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
import { FormFooter } from "./form-footer";
import { BindingForm } from "./binding-form";
import { BindingStatus, ConnectionStatus } from "./connection-status";
import { imQueryKeys, useIMBindings } from "../queries";
import type {
  ConnectionConfiguration,
  IMConnection,
  IMLogin,
  IMPlatform,
} from "../types";

export function ConnectionConfigurationForm({
  connection,
  platform = "weixin",
  onClose,
  onSaved,
  initialBindingId,
  host = "page",
  includeBindings = true,
  statusStale = false,
  dismissRef,
}: {
  connection?: IMConnection;
  platform?: IMPlatform;
  onClose: () => void;
  onSaved: () => void;
  initialBindingId?: string;
  host?: "page" | "dialog";
  includeBindings?: boolean;
  statusStale?: boolean;
  dismissRef?: RefObject<(() => void) | null>;
}) {
  const { t } = useTranslation("im");
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
  const [initialValue] = useState(value);
  const isWecom = value.platform === "wecom";
  const supportsManual = value.platform === "feishu" || isWecom;
  const [saved, setSaved] = useState(connection);
  const [credentialRecoverySaved, setCredentialRecoverySaved] = useState(false);
  const [login, setLogin] = useState<IMLogin>();
  const [manual, setManual] = useState(
    Boolean(
      (connection?.platform === "feishu" || connection?.platform === "wecom") &&
      connection.appId,
    ),
  );
  const [code, setCode] = useState("");
  const closed = useRef(false);
  const activeLogin = useRef<IMLogin | undefined>(undefined);
  const lifecycle = useRef({ generation: 0 });
  const cancelledLogins = useRef(new Set<string>());
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
  useEffect(() => {
    activeLogin.current = current;
  }, [current]);
  const cancelActiveLogin = () => {
    const pending = activeLogin.current;
    activeLogin.current = undefined;
    if (
      pending?.id &&
      !cancelledLogins.current.has(pending.id) &&
      !["confirmed", "cancelled", "expired", "failed"].includes(
        pending.status || "",
      )
    ) {
      cancelledLogins.current.add(pending.id);
      void cancelImLogin(
        withLocalErrorSurface({ path: { loginId: pending.id } }),
      );
    }
  };
  useEffect(() => {
    closed.current = false;
    const owner = lifecycle.current;
    const generation = ++owner.generation;
    return () => {
      queueMicrotask(() => {
        if (owner.generation !== generation) return;
        closed.current = true;
        cancelActiveLogin();
      });
    };
  }, []);
  const start = useMutation({
    mutationFn: async () => {
      if (closed.current) return;
      cancelActiveLogin();
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
      activeLogin.current = next;
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
          value.name?.trim() === saved?.name
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
      setSaved(result.data ? { ...saved, ...body, ...result.data } : saved);
      onSaved();
      if (manual) {
        setCredentialRecoverySaved(true);
        setValue((previous) => ({ ...previous, appSecret: "" }));
      }
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
    cancelActiveLogin();
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
  useEffect(() => {
    if (dismissRef) dismissRef.current = close;
  });
  const changeMode = () => {
    if (!manual) cancelActiveLogin();
    setManual(!manual);
    if (manual) begin();
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (save.isPending) return;
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
  const modeAction = supportsManual ? (
    <Button
      type="button"
      size={host === "page" ? "settings" : "sm"}
      variant={host === "page" ? "settings-outline" : "ghost"}
      onClick={changeMode}
      disabled={start.isPending || save.isPending}
    >
      {t(manual ? "returnQR" : isWecom ? "manualWecom" : "manualFeishu")}
    </Button>
  ) : null;
  const credentialDescription =
    t(isWecom ? "manualWecomHint" : "manualHint") +
    (saved?.appId
      ? " " + t(isWecom ? "existingWecomHint" : "existingFeishuHint")
      : "");
  const secretRow = (
    <SettingsFieldRow
      label={
        <FieldLabel htmlFor={id + "-secret"} required={!saved?.appId}>
          {t(isWecom ? "wecomBotSecret" : "appSecret")}
        </FieldLabel>
      }
    >
      <Input
        appearance={host === "page" ? "catalog" : "default"}
        id={id + "-secret"}
        type="password"
        autoComplete="new-password"
        disabled={save.isPending}
        value={value.appSecret}
        required={!saved?.appId}
        placeholder={t(
          isWecom && !saved?.appId ? "wecomBotSecretPlaceholder" : "secretHint",
        )}
        onChange={(e) => setValue({ ...value, appSecret: e.target.value })}
      />
      {host === "page" ? (
        <p className="type-resource-body text-content-secondary mt-2">
          {credentialDescription}
        </p>
      ) : null}
    </SettingsFieldRow>
  );
  const credentialRows = (
    <>
      <SettingsFieldRow
        label={
          <FieldLabel htmlFor={id + "-app"} required>
            {t(isWecom ? "wecomBotId" : "appId")}
          </FieldLabel>
        }
      >
        {host === "page" && saved?.appId ? (
          <p className="type-label text-content-secondary break-all select-text">
            {saved.appId}
          </p>
        ) : (
          <Input
            appearance={host === "page" ? "catalog" : "default"}
            id={id + "-app"}
            placeholder={t(
              isWecom ? "wecomBotIdPlaceholder" : "appIdPlaceholder",
            )}
            value={value.appId}
            required
            disabled={Boolean(saved?.appId) || save.isPending}
            onChange={(e) => setValue({ ...value, appId: e.target.value })}
          />
        )}
      </SettingsFieldRow>
      {secretRow}
    </>
  );
  const loginBody = (
    <div
      className="grid justify-items-center gap-3 p-4 text-center"
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
          <SettingsFieldGroup surface={host === "page" ? "plain" : "card"}>
            <SettingsFieldRow
              label={
                <FieldLabel htmlFor={id + "-code"}>{t("code")}</FieldLabel>
              }
            >
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  appearance={host === "page" ? "catalog" : "default"}
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
  );
  const reconnectAction = (
    <Button
      type="button"
      size={host === "page" ? "settings" : "sm"}
      variant={host === "page" ? "settings-outline" : "secondary"}
      disabled={start.isPending || save.isPending}
      onClick={() => begin()}
    >
      {t("reconnect")}
    </Button>
  );
  const showingLogin = Boolean(
    current || start.isPending || !connection?.appId,
  );
  const loginError =
    error && (host === "dialog" || error !== save.error) ? (
      <p role="alert" className="text-destructive type-caption">
        {localApiErrorMessage(error, t("operationFailed"))}
      </p>
    ) : null;
  const loginFields = (
    <SettingsGroup
      title={t("connectPlatform", { platform: t(value.platform || "weixin") })}
      trailing={modeAction}
    >
      {manual ? (
        <SettingsFieldGroup description={credentialDescription}>
          {credentialRows}
        </SettingsFieldGroup>
      ) : showingLogin ? (
        loginBody
      ) : (
        <SettingsFieldGroup>
          <SettingsFieldRow label={t("state")}>
            <div className="flex flex-wrap items-center justify-between gap-3">
              {connection ? <ConnectionStatus connection={connection} /> : null}
              {reconnectAction}
            </div>
          </SettingsFieldRow>
        </SettingsFieldGroup>
      )}
      {loginError}
    </SettingsGroup>
  );
  const nameChanged = Boolean(connection && value.name?.trim() !== saved?.name);
  const needsCredentialRecovery =
    isWecom &&
    manual &&
    !credentialRecoverySaved &&
    (connection?.state === "login_required" ||
      connection?.errorCode === "IMConnectionReplaced");
  const nameRow = (
    <SettingsFieldRow
      label={
        <FieldLabel htmlFor={id + "-name"} required>
          {t("name")}
        </FieldLabel>
      }
    >
      <div className="grid gap-2">
        <Input
          appearance={host === "page" ? "catalog" : "default"}
          id={id + "-name"}
          value={value.name}
          placeholder={t("namePlaceholder")}
          maxLength={120}
          disabled={save.isPending}
          required
          onChange={(event) => setValue({ ...value, name: event.target.value })}
        />
        {host === "dialog" ? (
          <p className="text-content-secondary type-caption">{t("nameHint")}</p>
        ) : null}
      </div>
    </SettingsFieldRow>
  );
  const nameField = (
    <SettingsFieldGroup title={t("connectionSettings")}>
      {nameRow}
    </SettingsFieldGroup>
  );
  const pageFields = (
    <SettingsFieldGroup surface="plain">
      {connection ? (
        <SettingsFieldRow label={t("state")}>
          <ConnectionStatus
            connection={connection}
            stale={statusStale}
            appearance="pill"
          />
        </SettingsFieldRow>
      ) : null}
      {nameRow}
      {manual ? (
        <>
          <SettingsFieldRow label={t(isWecom ? "wecomBotId" : "appId")}>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="min-w-0 flex-1">
                {saved?.appId ? (
                  <p className="type-label text-content-secondary break-all select-text">
                    {saved.appId}
                  </p>
                ) : (
                  <Input
                    appearance="catalog"
                    id={id + "-app"}
                    aria-label={t(isWecom ? "wecomBotId" : "appId")}
                    placeholder={t(
                      isWecom ? "wecomBotIdPlaceholder" : "appIdPlaceholder",
                    )}
                    value={value.appId}
                    required
                    disabled={save.isPending}
                    onChange={(event) =>
                      setValue({ ...value, appId: event.target.value })
                    }
                  />
                )}
              </div>
              {modeAction}
            </div>
          </SettingsFieldRow>

          {secretRow}
        </>
      ) : (
        <SettingsFieldRow
          label={t(
            connection?.platform === "wecom"
              ? "wecomBotId"
              : connection?.platform === "feishu"
                ? "appId"
                : "botAccount",
          )}
        >
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="type-label text-content-secondary min-w-0 break-all select-text">
              {connection?.appId || t("notConnected")}
            </p>
            <div className="flex flex-wrap items-center gap-2">
              {!showingLogin ? reconnectAction : null}
              {modeAction}
            </div>
          </div>
        </SettingsFieldRow>
      )}
      {!manual && showingLogin ? (
        <SettingsFieldRow
          label={t("connectPlatform", {
            platform: t(value.platform || "weixin"),
          })}
        >
          {loginBody}
        </SettingsFieldRow>
      ) : null}
      {loginError ? (
        <SettingsFieldRow label={t("needsAttention")}>
          {loginError}
        </SettingsFieldRow>
      ) : null}
      {connection ? (
        <SettingsFieldRow label={t("createdAt")}>
          <p className="type-label text-content-secondary">
            {connection.createdAt
              ? new Intl.DateTimeFormat(undefined, {
                  dateStyle: "medium",
                  timeStyle: "short",
                }).format(new Date(connection.createdAt))
              : "—"}
          </p>
        </SettingsFieldRow>
      ) : null}
    </SettingsFieldGroup>
  );
  const busy =
    save.isPending ||
    start.isPending ||
    Boolean(login && !confirmed && !manual);
  const dirty =
    value.name !== initialValue.name ||
    Boolean(value.appSecret) ||
    (!connection?.appId && value.appId !== initialValue.appId);
  useSettingsDraftGuard({
    dirty: host === "page" && dirty,
    pending: host === "page" && save.isPending,
  });
  return (
    <>
      {connection && binding && includeBindings ? (
        <BindingForm
          key={binding.id}
          host={host}
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
                      <SelectTrigger
                        appearance={host === "page" ? "settings" : "default"}
                        size={host === "page" ? "settings" : "sm"}
                        aria-label={t("chatAccess")}
                      >
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
        <form id={id + "-form"} onSubmit={submit} className="contents">
          <div
            className={
              host === "dialog"
                ? "grid min-h-0 gap-6 overflow-y-auto px-6 pb-6"
                : "min-h-0"
            }
          >
            {host === "page" ? (
              pageFields
            ) : (
              <>
                {nameField}
                {loginFields}
              </>
            )}
            {connection && includeBindings && bindings.isPending ? (
              <div role="status" aria-label={t("loading")} aria-busy="true">
                <Skeleton className="h-row-default w-full" />
              </div>
            ) : null}
            {connection &&
            includeBindings &&
            !bindings.isPending &&
            !binding ? (
              <p className="text-content-secondary type-body">
                {t(isWecom ? "wecomFirstMessage" : "waitingForChat")}
              </p>
            ) : null}
            {bindings.error && connection && includeBindings ? (
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
          <FormFooter host={host}>
            {host === "page" && save.error ? (
              <p role="alert" className="text-destructive type-body basis-full">
                {localApiErrorMessage(save.error, t("operationFailed"))}
              </p>
            ) : null}
            {host === "page" && dirty ? (
              <span className="type-body text-content-secondary mr-auto">
                {t("unsavedChanges")}
              </span>
            ) : null}
            <Button
              type="button"
              variant="secondary"
              disabled={save.isPending || (confirmed && !value.name?.trim())}
              onClick={close}
            >
              {t(
                host === "page"
                  ? "cancel"
                  : confirmed || connection
                    ? "done"
                    : "cancel",
              )}
            </Button>
            {manual || connection ? (
              <Button
                type="submit"
                form={id + "-form"}
                aria-busy={save.isPending}
                disabled={!canSave || busy || (host === "page" && !dirty)}
              >
                {t("save")}
              </Button>
            ) : null}
          </FormFooter>
        </form>
      )}
    </>
  );
}

/** 新增连接保留扫码弹窗，已有连接由三级页面承载。 */
export function ConnectionDialog(
  props: Omit<Parameters<typeof ConnectionConfigurationForm>[0], "host">,
) {
  const { t } = useTranslation("im");
  const dialogFocus = useDialogFocusRestore();
  const dismissRef = useRef<(() => void) | null>(null);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) dismissRef.current?.();
      }}
    >
      <DialogContent
        {...dialogFocus}
        size="lg"
        title={t(props.connection ? "editConnection" : "connectPlatform", {
          platform: t(props.connection?.platform || props.platform || "weixin"),
        })}
      >
        <ConnectionConfigurationForm
          {...props}
          host="dialog"
          dismissRef={dismissRef}
        />
      </DialogContent>
    </Dialog>
  );
}
