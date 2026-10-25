import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { CheckCircle2, RefreshCw } from "lucide-react";
import { useState } from "react";
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
import { MemoryWorkspaceDialogContent } from "./memory-workspace-dialog-content";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogClose, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";

type Props = {
  onOpenChange: (open: boolean) => void;
  open: boolean;
};

type SettingsSnapshot = {
  apiKeyConfigured: boolean;
  baseUrl: string;
  model: string;
};

export function MemorySettingsDialog({ onOpenChange, open }: Props) {
  const { t } = useTranslation("memory");
  const settings = useQuery({
    ...getMemorySettingsOptions(withLocalErrorSurface({})),
    enabled: open,
  });
  const status = useQuery({
    ...getMemoryStatusOptions(withLocalErrorSurface({})),
    enabled: open,
  });

  if (!open) return null;
  if (!settings.data) {
    return (
      <Dialog open onOpenChange={onOpenChange}>
        <MemoryWorkspaceDialogContent
          title={t("settings.title")}
          description={
            settings.isError ? t("settings.loadError") : t("settings.loading")
          }
        >
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="secondary">{t("settings.close")}</Button>
            </DialogClose>
            {settings.isError ? (
              <Button onClick={() => void settings.refetch()}>
                {t("settings.retry")}
              </Button>
            ) : null}
          </DialogFooter>
        </MemoryWorkspaceDialogContent>
      </Dialog>
    );
  }

  return (
    <OpenMemorySettingsDialog
      initial={{
        apiKeyConfigured: Boolean(settings.data.apiKeyConfigured),
        baseUrl: settings.data.baseUrl ?? "",
        model: settings.data.model ?? "",
      }}
      status={status.data}
      onOpenChange={onOpenChange}
    />
  );
}

