import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import {
  ChevronRight,
  Eye,
  EyeOff,
  ExternalLink,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
  X,
} from "lucide-react";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  createAiAccountMutation,
  deleteAiAccountMutation,
  listAiAccountsOptions,
  listAiAccountsQueryKey,
  previewAiAccountModelsMutation,
  refreshAiAccountModelsMutation,
  updateAiAccountMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type {
  GithubComJieWaZiNeuxBotServerRepositoriesModelsAiAccount as AIAccount,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsAiAccountEndpoint as Endpoint,
  GithubComJieWaZiNeuxBotServerApisAiAccountsAiAccountModel as AIAccountModel,
} from "@/shared/api/generated/types.gen";
import { getAiAccountCredential } from "@/shared/api/generated/sdk.gen";
import { openExternalURL } from "@/shared/platform";
import { AccountLogin } from "./account-login";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/shared/ui/collapsible";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import { FieldCheckboxDropdown } from "@/shared/ui/field-checkbox-dropdown";
import { SearchField } from "@/shared/ui/search-field";
import { SearchableMultiSelect } from "@/shared/ui/searchable-multi-select";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import {
  accountProviderIDs,
  accountPresets,
  officialEndpoints,
  presetForEndpoint,
  providerForPreset,
  providerWebsites,
  protocols,
  type AccountPresetID,
} from "../lib/account-presets";
import {
  claudeRoleDefaultsFor,
  modelDefaultsFor,
} from "../lib/account-model-defaults";
import { SettingsContentShell } from "./settings-section-layout";
import { SettingsFieldGroup, SettingsFieldRow } from "./settings-field-group";

const reasoningLevels = [
  "none",
  "minimal",
  "low",
  "medium",
  "high",
  "xhigh",
  "max",
];
const defaultResponseReasoningLevels = ["low", "medium", "high"];
const claudeRoles = ["sonnet", "opus", "haiku", "fable", "subagent"];
const followChatModelValue = "__ally_follow_chat_model__";

