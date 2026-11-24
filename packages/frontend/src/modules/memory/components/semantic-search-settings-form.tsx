import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { CheckCircle2, RefreshCw } from "lucide-react";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  getMemorySettingsOptions,
  getMemoryStatusOptions,
  invalidateMemoryQueries,
  reindexMemoryIndexMutation,
  testMemoryEmbeddingMutation,
  updateMemorySettingsMutation,
} from "../api/memory-queries";
import type { MemoryStatusResource } from "../model/types";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Skeleton } from "@/shared/ui/skeleton";

type SettingsSnapshot = {
  apiKeyConfigured: boolean;
  baseUrl: string;
  model: string;
};

export function SemanticSearchSettingsForm({
  onClose,
}: {
  onClose: () => void;
}) {
  const { t } = useTranslation("memory");
  const settings = useQuery(
    getMemorySettingsOptions(withLocalErrorSurface({})),
  );
  const status = useQuery(getMemoryStatusOptions(withLocalErrorSurface({})));

  if (!settings.data) {
    return (
      <div className="min-w-0 space-y-4">
        {settings.isError ? (
          <p role="alert" className="text-destructive type-body">
            {t("settings.loadError")}
          </p>
        ) : (
          <div
            className="@container grid gap-3"
            role="status"
            aria-busy="true"
            aria-label={t("settings.loading")}
          >
            <Skeleton className="h-control-md w-full" />
            <div className="grid gap-3 @sm:grid-cols-2">
              <Skeleton className="h-control-md w-full" />
              <Skeleton className="h-control-md w-full" />
            </div>
          </div>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          <Button
            size="settings"
            type="button"
            variant="settings-outline"
            onClick={onClose}
          >
            {t("common:cancel")}
          </Button>
          {settings.isError ? (
            <Button
              size="settings"
              type="button"
              onClick={() => void settings.refetch()}
            >
              {t("settings.retry")}
            </Button>
          ) : null}
        </div>
      </div>
    );
  }

  return (
    <EditableSemanticSearchSettings
      initial={{
        apiKeyConfigured: Boolean(settings.data.apiKeyConfigured),
        baseUrl: settings.data.baseUrl ?? "",
        model: settings.data.model ?? "",
      }}
      status={status.data}
      onClose={onClose}
    />
  );
}

function EditableSemanticSearchSettings({
  initial,
  onClose,
  status,
}: {
  initial: SettingsSnapshot;
  onClose: () => void;
  status?: MemoryStatusResource;
}) {
  const { t } = useTranslation("memory");
  const queryClient = useQueryClient();
  const descriptionId = useId();
  const baseURLId = useId();
  const apiKeyId = useId();
  const updateSettings = useMutation({
    ...withLocalErrorMutation(updateMemorySettingsMutation()),
    onSuccess: () => invalidateMemoryQueries(queryClient),
  });
  const testEmbedding = useMutation(
    withLocalErrorMutation(testMemoryEmbeddingMutation()),
  );
  const reindex = useMutation(
    withLocalErrorMutation(reindexMemoryIndexMutation()),
  );
  const [saved] = useState(initial);
  const [baseURL, setBaseURL] = useState(initial.baseUrl);
  const [model, setModel] = useState(initial.model);
  const [apiKey, setAPIKey] = useState("");
  const [clearAPIKey, setClearAPIKey] = useState(false);
  const [localError, setLocalError] = useState("");

  const normalizedBaseURL = baseURL.trim();
  const normalizedModel = model.trim();
  const dirty =
    normalizedBaseURL !== saved.baseUrl ||
    normalizedModel !== saved.model ||
    Boolean(apiKey) ||
    clearAPIKey;
  const savedComplete = Boolean(
    saved.baseUrl && saved.model && saved.apiKeyConfigured,
  );
  const showUpdateMemories =
    Boolean(status?.configured) &&
    Boolean(status?.needsReindex || status?.conflict);

  const save = () => {
    setLocalError("");
    if (normalizedBaseURL) {
      try {
        const parsed = new URL(normalizedBaseURL);
        if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
          throw new Error("invalid protocol");
        }
      } catch {
        setLocalError(t("settings.baseURLInvalid"));
        return;
      }
    }
    updateSettings.mutate(
      {
        body: {
          apiKey,
          baseUrl: normalizedBaseURL,
          clearApiKey: clearAPIKey,
          model: normalizedModel,
        },
      },
      {
        onSuccess: onClose,
        onError: (error) =>
          setLocalError(localApiErrorMessage(error, t("settings.saveFailed"))),
      },
    );
  };

  const testConnection = () => {
    setLocalError("");
    if (dirty) {
      setLocalError(t("settings.saveBeforeTest"));
      return;
    }
    testEmbedding.mutate({});
  };

  const updateExistingMemories = async () => {
    try {
      await reindex.mutateAsync({});
      await invalidateMemoryQueries(queryClient);
    } catch {
      // Mutation state renders the nearby error message.
    }
  };

  return (
    <form
      className="@container min-w-0 space-y-4"
      aria-label={t("review.semanticSearch")}
      aria-describedby={descriptionId}
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        if (dirty && !updateSettings.isPending) save();
      }}
    >
      <p id={descriptionId} className="text-content-secondary type-body">
        {t("settings.description")}
      </p>
      <div className="grid gap-x-4 gap-y-4 @sm:grid-cols-2">
        <div className="col-span-full grid gap-2">
          <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
            <label className="type-label font-medium" htmlFor={baseURLId}>
              {t("settings.baseURL")}
            </label>
            <Button
              disabled={
                testEmbedding.isPending ||
                updateSettings.isPending ||
                !savedComplete
              }
              size="settings"
              type="button"
              variant="settings-outline"
              onClick={testConnection}
            >
              {testEmbedding.isSuccess ? (
                <CheckCircle2 className="text-success" />
              ) : null}
              {testEmbedding.isPending
                ? t("settings.testing")
                : testEmbedding.isSuccess
                  ? t("settings.testSucceeded")
                  : t("settings.testConnection")}
            </Button>
          </div>
          <Input
            id={baseURLId}
            aria-describedby={`${baseURLId}-help`}
            placeholder={t("settings.baseURLPlaceholder")}
            disabled={updateSettings.isPending}
            size="md"
            type="url"
            value={baseURL}
            onChange={(event) => setBaseURL(event.target.value)}
          />
          <span
            id={`${baseURLId}-help`}
            className="text-muted-foreground type-caption"
          >
            {t("settings.baseURLHelp")}
          </span>
        </div>
        <label className="grid content-start gap-2">
          <span className="type-label font-medium">{t("settings.model")}</span>
          <Input
            placeholder={t("settings.modelPlaceholder")}
            disabled={updateSettings.isPending}
            size="md"
            value={model}
            onChange={(event) => setModel(event.target.value)}
          />
        </label>
        <div className="grid gap-2">
          <label className="type-label font-medium" htmlFor={apiKeyId}>
            {t("settings.apiKey")}
          </label>
          <div className="flex items-center gap-2">
            <Input
              id={apiKeyId}
              aria-describedby={`${apiKeyId}-help`}
              placeholder={
                clearAPIKey
                  ? t("settings.apiKeyWillClear")
                  : saved.apiKeyConfigured
                    ? t("settings.apiKeyKeep")
                    : t("settings.apiKeyPlaceholder")
              }
              disabled={updateSettings.isPending}
              size="md"
              type="password"
              value={apiKey}
              onChange={(event) => {
                setAPIKey(event.target.value);
                setClearAPIKey(false);
              }}
            />
            {saved.apiKeyConfigured ? (
              <Button
                disabled={updateSettings.isPending}
                size="settings"
                type="button"
                variant="ghost"
                onClick={() => {
                  setAPIKey("");
                  setClearAPIKey((value) => !value);
                }}
              >
                {clearAPIKey ? t("settings.undoClear") : t("settings.clear")}
              </Button>
            ) : null}
          </div>
          <span
            id={`${apiKeyId}-help`}
            className="text-muted-foreground type-caption"
          >
            {t("settings.apiKeyHelp")}
          </span>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <p className="text-muted-foreground type-caption">
          {t(
            dirty && status?.configured
              ? "settings.saveBeforeUpdate"
              : "settings.reindexHelp",
          )}
        </p>
        {showUpdateMemories ? (
          <Button
            disabled={reindex.isPending || dirty}
            size="settings"
            type="button"
            variant="settings-outline"
            onClick={() => void updateExistingMemories()}
          >
            <RefreshCw />
            {reindex.isPending
              ? t("settings.updatingMemories")
              : t("settings.updateMemories")}
          </Button>
        ) : null}
      </div>
      {reindex.isSuccess ? (
        <p className="text-success type-body">
          {t("settings.updateSucceeded")}
        </p>
      ) : null}
      {reindex.isError ? (
        <p role="alert" className="text-destructive type-body">
          {localApiErrorMessage(reindex.error, t("settings.updateFailed"))}
        </p>
      ) : null}
      {testEmbedding.isError ? (
        <p role="alert" className="text-destructive type-body">
          {localApiErrorMessage(testEmbedding.error, t("settings.testFailed"))}
        </p>
      ) : null}
      {localError ? (
        <p role="alert" className="text-destructive type-body">
          {localError}
        </p>
      ) : null}
      <div className="flex flex-wrap justify-end gap-2">
        <Button
          size="settings"
          type="button"
          variant="settings-outline"
          disabled={updateSettings.isPending}
          onClick={onClose}
        >
          {t("common:cancel")}
        </Button>
        <Button
          size="settings"
          type="submit"
          disabled={updateSettings.isPending || !dirty}
        >
          {updateSettings.isPending ? t("settings.saving") : t("settings.save")}
        </Button>
      </div>
    </form>
  );
}