function OpenMemorySettingsDialog({
  initial,
  onOpenChange,
  status,
}: {
  initial: SettingsSnapshot;
  onOpenChange: (open: boolean) => void;
  status?: MemoryStatusResource;
}) {
  const { t } = useTranslation("memory");
  const queryClient = useQueryClient();
  const updateSettings = useMutation(
    withLocalErrorMutation(updateMemorySettingsMutation()),
  );
  const testEmbedding = useMutation(
    withLocalErrorMutation(testMemoryEmbeddingMutation()),
  );
  const reindex = useMutation(
    withLocalErrorMutation(reindexMemoryIndexMutation()),
  );
  const [saved, setSaved] = useState(initial);
  const [baseURL, setBaseURL] = useState(initial.baseUrl);
  const [model, setModel] = useState(initial.model);
  const [apiKey, setAPIKey] = useState("");
  const [clearAPIKey, setClearAPIKey] = useState(false);
  const [localError, setLocalError] = useState("");
  const [saveSucceeded, setSaveSucceeded] = useState(false);

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

  const save = async () => {
    setLocalError("");
    setSaveSucceeded(false);
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
    try {
      await updateSettings.mutateAsync({
        body: {
          apiKey,
          baseUrl: normalizedBaseURL,
          clearApiKey: clearAPIKey,
          model: normalizedModel,
        },
      });
      const apiKeyConfigured = clearAPIKey
        ? false
        : Boolean(apiKey) || saved.apiKeyConfigured;
      setSaved({
        apiKeyConfigured,
        baseUrl: normalizedBaseURL,
        model: normalizedModel,
      });
      setAPIKey("");
      setClearAPIKey(false);
      setSaveSucceeded(true);
      await invalidateMemoryQueries(queryClient);
    } catch (error) {
      setLocalError(localApiErrorMessage(error, t("settings.saveFailed")));
    }
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
    <Dialog open onOpenChange={onOpenChange}>
      <MemoryWorkspaceDialogContent
        description={t("settings.description")}
        title={t("settings.title")}
      >
        <div className="scrollbar-hidden grid min-h-0 flex-1 gap-6 overflow-y-auto px-6 py-5">
          <section>
            <div className="mb-4 flex items-center justify-between gap-4">
              <div>
                <h3 className="type-label font-medium">
                  {t("settings.serviceTitle")}
                </h3>
                <p className="text-muted-foreground type-caption mt-1">
                  {t("settings.serviceDescription")}
                </p>
              </div>
              <div className="flex items-center gap-2">
                {showUpdateMemories ? (
                  <Button
                    disabled={reindex.isPending || dirty}
                    size="sm"
                    variant="secondary"
                    onClick={() => void updateExistingMemories()}
                  >
                    <RefreshCw />
                    {reindex.isPending
                      ? t("settings.updatingMemories")
                      : t("settings.updateMemories")}
                  </Button>
                ) : null}
                <Button
                  disabled={testEmbedding.isPending || !savedComplete}
                  size="sm"
                  type="button"
                  variant="secondary"
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
            </div>

            <div className="grid grid-cols-2 gap-x-4 gap-y-5">
              <label className="col-span-2 grid gap-2">
                <span className="type-label font-medium">
                  {t("settings.baseURL")}
                </span>
                <Input
                  placeholder={t("settings.baseURLPlaceholder")}
                  type="url"
                  value={baseURL}
                  onChange={(event) => {
                    setBaseURL(event.target.value);
                    setSaveSucceeded(false);
                  }}
                />
                <span className="text-muted-foreground type-caption">
                  {t("settings.baseURLHelp", { base: "{base}" })}
                </span>
              </label>
              <label className="grid gap-2">
                <span className="type-label font-medium">
                  {t("settings.model")}
                </span>
                <Input
                  placeholder={t("settings.modelPlaceholder")}
                  value={model}
                  onChange={(event) => {
                    setModel(event.target.value);
                    setSaveSucceeded(false);
                  }}
                />
                <span className="text-muted-foreground type-caption">
                  {t("settings.modelHelp")}
                </span>
              </label>
              <div className="grid gap-2">
                <span className="type-label font-medium">
                  {t("settings.apiKey")}
                </span>
                <div className="flex items-center gap-2">
                  <Input
                    placeholder={
                      clearAPIKey
                        ? t("settings.apiKeyWillClear")
                        : saved.apiKeyConfigured
                          ? t("settings.apiKeyKeep")
                          : t("settings.apiKeyPlaceholder")
                    }
                    type="password"
                    value={apiKey}
                    onChange={(event) => {
                      setAPIKey(event.target.value);
                      setClearAPIKey(false);
                      setSaveSucceeded(false);
                    }}
                  />
                  {saved.apiKeyConfigured ? (
                    <Button
                      size="sm"
                      type="button"
                      variant="ghost"
                      onClick={() => {
                        setAPIKey("");
                        setClearAPIKey((value) => !value);
                        setSaveSucceeded(false);
                      }}
                    >
                      {clearAPIKey
                        ? t("settings.undoClear")
                        : t("settings.clear")}
                    </Button>
                  ) : null}
                </div>
                <span className="text-muted-foreground type-caption">
                  {t("settings.apiKeyHelp")}
                </span>
              </div>
            </div>

            {dirty && status?.configured ? (
              <p className="text-muted-foreground type-caption mt-4">
                {t("settings.saveBeforeUpdate")}
              </p>
            ) : null}
            {reindex.isSuccess ? (
              <p className="text-success type-body mt-4">
                {t("settings.updateSucceeded")}
              </p>
            ) : null}
            {reindex.isError ? (
              <p role="alert" className="text-destructive type-body mt-4">
                {localApiErrorMessage(
                  reindex.error,
                  t("settings.updateFailed"),
                )}
              </p>
            ) : null}
            {testEmbedding.isError ? (
              <p role="alert" className="text-destructive type-body mt-4">
                {localApiErrorMessage(
                  testEmbedding.error,
                  t("settings.testFailed"),
                )}
              </p>
            ) : null}
          </section>

          {localError ? (
            <p role="alert" className="text-destructive type-body">
              {localError}
            </p>
          ) : null}
          {saveSucceeded ? (
            <p className="text-success type-body">
              {t("settings.saveSucceeded")}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="secondary" disabled={updateSettings.isPending}>
              {t("settings.close")}
            </Button>
          </DialogClose>
          <Button
            disabled={updateSettings.isPending || !dirty}
            onClick={() => void save()}
          >
            {updateSettings.isPending
              ? t("settings.saving")
              : t("settings.save")}
          </Button>
        </DialogFooter>
      </MemoryWorkspaceDialogContent>
    </Dialog>
  );
}