function AccountForm({
  account,
  accounts,
  onSaved,
  onClose,
}: {
  account?: AIAccount;
  accounts: AIAccount[];
  onSaved: (id?: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation(["aiAccounts", "common"]);
  const formId = useId();
  const [name, setName] = useState(account?.name ?? t("presets.openai"));
  const [nameEdited, setNameEdited] = useState(Boolean(account));
  const [authMode, setAuthMode] = useState<"api-key" | "subscription">(
    account?.authMode === "subscription" ? "subscription" : "api-key",
  );
  const [preset, setPreset] = useState<AccountPresetID>(() =>
    presetForEndpoint(account?.endpoints?.[0]),
  );
  const [endpoint, setEndpoint] = useState<Endpoint>(
    account?.endpoints?.[0] ?? {
      protocol: "openai-responses",
      baseUrl: officialEndpoints["openai-responses"],
    },
  );
  const [apiKey, setApiKey] = useState("");
  const [keyVisible, setKeyVisible] = useState(false);
  const [keyEdited, setKeyEdited] = useState(false);
  const [customModels, setCustomModels] = useState<string[]>(
    account?.customModels ?? [],
  );
  const [discoveredModels, setDiscoveredModels] = useState<AIAccountModel[]>(
    (account?.modelCatalog ?? []).map((id) => ({ id, name: id })),
  );
  const [selectedModels, setSelectedModels] = useState<string[]>(
    account?.selectedModels ?? [],
  );
  const [modelContextSizes, setModelContextSizes] = useState<
    Record<string, number>
  >(account?.modelContextSizes ?? {});
  const [modelDisplayNames, setModelDisplayNames] = useState<
    Record<string, string>
  >(account?.modelDisplayNames ?? {});
  const [modelReasoningLevels, setModelReasoningLevels] = useState<
    Record<string, string[]>
  >(account?.modelReasoningLevels ?? {});
  const [claudeModelRoles, setClaudeModelRoles] = useState<
    Record<string, string>
  >(account?.claudeModelRoles ?? {});
  const [modelContextInputs, setModelContextInputs] = useState<
    Record<string, string>
  >(
    Object.fromEntries(
      Object.entries(account?.modelContextSizes ?? {}).map(([id, value]) => [
        id,
        String(value),
      ]),
    ),
  );
  const [customModelInput, setCustomModelInput] = useState("");
  const [modelRefreshEmpty, setModelRefreshEmpty] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(true);
  const provider = providerForPreset(preset);
  const claudeDefaults = claudeRoleDefaultsFor(preset, selectedModels);
  const providerWebsite =
    preset === "custom" ? undefined : providerWebsites[preset];
  const initialPreset = presetForEndpoint(account?.endpoints?.[0]);
  const keyRequired = !account?.hasCredential || preset !== initialPreset;
  const credentialQuery = useQuery({
    queryKey: ["ai-account-credential", account?.id],
    queryFn: async ({ signal }) => {
      const { data } = await getAiAccountCredential(
        withLocalErrorSurface({
          path: { accountId: account?.id ?? "" },
          signal,
          throwOnError: true,
        }),
      );
      return data?.apiKey ?? "";
    },
    enabled: Boolean(
      account?.id &&
      account.hasCredential &&
      authMode === "api-key" &&
      !keyRequired,
    ),
    retry: false,
    gcTime: 0,
  });
  const savedApiKey = credentialQuery.data ?? "";
  const displayedApiKey = !keyRequired && !keyEdited ? savedApiKey : apiKey;
  const apiKeyChanged = Boolean(
    displayedApiKey.trim() && displayedApiKey.trim() !== savedApiKey,
  );
  const keyReadError = credentialQuery.error
    ? credentialQuery.error instanceof Error
      ? credentialQuery.error.message
      : t("common:unknownError")
    : "";
  const accessMode =
    authMode === "subscription"
      ? preset === "kimi"
        ? "kimi-subscription"
        : "chatgpt-subscription"
      : preset === "kimi"
        ? "kimi-plan"
        : preset === "volcengineCoding"
          ? "volcengine-coding"
          : preset === "volcengineAgent"
            ? "volcengine-agent"
            : "api-key";
  const authSelectValue =
    accessMode === "kimi-plan" ||
    accessMode === "volcengine-coding" ||
    accessMode === "volcengine-agent"
      ? "key-subscription"
      : accessMode;
  const chooseKeyPreset = (nextPreset: AccountPresetID) => {
    const nextEndpoint = accountPresets[nextPreset][0];
    if (!nextEndpoint) return;
    setPreset(nextPreset);
    setEndpoint({ ...nextEndpoint });
    setApiKey("");
    setKeyEdited(false);
    setDiscoveredModels([]);
    setSelectedModels([]);
    setCustomModels([]);
    setModelDisplayNames({});
    setModelReasoningLevels({});
    setClaudeModelRoles({});
    setCustomModelInput("");
    setModelRefreshEmpty(false);
  };
  const receiveModels = (resources: AIAccountModel[]) => {
    setModelRefreshEmpty(resources.length === 0);
    if (resources.length === 0) return;
    setDiscoveredModels(resources);
  };
  const refreshModels = useMutation({
    ...withLocalErrorMutation(refreshAiAccountModelsMutation()),
    onSuccess: (data) => receiveModels(data?.resources ?? []),
  });
  const previewModels = useMutation({
    ...withLocalErrorMutation(previewAiAccountModelsMutation()),
    onSuccess: (data) => receiveModels(data?.resources ?? []),
  });
  const create = useMutation(withLocalErrorMutation(createAiAccountMutation()));
  const update = useMutation(withLocalErrorMutation(updateAiAccountMutation()));
  const pending = create.isPending || update.isPending;
  const error =
    create.error ?? update.error ?? refreshModels.error ?? previewModels.error;
  const nameConflict = accounts.some(
    (item) => item.id !== account?.id && item.name === name.trim(),
  );
  const save = async () => {
    if (nameConflict) return;
    const contextSizes = Object.fromEntries(
      selectedModels.map((id) => [
        id,
        modelContextSizes[id] ??
          modelDefaultsFor(preset, id)?.contextWindow ??
          262144,
      ]),
    );
    for (const [modelID, raw] of Object.entries(modelContextInputs)) {
      const value = Number(raw);
      if (Number.isSafeInteger(value) && value >= 1 && value <= 4000000) {
        contextSizes[modelID] = value;
      }
    }
    const body = {
      name: name.trim(),
      endpoints: [{ protocol: endpoint.protocol, baseUrl: endpoint.baseUrl }],
      modelCatalog: discoveredModels
        .map((item) => item.id ?? "")
        .filter(Boolean),
      selectedModels,
      modelContextSizes: contextSizes,
      modelDisplayNames: Object.fromEntries(
        selectedModels
          .filter((id) =>
            (
              modelDisplayNames[id] ??
              (endpoint.protocol === "openai-responses"
                ? modelDefaultsFor(preset, id)?.displayName
                : "")
            )?.trim(),
          )
          .map((id) => [
            id,
            (
              modelDisplayNames[id] ??
              (endpoint.protocol === "openai-responses"
                ? modelDefaultsFor(preset, id)?.displayName
                : "") ??
              ""
            ).trim(),
          ]),
      ),
      modelReasoningLevels: Object.fromEntries(
        selectedModels
          .filter(
            (id) =>
              (
                modelReasoningLevels[id] ??
                (endpoint.protocol === "openai-responses"
                  ? modelDefaultsFor(preset, id)?.reasoningLevels
                  : undefined) ??
                (endpoint.protocol === "openai-responses"
                  ? defaultResponseReasoningLevels
                  : undefined)
              )?.length,
          )
          .map((id) => [
            id,
            modelReasoningLevels[id] ??
              (endpoint.protocol === "openai-responses"
                ? modelDefaultsFor(preset, id)?.reasoningLevels
                : undefined) ??
              (endpoint.protocol === "openai-responses"
                ? defaultResponseReasoningLevels
                : undefined) ??
              [],
          ]),
      ),
      claudeModelRoles: Object.fromEntries(
        Object.entries({
          ...(endpoint.protocol === "anthropic-messages" ? claudeDefaults : {}),
          ...claudeModelRoles,
        }).filter(([, id]) => selectedModels.includes(id)),
      ),
      apiKey: apiKeyChanged ? displayedApiKey.trim() : "",
    };
    if (account?.id) {
      await update.mutateAsync({
        path: { accountId: account.id },
        body: {
          ...body,
          customModels,
          defaultModel: selectedModels.includes(account?.defaultModel ?? "")
            ? (account?.defaultModel ?? "")
            : (selectedModels[0] ?? ""),
          defaultReasoning: account?.defaultReasoning ?? "",
          revision: account.revision ?? 0,
        },
      });
      onSaved();
    } else {
      const created = await create.mutateAsync({
        body: {
          ...body,
          authMode,
          defaultModel: selectedModels[0] ?? "",
          ...(customModels.length ? { customModels } : {}),
        },
      });
      onSaved(authMode === "subscription" ? created.id : undefined);
    }
  };
  const availableModels = [
    ...discoveredModels,
    ...[...new Set([...customModels, ...selectedModels])]
      .filter((id) => !discoveredModels.some((item) => item.id === id))
      .map((id) => ({ id, name: id })),
  ];
  const modelOptions = availableModels
    .filter((model) => model.id)
    .map((model) => ({
      value: model.id ?? "",
      label: model.name || model.id || "",
    }));
  const addCustomModel = () => {
    const modelID = customModelInput.trim();
    if (!modelID || customModels.includes(modelID)) return;
    if (modelID.length > 200) return;
    setCustomModels((current) => [...current, modelID]);
    setSelectedModels((current) => [...current, modelID]);
    setCustomModelInput("");
  };
  const contextInput = (modelID: string) => (
    <Input
      size="sm"
      type="number"
      min={1}
      max={4000000}
      className="w-full min-w-0"
      aria-label={t("modelContextSizeFor", { model: modelID })}
      value={
        modelContextInputs[modelID] ??
        String(
          modelContextSizes[modelID] ??
            modelDefaultsFor(preset, modelID)?.contextWindow ??
            262144,
        )
      }
      onChange={(event) =>
        setModelContextInputs((current) => ({
          ...current,
          [modelID]: event.target.value,
        }))
      }
      onBlur={() => {
        const value = Number(modelContextInputs[modelID]);
        if (Number.isSafeInteger(value) && value >= 1 && value <= 4000000) {
          setModelContextSizes((current) => ({
            ...current,
            [modelID]: value,
          }));
        } else {
          setModelContextInputs((current) => ({
            ...current,
            [modelID]: String(
              modelContextSizes[modelID] ??
                modelDefaultsFor(preset, modelID)?.contextWindow ??
                262144,
            ),
          }));
        }
      }}
    />
  );
  const reasoningInput = (modelID: string) => {
    const selected =
      modelReasoningLevels[modelID] ??
      (endpoint.protocol === "openai-responses"
        ? modelDefaultsFor(preset, modelID)?.reasoningLevels
        : undefined) ??
      (endpoint.protocol === "openai-responses"
        ? defaultResponseReasoningLevels
        : undefined) ??
      [];
    return (
      <FieldCheckboxDropdown
        ariaLabel={t("thinkingLevelsFor", { model: modelID })}
        options={[...new Set([...reasoningLevels, ...selected])]}
        value={selected}
        triggerLabel={selected.join(", ") || t("nativeDefault")}
        onValueChange={(next) =>
          setModelReasoningLevels((current) => ({
            ...current,
            [modelID]: next,
          }))
        }
      />
    );
  };
  return (
    <DialogContent
      title={t(account ? "edit" : "add")}
      size="lg"
      layout="form"
      aria-describedby={undefined}
    >
      <form
        className="flex min-h-0 flex-1 flex-col overflow-hidden"
        onSubmit={(event) => {
          event.preventDefault();
          void save().catch(() => undefined);
        }}
      >
        <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-6 pb-5">
          <SettingsFieldGroup>
            <SettingsFieldRow label={t("name")} htmlFor={`${formId}-name`}>
              <Input
                id={`${formId}-name`}
                autoFocus
                size="sm"
                value={name}
                onChange={(event) => {
                  setName(event.target.value);
                  setNameEdited(true);
                }}
                placeholder={t("namePlaceholder")}
                maxLength={120}
                required
                aria-invalid={nameConflict || undefined}
                aria-describedby={
                  nameConflict ? `${formId}-name-error` : undefined
                }
                autoComplete="off"
              />
              {nameConflict ? (
                <p
                  id={`${formId}-name-error`}
                  role="alert"
                  className="type-ui text-destructive mt-2"
                >
                  {t("nameConflict")}
                </p>
              ) : null}
            </SettingsFieldRow>
            {authMode === "api-key" ? (
              <SettingsFieldRow
                label={t("preset")}
                htmlFor={`${formId}-preset`}
              >
                <div className="flex min-w-0 items-center gap-2">
                  <Select
                    value={provider}
                    onValueChange={(value) => {
                      const nextProvider = accountProviderIDs.find(
                        (id) => id === value,
                      );
                      if (!nextProvider) return;
                      const nextPreset =
                        nextProvider === "kimi" ? "moonshot" : nextProvider;
                      if (nextProvider === provider) return;
                      chooseKeyPreset(nextPreset);
                      if (!nameEdited) setName(t(`presets.${nextProvider}`));
                    }}
                  >
                    <SelectTrigger
                      id={`${formId}-preset`}
                      size="sm"
                      className="min-w-0 flex-1"
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {accountProviderIDs.map((id) => (
                        <SelectItem key={id} value={id}>
                          {t(`presets.${id}`)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {providerWebsite ? (
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="ghost"
                      aria-label={t("openProviderWebsite", {
                        provider: t(`presets.${provider}`),
                      })}
                      title={t("openProviderWebsite", {
                        provider: t(`presets.${provider}`),
                      })}
                      onClick={() => void openExternalURL(providerWebsite)}
                    >
                      <ExternalLink />
                    </Button>
                  ) : null}
                </div>
              </SettingsFieldRow>
            ) : null}
            <SettingsFieldRow
              label={t("authMode")}
              htmlFor={
                (!account && provider === "openai") ||
                provider === "kimi" ||
                provider === "volcengine"
                  ? `${formId}-auth`
                  : undefined
              }
            >
              {(account && authMode === "subscription") ||
              (provider !== "openai" &&
                provider !== "kimi" &&
                provider !== "volcengine") ? (
                <p className="type-ui text-content-secondary">
                  {t(
                    authMode === "subscription"
                      ? preset === "kimi"
                        ? "kimiSubscriptionMode"
                        : "subscriptionMode"
                      : "apiKeyMode",
                  )}
                </p>
              ) : (
                <div className="flex min-w-0 items-center gap-2">
                  <Select
                    value={authSelectValue}
                    onValueChange={(value) => {
                      if (value === authSelectValue) return;
                      if (provider === "openai") {
                        const next =
                          value === "chatgpt-subscription"
                            ? "subscription"
                            : "api-key";
                        setAuthMode(next);
                        setEndpoint({
                          protocol: "openai-responses",
                          baseUrl: officialEndpoints["openai-responses"],
                        });
                        if (!nameEdited)
                          setName(
                            t(
                              next === "subscription"
                                ? "subscriptionMode"
                                : "presets.openai",
                            ),
                          );
                        setApiKey("");
                        setKeyEdited(false);
                        setDiscoveredModels([]);
                        setSelectedModels([]);
                        setCustomModels([]);
                        setCustomModelInput("");
                      } else {
                        if (
                          provider === "kimi" &&
                          value === "kimi-subscription"
                        ) {
                          setAuthMode("subscription");
                          setPreset("kimi");
                          setEndpoint({
                            protocol: "openai-completions",
                            baseUrl: "https://api.kimi.com/coding/v1",
                          });
                          if (!nameEdited) setName(t("kimiSubscriptionMode"));
                          setApiKey("");
                          setKeyEdited(false);
                          setDiscoveredModels([]);
                          setSelectedModels([]);
                          setCustomModels([]);
                        } else {
                          setAuthMode("api-key");
                          chooseKeyPreset(
                            provider === "kimi"
                              ? value === "key-subscription"
                                ? "kimi"
                                : "moonshot"
                              : value === "key-subscription"
                                ? "volcengineCoding"
                                : "volcengine",
                          );
                        }
                      }
                    }}
                  >
                    <SelectTrigger
                      id={`${formId}-auth`}
                      size="sm"
                      aria-label={t("authMode")}
                      className="min-w-0 flex-1"
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="api-key">{t("apiKeyMode")}</SelectItem>
                      {provider === "openai" && !account ? (
                        <SelectItem value="chatgpt-subscription">
                          {t("subscriptionMode")}
                        </SelectItem>
                      ) : null}
                      {provider === "kimi" ? (
                        <>
                          <SelectItem value="key-subscription">
                            {t("keySubscriptionMode")}
                          </SelectItem>
                          {!account ? (
                            <SelectItem value="kimi-subscription">
                              {t("kimiSubscriptionMode")}
                            </SelectItem>
                          ) : null}
                        </>
                      ) : null}
                      {provider === "volcengine" ? (
                        <SelectItem value="key-subscription">
                          {t("keySubscriptionMode")}
                        </SelectItem>
                      ) : null}
                    </SelectContent>
                  </Select>
                  {provider === "volcengine" &&
                  authSelectValue === "key-subscription" ? (
                    <Select
                      value={preset}
                      onValueChange={(value) => {
                        if (
                          value !== "volcengineCoding" &&
                          value !== "volcengineAgent"
                        )
                          return;
                        chooseKeyPreset(value);
                      }}
                    >
                      <SelectTrigger
                        size="sm"
                        aria-label={t("planType")}
                        className="min-w-0 flex-1"
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="volcengineCoding">
                          {t("volcengineCodingPlan")}
                        </SelectItem>
                        <SelectItem value="volcengineAgent">
                          {t("volcengineAgentPlan")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  ) : null}
                </div>
              )}
            </SettingsFieldRow>
            {authMode === "api-key" ? (
              <>
                <SettingsFieldRow label={t("key")} htmlFor={`${formId}-key`}>
                  <div className="flex min-w-0 items-center gap-2">
                    <Input
                      id={`${formId}-key`}
                      size="sm"
                      type={keyVisible ? "text" : "password"}
                      value={displayedApiKey}
                      onChange={(event) => {
                        setKeyEdited(true);
                        setApiKey(event.target.value);
                      }}
                      required={keyRequired}
                      autoComplete="new-password"
                      placeholder={
                        account?.hasCredential && !keyRequired
                          ? t("keepKey")
                          : t("keyPlaceholder")
                      }
                    />
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="ghost"
                      className="shrink-0"
                      aria-label={t(keyVisible ? "hideKey" : "showKey")}
                      onClick={() => setKeyVisible((current) => !current)}
                    >
                      {keyVisible ? <EyeOff /> : <Eye />}
                    </Button>
                  </div>
                  {keyReadError ? (
                    <p
                      role="alert"
                      className="type-caption text-destructive mt-1"
                    >
                      {keyReadError}
                    </p>
                  ) : null}
                </SettingsFieldRow>
              </>
            ) : null}
            {authMode === "api-key" ? (
              <SettingsFieldRow
                label={t("requestProtocol")}
                htmlFor={`${formId}-protocol`}
              >
                <Select
                  value={endpoint.protocol}
                  onValueChange={(value) => {
                    const protocol = value as Endpoint["protocol"];
                    const presetEndpoint = accountPresets[preset].find(
                      (item) => item.protocol === protocol,
                    );
                    setEndpoint({
                      protocol,
                      baseUrl:
                        preset === "custom"
                          ? endpoint.baseUrl
                          : (presetEndpoint?.baseUrl ?? endpoint.baseUrl),
                    });
                    setDiscoveredModels([]);
                    setSelectedModels([]);
                    setCustomModels([]);
                  }}
                >
                  <SelectTrigger id={`${formId}-protocol`} size="sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(preset === "custom"
                      ? protocols
                      : accountPresets[preset].map((item) => item.protocol)
                    ).map((protocol) => (
                      <SelectItem key={protocol} value={protocol}>
                        {t(`protocol.${protocol}`)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </SettingsFieldRow>
            ) : null}
            {authMode === "api-key" ? (
              <>
                <SettingsFieldRow
                  label={t("apiAddress")}
                  htmlFor={`${formId}-url`}
                >
                  <Input
                    id={`${formId}-url`}
                    size="sm"
                    type="url"
                    value={endpoint.baseUrl}
                    required
                    placeholder="https://api.example.com/v1"
                    onChange={(event) =>
                      setEndpoint({
                        ...endpoint,
                        baseUrl: event.target.value,
                      })
                    }
                  />
                </SettingsFieldRow>
                <div className="flex min-w-0 flex-col gap-3 px-4 py-3">
                  <p className="type-ui text-content-primary font-medium">
                    {t("modelList")}
                  </p>
                  <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-2 gap-y-3">
                    <SearchableMultiSelect
                      options={modelOptions}
                      value={selectedModels}
                      onValueChange={setSelectedModels}
                      triggerLabel={
                        modelOptions.length
                          ? t("modelCountSummary", {
                              total: modelOptions.length,
                              selected: selectedModels.length,
                            })
                          : t("selectModels")
                      }
                      searchLabel={t("searchModels")}
                      emptyLabel={t("noMatchingModels")}
                      clearSearchLabel={t("clearSearch")}
                      clearSelectionLabel={t("clearSelectedModels")}
                    />
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      className="w-full justify-start"
                      disabled={
                        refreshModels.isPending ||
                        previewModels.isPending ||
                        (keyRequired && !displayedApiKey.trim())
                      }
                      onClick={() =>
                        account?.id &&
                        !apiKeyChanged &&
                        account.endpoints?.[0]?.protocol ===
                          endpoint.protocol &&
                        account.endpoints?.[0]?.baseUrl === endpoint.baseUrl
                          ? refreshModels.mutate({
                              path: { accountId: account.id },
                            })
                          : previewModels.mutate({
                              body: {
                                endpoint,
                                apiKey: apiKeyChanged
                                  ? displayedApiKey.trim()
                                  : "",
                                accountId: account?.id,
                              },
                            })
                      }
                    >
                      <RefreshCw
                        className={
                          refreshModels.isPending || previewModels.isPending
                            ? "animate-spin"
                            : undefined
                        }
                      />
                      {t("refreshModels")}
                    </Button>
                    <Input
                      size="sm"
                      className="min-w-0"
                      aria-label={t("customModelInput")}
                      value={customModelInput}
                      placeholder={t("customModelPlaceholder")}
                      onChange={(event) =>
                        setCustomModelInput(event.target.value)
                      }
                      onKeyDown={(event) => {
                        if (event.key !== "Enter") return;
                        event.preventDefault();
                        addCustomModel();
                      }}
                    />
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      className="w-full justify-start"
                      disabled={!customModelInput.trim()}
                      onClick={addCustomModel}
                    >
                      <Plus />
                      {t("addCustomModel")}
                    </Button>
                  </div>
                  {modelRefreshEmpty ? (
                    <p className="type-caption text-content-secondary">
                      {t("emptyModelRefresh")}
                    </p>
                  ) : null}
                  {selectedModels.length ? (
                    <div className="flex flex-col gap-2">
                      {selectedModels.map((modelID) => (
                        <div
                          key={modelID}
                          className="type-ui border-border-subtle bg-surface-elevated flex min-w-0 items-center gap-2 rounded-md border px-3 py-1.5"
                        >
                          <span
                            className="min-w-0 flex-1 truncate"
                            title={modelID}
                          >
                            {modelDisplayNames[modelID] ||
                              (endpoint.protocol === "openai-responses"
                                ? modelDefaultsFor(preset, modelID)?.displayName
                                : undefined) ||
                              modelID}
                          </span>
                          <Button
                            type="button"
                            size="icon-sm"
                            variant="ghost"
                            aria-label={t("removeCustomModel", {
                              model: modelID,
                            })}
                            onClick={() => {
                              setSelectedModels((current) =>
                                current.filter((item) => item !== modelID),
                              );
                              setCustomModels((current) =>
                                current.filter((item) => item !== modelID),
                              );
                            }}
                          >
                            <X />
                          </Button>
                        </div>
                      ))}
                    </div>
                  ) : null}
                </div>
              </>
            ) : null}
          </SettingsFieldGroup>
          {authMode === "api-key" && selectedModels.length ? (
            <Collapsible
              className="bg-surface-tertiary @container min-w-0 overflow-hidden rounded-lg"
              open={advancedOpen}
              onOpenChange={setAdvancedOpen}
            >
              <CollapsibleTrigger className="group type-ui text-content-secondary hover:text-content-primary flex w-full items-center gap-1 px-4 py-3 text-left">
                <ChevronRight
                  aria-hidden="true"
                  className="size-icon-sm group-data-[state=open]:rotate-90"
                />
                {t("advanced")}
              </CollapsibleTrigger>
              <CollapsibleContent className="flex flex-col gap-3 px-4 pb-4">
                {endpoint.protocol === "anthropic-messages" ? (
                  <>
                    <div className="flex items-center justify-between gap-2">
                      <p className="type-ui font-medium">{t("modelMapping")}</p>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        onClick={() =>
                          setClaudeModelRoles(
                            Object.fromEntries(
                              claudeRoles.map((role) => [
                                role,
                                selectedModels[0] ?? "",
                              ]),
                            ),
                          )
                        }
                      >
                        {t("applyModelToRoles")}
                      </Button>
                    </div>
                    <div className="type-caption text-content-secondary grid grid-cols-[4.5rem_minmax(0,1fr)_minmax(0,1fr)] gap-2 px-1">
                      <span>{t("modelRole")}</span>
                      <span>{t("modelDisplayName")}</span>
                      <span>{t("actualModel")}</span>
                    </div>
                    {claudeRoles.map((role) => {
                      const defaultModel = claudeDefaults[role];
                      const mappedModel =
                        claudeModelRoles[role] ?? defaultModel ?? "";
                      const actualModel = selectedModels.includes(mappedModel)
                        ? mappedModel
                        : "";
                      return (
                        <div
                          key={role}
                          className="grid min-w-0 grid-cols-[4.5rem_minmax(0,1fr)_minmax(0,1fr)] items-center gap-2"
                        >
                          <span className="type-ui truncate">
                            {t(`claudeRole.${role}`)}
                          </span>
                          <Input
                            size="sm"
                            className="min-w-0"
                            disabled={!actualModel}
                            value={
                              actualModel
                                ? (modelDisplayNames[actualModel] ?? "")
                                : ""
                            }
                            placeholder={actualModel || t("followChatModel")}
                            aria-label={t("claudeRoleDisplayNameFor", {
                              role: t(`claudeRole.${role}`),
                            })}
                            onChange={(event) =>
                              setModelDisplayNames((current) => ({
                                ...current,
                                [actualModel]: event.target.value,
                              }))
                            }
                          />
                          <Select
                            value={actualModel || followChatModelValue}
                            onValueChange={(value) =>
                              setClaudeModelRoles((current) => ({
                                ...current,
                                [role]: value,
                              }))
                            }
                          >
                            <SelectTrigger
                              size="sm"
                              className="min-w-0"
                              aria-label={t("claudeRoleModelFor", {
                                role: t(`claudeRole.${role}`),
                              })}
                            >
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value={followChatModelValue}>
                                {t("followChatModel")}
                              </SelectItem>
                              {selectedModels.map((id) => (
                                <SelectItem key={id} value={id}>
                                  {id}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                        </div>
                      );
                    })}
                    <div className="border-border-subtle flex flex-col gap-2 border-t pt-3">
                      <div className="type-caption text-content-secondary grid grid-cols-[minmax(0,1fr)_6.5rem_7rem] items-center gap-2 px-1">
                        <span>{t("actualModel")}</span>
                        <span>{t("contextWindow")}</span>
                        <span>{t("thinkingLevels")}</span>
                      </div>
                      {selectedModels.map((modelID) => (
                        <div
                          key={modelID}
                          className="grid grid-cols-[minmax(0,1fr)_6.5rem_7rem] items-center gap-2"
                        >
                          <span className="type-ui truncate" title={modelID}>
                            {modelID}
                          </span>
                          {contextInput(modelID)}
                          {reasoningInput(modelID)}
                        </div>
                      ))}
                    </div>
                  </>
                ) : (
                  <>
                    <p className="type-ui font-medium">
                      {t(
                        endpoint.protocol === "openai-responses"
                          ? "modelMapping"
                          : "modelSettings",
                      )}
                    </p>
                    {selectedModels.map((modelID) => (
                      <div
                        key={modelID}
                        className="border-border-subtle bg-surface-elevated flex min-w-0 flex-col gap-2 rounded-md border p-3"
                      >
                        <div className="type-ui flex min-w-0 flex-wrap gap-x-2">
                          <span className="text-content-secondary">
                            {t("actualModel")}
                          </span>
                          <span className="min-w-0 break-all" title={modelID}>
                            {modelID}
                          </span>
                        </div>
                        <div
                          className={
                            endpoint.protocol === "openai-responses"
                              ? "grid min-w-0 grid-cols-1 gap-2 @[32rem]:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)_minmax(0,1fr)]"
                              : "grid min-w-0 grid-cols-1 gap-2 @[24rem]:grid-cols-2"
                          }
                        >
                          {endpoint.protocol === "openai-responses" ? (
                            <div className="min-w-0 space-y-1">
                              <span className="type-caption text-content-secondary">
                                {t("modelDisplayName")}
                              </span>
                              <Input
                                size="sm"
                                className="min-w-0"
                                value={
                                  modelDisplayNames[modelID] ??
                                  modelDefaultsFor(preset, modelID)
                                    ?.displayName ??
                                  ""
                                }
                                placeholder={modelID}
                                aria-label={t("modelDisplayNameFor", {
                                  model: modelID,
                                })}
                                maxLength={200}
                                onChange={(event) =>
                                  setModelDisplayNames((current) => ({
                                    ...current,
                                    [modelID]: event.target.value,
                                  }))
                                }
                              />
                            </div>
                          ) : null}
                          <div className="type-caption text-content-secondary grid min-w-0 gap-1.5">
                            <span>{t("contextWindow")}</span>
                            {contextInput(modelID)}
                          </div>
                          <div className="type-caption text-content-secondary grid min-w-0 gap-1.5">
                            <span>{t("thinkingLevels")}</span>
                            {reasoningInput(modelID)}
                          </div>
                        </div>
                      </div>
                    ))}
                  </>
                )}
              </CollapsibleContent>
            </Collapsible>
          ) : null}
          {authMode === "subscription" ? (
            <p className="type-ui text-content-secondary">
              {t("subscriptionDescription")}
            </p>
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
            type="button"
            variant="secondary"
            disabled={pending}
            onClick={onClose}
          >
            {t("common:cancel")}
          </Button>
          <Button
            type="submit"
            disabled={
              pending ||
              nameConflict ||
              !name.trim() ||
              !endpoint.baseUrl.trim()
            }
          >
            {t("common:save")}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  );
}

function AccountRow({
  account,
  onEdit,
  onDelete,
  onLogin,
}: {
  account: AIAccount;
  onEdit: () => void;
  onDelete: () => void;
  onLogin: () => void;
}) {
  const { t } = useTranslation("aiAccounts");
  const [expanded, setExpanded] = useState(false);
  const summary = account.endpoints?.[0]
    ? t(`protocol.${account.endpoints[0].protocol}`)
    : "";
  return (
    <Collapsible
      open={expanded}
      onOpenChange={setExpanded}
      className="bg-surface-tertiary overflow-hidden rounded-lg"
    >
      <div className="min-h-row-rich flex items-center gap-2 pr-3">
        <CollapsibleTrigger
          aria-label={t(expanded ? "collapseNamed" : "expandNamed", {
            name: account.name,
          })}
          className="group text-content-primary hover:bg-action-quiet-hover focus-visible:ring-ring flex min-w-0 flex-1 items-center gap-2 self-stretch px-4 py-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset"
        >
          <ChevronRight
            aria-hidden="true"
            className="size-icon-sm text-content-muted shrink-0 group-data-[state=open]:rotate-90"
          />
          <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span className="type-body font-medium break-words">
              {account.name}
            </span>
            <span
              className="type-ui text-content-secondary min-w-0 truncate"
              title={summary}
            >
              {summary}
            </span>
          </span>
        </CollapsibleTrigger>
        <div className="flex shrink-0 items-center gap-1">
          {account.authMode === "subscription" && !account.hasCredential ? (
            <Button size="sm" variant="ghost" onClick={onLogin}>
              {t("login")}
            </Button>
          ) : null}
          <div className="p-1">
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label={t("editNamed", { name: account.name })}
              title={t("edit")}
              onClick={onEdit}
            >
              <Pencil />
            </Button>
          </div>
          <div className="p-1">
            <Button
              size="icon-sm"
              variant="ghost"
              className="text-destructive hover:text-destructive"
              aria-label={t("deleteNamed", { name: account.name })}
              title={t("deleteNamed", { name: account.name })}
              onClick={onDelete}
            >
              <Trash2 />
            </Button>
          </div>
        </div>
      </div>
      <CollapsibleContent>
        <div className="border-border-subtle @container border-t">
          <SettingsFieldRow label={t("authMode")}>
            <div className="type-ui text-content-secondary flex flex-wrap items-center justify-between gap-2">
              <span>
                {t(
                  account.authMode === "subscription"
                    ? account.endpoints?.[0]?.baseUrl ===
                      "https://api.kimi.com/coding/v1"
                      ? "kimiSubscriptionMode"
                      : "subscriptionMode"
                    : "apiKeyMode",
                )}{" "}
                · {t(account.hasCredential ? "configured" : "needsLogin")}
              </span>
              {account.authMode === "subscription" ? (
                <Button size="sm" variant="ghost" onClick={onLogin}>
                  {t(account.hasCredential ? "reauth" : "login")}
                </Button>
              ) : null}
            </div>
          </SettingsFieldRow>
          <dl className="border-border-subtle divide-border-subtle divide-y border-t">
            {account.endpoints?.slice(0, 1).map((endpoint) => (
              <SettingsFieldRow
                key={endpoint.protocol}
                label={<dt>{t(`protocol.${endpoint.protocol}`)}</dt>}
              >
                <dd className="type-ui text-content-secondary break-all">
                  {endpoint.baseUrl}
                </dd>
              </SettingsFieldRow>
            ))}
          </dl>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

export function AIAccountsSection() {
  const { t } = useTranslation(["aiAccounts", "common"]);
  const queryClient = useQueryClient();
  const accounts = useQuery(listAiAccountsOptions(withLocalErrorSurface({})));
  const remove = useMutation(withLocalErrorMutation(deleteAiAccountMutation()));
  const [search, setSearch] = useState("");
  const [editing, setEditing] = useState<AIAccount | "new">();
  const [deleting, setDeleting] = useState<AIAccount>();
  const [loginAccount, setLoginAccount] = useState<string>();
  const filtered = (accounts.data?.resources ?? []).filter((account) =>
    `${account.name} ${account.endpoints?.[0]?.baseUrl ?? ""}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  return (
    <SettingsContentShell
      title={t("title")}
      description={t("description")}
      actions={
        <Button onClick={() => setEditing("new")}>
          <Plus />
          {t("add")}
        </Button>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="type-ui text-content-secondary">
            {t("accountCount", {
              count: accounts.data?.resources?.length ?? 0,
            })}
          </p>
          <SearchField
            width="toolbar"
            aria-label={t("search")}
            placeholder={t("search")}
            clearLabel={t("clearSearch")}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </div>
        {accounts.isError ? (
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p role="alert" className="type-body text-destructive">
              {accounts.error.message}
            </p>
            <Button
              size="sm"
              variant="secondary"
              disabled={accounts.isFetching}
              onClick={() => void accounts.refetch()}
            >
              {t("common:retry")}
            </Button>
          </div>
        ) : null}
        {accounts.isPending ? (
          <p role="status" className="type-body text-content-secondary">
            {t("common:pageLoading")}
          </p>
        ) : !accounts.data ? null : filtered.length === 0 ? (
          <p className="type-body text-content-secondary py-6">
            {t(search ? "noMatches" : "empty")}
          </p>
        ) : (
          <div className="flex flex-col gap-3">
            {filtered.map((account) => (
              <AccountRow
                key={account.id}
                account={account}
                onEdit={() => setEditing(account)}
                onDelete={() => {
                  remove.reset();
                  setDeleting(account);
                }}
                onLogin={() => setLoginAccount(account.id)}
              />
            ))}
          </div>
        )}
      </div>
      <Dialog
        open={Boolean(editing)}
        onOpenChange={(open) => {
          if (!open) setEditing(undefined);
        }}
      >
        {editing ? (
          <AccountForm
            key={editing === "new" ? "new" : editing.id}
            account={editing === "new" ? undefined : editing}
            accounts={accounts.data?.resources ?? []}
            onClose={() => setEditing(undefined)}
            onSaved={(id) => {
              setEditing(undefined);
              void queryClient.invalidateQueries({
                queryKey: listAiAccountsQueryKey(),
              });
              if (id) setLoginAccount(id);
            }}
          />
        ) : null}
      </Dialog>
      <Dialog
        open={Boolean(deleting)}
        onOpenChange={(open) => {
          if (!open) setDeleting(undefined);
        }}
      >
        {deleting ? (
          <DialogContent
            title={t("deleteNamed", { name: deleting.name })}
            description={t("deleteDescription")}
          >
            {remove.error ? (
              <p role="alert" className="type-body text-destructive px-6 pb-4">
                {remove.error.message}
              </p>
            ) : null}
            <DialogFooter>
              <Button
                variant="secondary"
                disabled={remove.isPending}
                onClick={() => setDeleting(undefined)}
              >
                {t("common:cancel")}
              </Button>
              <Button
                variant="danger"
                disabled={remove.isPending}
                onClick={() =>
                  void remove
                    .mutateAsync({
                      path: { accountId: deleting.id ?? "" },
                      query: { revision: deleting.revision ?? 0 },
                    })
                    .then(() => {
                      setDeleting(undefined);
                      return queryClient.invalidateQueries({
                        queryKey: listAiAccountsQueryKey(),
                      });
                    })
                    .catch(() => undefined)
                }
              >
                {t("common:delete")}
              </Button>
            </DialogFooter>
          </DialogContent>
        ) : null}
      </Dialog>
      {loginAccount ? (
        <AccountLogin
          accountId={loginAccount}
          onClose={() => setLoginAccount(undefined)}
        />
      ) : null}
    </SettingsContentShell>
  );
}
