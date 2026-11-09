import { Check, Copy } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useRef, useState, type FormEvent } from "react";
import {
  integrationServiceOptions,
  integrationServiceMutation,
  integrationServiceQueryKey,
} from "@/shared/api/integration-service";
import type { GetApiServiceResponse } from "@/shared/api/generated/types.gen";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Skeleton } from "@/shared/ui/skeleton";
import { Switch } from "@/shared/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import {
  SettingsCard,
  SettingsPageFooter,
  useSettingsDraftGuard,
} from "@/modules/settings";
import { useIntegrationCopy } from "../use-integration-copy";
import { SettingsFieldGroup, SettingsFieldRow } from "./settings-field-group";
import { SettingsGroup } from "./settings-section-layout";

type ServiceDraft = {
  ip: string;
  port: string;
  enabled: boolean;
  publicURL: string;
};

export function APIServiceSettings() {
  const t = useIntegrationCopy();
  const id = useId();
  const formId = `${id}-config`;
  const client = useQueryClient();
  const service = useQuery(integrationServiceOptions());
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState("");
  const [draft, setDraft] = useState<ServiceDraft>();
  const [baseline, setBaseline] = useState<ServiceDraft>();
  const [readbackFailed, setReadbackFailed] = useState(false);
  const [awaitingReadback, setAwaitingReadback] = useState(false);
  const draftVersion = useRef(0);
  const savedVersion = useRef<number | undefined>(undefined);
  const readbackRun = useRef(0);
  function changeDraft(value: ServiceDraft) {
    draftVersion.current += 1;
    setDraft(value);
  }
  const dirty = Boolean(
    draft &&
    baseline &&
    (draft.ip !== baseline.ip ||
      draft.port !== baseline.port ||
      draft.enabled !== baseline.enabled ||
      draft.publicURL !== baseline.publicURL),
  );
  function closeEditor() {
    draftVersion.current += 1;
    setDraft(undefined);
    save.reset();
    requestAnimationFrame(() => document.getElementById(`${id}-edit`)?.focus());
  }
  async function refreshSaved() {
    const run = ++readbackRun.current;
    const version = draftVersion.current;
    try {
      await client.invalidateQueries(
        { queryKey: integrationServiceQueryKey() },
        { throwOnError: true },
      );
      if (run !== readbackRun.current) return;
      setReadbackFailed(false);
      setAwaitingReadback(false);
      // 读取期间的新输入与后续提交属于新版本，旧回调不能退出该编辑。
      if (version === draftVersion.current && version === savedVersion.current)
        closeEditor();
    } catch {
      if (run === readbackRun.current) setReadbackFailed(true);
    }
  }
  const save = useMutation({
    ...integrationServiceMutation(),
    onSuccess: async (_receipt, variables) => {
      // 写入回执已成功，后续读取失败不能让重试重复提交同一配置。
      const saved = {
        ip: variables.body.ip,
        port: String(variables.body.port),
        enabled: variables.body.enabled === true,
        publicURL: variables.body.publicBaseUrl ?? "",
      };
      draftVersion.current += 1;
      savedVersion.current = draftVersion.current;
      setDraft(saved);
      setBaseline(saved);
      setAwaitingReadback(true);
      setCopied(false);
      const queryKey = integrationServiceOptions().queryKey;
      const updatedAt = client.getQueryState(queryKey)?.dataUpdatedAt ?? 0;
      await client.cancelQueries({ queryKey });
      // 回执确认接受字段；canonical URL与实际可用性仍等待GET，不把旧缓存当新配置。
      client.setQueryData<GetApiServiceResponse>(
        queryKey,
        (current) => ({
          ...current,
          ip: saved.ip,
          port: Number(saved.port),
          enabled: saved.enabled,
          publicBaseUrl: undefined,
          available: undefined,
          errorCode: undefined,
        }),
        { updatedAt },
      );
      await refreshSaved();
    },
  });
  useSettingsDraftGuard({ dirty, pending: save.isPending });
  function submit(event: FormEvent) {
    event.preventDefault();
    if (!draft || !dirty || invalid || save.isPending) return;
    readbackRun.current += 1;
    save.mutate({
      body: {
        ip: draft.ip.trim(),
        port: Number(draft.port),
        enabled: draft.enabled,
        publicBaseUrl: draft.publicURL.trim(),
      },
    });
  }
  const invalid =
    !draft?.ip.trim() ||
    !Number.isInteger(Number(draft.port)) ||
    Number(draft.port) < 1 ||
    Number(draft.port) > 65535;
  const base = service.data?.publicBaseUrl ?? "";
  const configuredEnabled = awaitingReadback
    ? baseline?.enabled
    : service.data?.enabled;
  const copyAction = base ? (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            type="button"
            size="icon"
            variant="ghost"
            disabled={
              awaitingReadback ||
              Boolean(service.error) ||
              typeof service.data?.available !== "boolean"
            }
            aria-label={t(copied ? "copiedAddress" : "copyAddress")}
            onClick={() => {
              setCopyError("");
              void navigator.clipboard
                .writeText(base)
                .then(() => setCopied(true))
                .catch(() => {
                  setCopied(false);
                  setCopyError(t("mcp.copyFailed"));
                });
            }}
          >
            {copied ? (
              <Check aria-hidden="true" />
            ) : (
              <Copy aria-hidden="true" />
            )}
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          {t(copied ? "copiedAddress" : "copyAddress")}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  ) : null;
  return (
    <>
      <SettingsGroup
        title={t("connectionSettings")}
        trailing={
          !draft && service.data ? (
            <Button
              id={`${id}-edit`}
              type="button"
              size="settings"
              variant="settings-outline"
              aria-label={t("editConnection")}
              disabled={
                awaitingReadback ||
                Boolean(service.error) ||
                typeof service.data?.available !== "boolean"
              }
              onClick={() => {
                const values = {
                  ip: service.data?.ip ?? "",
                  port: String(service.data?.port ?? ""),
                  enabled: service.data?.enabled === true,
                  publicURL: base,
                };
                setBaseline(values);
                changeDraft(values);
                save.reset();
              }}
            >
              {t("editConfiguration")}
            </Button>
          ) : undefined
        }
      >
        <SettingsCard data-od-id="api-service">
          {service.isPending ? (
            <div
              role="status"
              aria-busy="true"
              aria-label={t("serviceStatus")}
              className="grid gap-3 px-4 py-3"
            >
              <Skeleton className="h-row-detail w-full" />
              <Skeleton className="h-row-detail w-full" />
              <Skeleton className="h-row-detail w-full" />
            </div>
          ) : !service.data ? (
            <div className="px-4 py-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p role="alert" className="type-resource-body text-destructive">
                  {localApiErrorMessage(service.error, t("readFailed"))}
                </p>
                <Button
                  type="button"
                  size="settings"
                  variant="settings-outline"
                  disabled={service.isFetching}
                  onClick={() => void service.refetch()}
                >
                  {t("retry")}
                </Button>
              </div>
            </div>
          ) : (
            <>
              <form id={formId} onSubmit={submit}>
                <SettingsFieldGroup surface="plain">
                  <SettingsFieldRow
                    label={t(
                      draft
                        ? awaitingReadback
                          ? "lastReadPublicURL"
                          : "savedPublicURL"
                        : "publicURL",
                    )}
                  >
                    <div className="flex min-w-0 flex-wrap items-center gap-2">
                      <p className="type-resource-body min-w-0 flex-1 break-all select-text">
                        {awaitingReadback && !draft
                          ? t("pendingPublicURL")
                          : base ||
                            t(
                              service.error ||
                                typeof service.data.available !== "boolean"
                                ? "pendingPublicURL"
                                : "noPublicURL",
                            )}
                      </p>
                      {draft || !awaitingReadback ? copyAction : null}
                    </div>
                  </SettingsFieldRow>
                  <SettingsFieldRow
                    label={t("ip")}
                    htmlFor={draft ? `${id}-ip` : undefined}
                  >
                    {draft ? (
                      <div className="grid gap-2">
                        <Input
                          appearance="catalog"
                          id={`${id}-ip`}
                          value={draft.ip}
                          placeholder="127.0.0.1"
                          onChange={(event) =>
                            changeDraft({ ...draft, ip: event.target.value })
                          }
                          disabled={save.isPending}
                        />
                        <p className="type-ui text-content-secondary">
                          {t("ipHelp")}
                        </p>
                      </div>
                    ) : (
                      <p className="type-resource-body break-all select-text">
                        {(awaitingReadback ? baseline?.ip : service.data.ip) ||
                          t("unknownValue")}
                      </p>
                    )}
                  </SettingsFieldRow>
                  <SettingsFieldRow
                    label={t("port")}
                    htmlFor={draft ? `${id}-port` : undefined}
                  >
                    {draft ? (
                      <div className="grid gap-2">
                        <Input
                          appearance="catalog"
                          id={`${id}-port`}
                          type="number"
                          value={draft.port}
                          min={1}
                          max={65535}
                          placeholder="8431"
                          onChange={(event) =>
                            changeDraft({ ...draft, port: event.target.value })
                          }
                          disabled={save.isPending}
                        />
                        <p className="type-ui text-content-secondary">
                          {t("portHelp")}
                        </p>
                      </div>
                    ) : (
                      <p className="type-resource-body select-text">
                        {(awaitingReadback
                          ? baseline?.port
                          : service.data.port) ?? t("unknownValue")}
                      </p>
                    )}
                  </SettingsFieldRow>
                  <SettingsFieldRow
                    label={t("enabled")}
                    htmlFor={draft ? `${id}-enabled` : undefined}
                  >
                    {draft ? (
                      <Switch
                        id={`${id}-enabled`}
                        checked={draft.enabled}
                        onClick={() =>
                          changeDraft({ ...draft, enabled: !draft.enabled })
                        }
                        disabled={save.isPending}
                      />
                    ) : (
                      <p className="type-resource-body">
                        {t(
                          typeof configuredEnabled !== "boolean"
                            ? "unknownValue"
                            : configuredEnabled
                              ? "configurationEnabled"
                              : "configurationDisabled",
                        )}
                      </p>
                    )}
                  </SettingsFieldRow>
                  {draft ? (
                    <SettingsFieldRow
                      label={t("publicURL")}
                      htmlFor={`${id}-url`}
                    >
                      <div className="grid gap-2">
                        <Input
                          appearance="catalog"
                          id={`${id}-url`}
                          value={draft.publicURL}
                          placeholder={t("publicURLPlaceholder")}
                          onChange={(event) =>
                            changeDraft({
                              ...draft,
                              publicURL: event.target.value,
                            })
                          }
                          disabled={save.isPending}
                        />
                        <p className="type-ui text-content-secondary">
                          {t("publicURLHelp")}
                        </p>
                      </div>
                    </SettingsFieldRow>
                  ) : null}
                </SettingsFieldGroup>
              </form>
              {copyError ? (
                <p
                  role="alert"
                  className="type-resource-body text-destructive px-4 py-3"
                >
                  {copyError}
                </p>
              ) : null}
              {readbackFailed ? (
                <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
                  <p
                    role="alert"
                    className="type-resource-body text-destructive"
                  >
                    {t("savedReadFailed")}
                  </p>
                  <Button
                    type="button"
                    size="settings"
                    variant="settings-outline"
                    disabled={service.isFetching || save.isPending}
                    onClick={() => void refreshSaved()}
                  >
                    {t("retry")}
                  </Button>
                </div>
              ) : service.error ? (
                <div className="px-4 py-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <p
                      role="alert"
                      className="type-resource-body text-destructive"
                    >
                      {localApiErrorMessage(service.error, t("readFailed"))}
                    </p>
                    <Button
                      type="button"
                      size="settings"
                      variant="settings-outline"
                      disabled={service.isFetching}
                      onClick={() => void service.refetch()}
                    >
                      {t("retry")}
                    </Button>
                  </div>
                </div>
              ) : null}
            </>
          )}
        </SettingsCard>
      </SettingsGroup>
      {draft ? (
        <SettingsPageFooter>
          <div
            className="type-resource-body min-w-0 flex-1"
            role={save.error ? "alert" : undefined}
          >
            {save.error ? (
              <span className="text-destructive">
                {localApiErrorMessage(save.error, t("failed"))}
              </span>
            ) : dirty ? (
              t("unsavedChanges")
            ) : null}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              size="settings"
              variant="ghost"
              disabled={save.isPending}
              onClick={closeEditor}
            >
              {t("cancel")}
            </Button>
            <Button
              type="submit"
              size="settings"
              form={formId}
              disabled={!dirty || invalid || save.isPending}
              aria-busy={save.isPending}
            >
              {t(save.isPending ? "saving" : "save")}
            </Button>
          </div>
        </SettingsPageFooter>
      ) : null}
    </>
  );
}
