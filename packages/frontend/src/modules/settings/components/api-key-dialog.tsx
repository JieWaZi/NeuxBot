import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm, useWatch } from "react-hook-form";
import { useId, useState } from "react";
import { Check, Copy, Info } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import {
  createApiKey,
  updateApiKey,
  listPublishableResources,
} from "@/shared/api/generated/sdk.gen";
import type {
  CreateApiKeyResponse,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsIntegrationKey as APIKey,
} from "@/shared/api/generated/types.gen";
import {
  withLocalErrorSurface,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";
import { ApiError } from "@/shared/api/errors";
import { Button } from "@/shared/ui/button";
import { DatePicker, TimePicker } from "@/shared/ui/date-time-picker";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import { QueryState } from "@/shared/ui/query-state";
import { SearchableMultiSelect } from "@/shared/ui/searchable-multi-select";
import { Skeleton } from "@/shared/ui/skeleton";
import {
  apiKeyFormSchema,
  knowledgeCapabilities,
  taskCapabilities,
  toAPIKeyGrants,
  type APIKeyForm,
} from "../lib/api-key-form";
import {
  apiKeysQueryKey,
  publishableResourcesKey,
} from "../api/integration-queries";
import { useIntegrationCopy } from "../use-integration-copy";

function ResourcePicker({
  kind,
  value,
  onChange,
  disabled,
}: {
  kind: "knowledge" | "automatic_task";
  value: string[];
  onChange: (value: string[]) => void;
  disabled: boolean;
}) {
  const t = useIntegrationCopy();
  const resources = useInfiniteQuery({
    queryKey: publishableResourcesKey({ query: { kind, pageSize: 100 } }),
    initialPageParam: 1,
    queryFn: async ({ pageParam, signal }) =>
      (
        await listPublishableResources<true>(
          withLocalErrorSurface({
            query: { kind, pageNum: pageParam, pageSize: 100 },
            signal,
            throwOnError: true,
          }),
        )
      ).data,
    getNextPageParam: (last, pages) =>
      pages.length * 100 < Math.min(last.totalSize ?? 0, 1000)
        ? pages.length + 1
        : undefined,
  });
  const rows =
    resources.data?.pages
      .flatMap((page) => page.resources ?? [])
      .flatMap((row) =>
        row.id ? [{ value: row.id, label: row.name || row.id }] : [],
      ) ?? [];
  const options = [
    ...rows,
    ...value
      .filter((id) => !rows.some((row) => row.value === id))
      .map((id) => ({ value: id, label: id })),
  ];
  return (
    <div className="grid min-w-0 gap-2">
      <SearchableMultiSelect
        size="md"
        options={options}
        value={value}
        onValueChange={onChange}
        disabled={disabled}
        triggerLabel={t("resourceSelection", { count: value.length })}
        searchLabel={t("resourceSearch")}
        emptyLabel={t("noResources")}
        clearSearchLabel={t("clearSearch")}
        clearSelectionLabel={t("clearSelection")}
      />
      {resources.isPending ? (
        <div role="status" aria-busy="true">
          <Skeleton className="h-4 w-full" />
        </div>
      ) : resources.isError ? (
        <QueryState
          error={resources.error}
          onRetry={() => void resources.refetch()}
        />
      ) : resources.hasNextPage ? (
        <Button
          type="button"
          size="sm"
          variant="ghost"
          disabled={disabled || resources.isFetchingNextPage}
          onClick={() => void resources.fetchNextPage()}
        >
          {t("loadMore")}
        </Button>
      ) : null}
    </div>
  );
}

export function APIKeySecret({
  issuance,
  onClose,
  helpId,
}: {
  helpId?: string;
  issuance: CreateApiKeyResponse;
  onClose: () => void;
}) {
  const t = useIntegrationCopy();
  const [copied, setCopied] = useState(false);
  return (
    <>
      <div className="grid gap-3 px-6 pb-6">
        <p
          id={helpId}
          role="note"
          className="bg-status-warning-surface text-status-warning-content type-body flex items-start gap-2 rounded-lg p-3"
        >
          <Info aria-hidden="true" className="size-icon-md mt-0.5 shrink-0" />
          <span>
            {t(
              issuance.secret
                ? "secretHelp"
                : issuance.issuanceState === "incomplete"
                  ? "secretIncomplete"
                  : "secretUnavailable",
            )}
          </span>
        </p>
        {issuance.secret && (
          <div className="flex min-w-0 items-center gap-2">
            <Input
              aria-label={t("secretTitle")}
              className="min-w-0 flex-1"
              value={issuance.secret}
              readOnly
              autoComplete="off"
            />
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    className="shrink-0"
                    aria-label={t(copied ? "copied" : "copy")}
                    onClick={async () => {
                      await navigator.clipboard.writeText(
                        issuance.secret ?? "",
                      );
                      setCopied(true);
                    }}
                  >
                    {copied ? (
                      <Check aria-hidden="true" />
                    ) : (
                      <Copy aria-hidden="true" />
                    )}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t(copied ? "copied" : "copy")}</TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
        )}
      </div>
      <DialogFooter>
        <Button onClick={onClose}>{t("close")}</Button>
      </DialogFooter>
    </>
  );
}

/** 表单以打开时的版本提交；冲突不重设默认值，因此不会丢失编辑草稿。 */
export function APIKeyDialog({
  entry,
  onClose,
}: {
  entry?: APIKey;
  onClose: () => void;
}) {
  const t = useIntegrationCopy();
  const id = useId();
  const queryClient = useQueryClient();
  const [requestId] = useState(() => crypto.randomUUID());
  const [issuance, setIssuance] = useState<CreateApiKeyResponse>();
  const group = (kind: string) =>
    entry?.grants?.find((grant) => grant.resourceKind === kind);
  const initialExpiry = entry?.expiresAt
    ? new Date(
        entry.expiresAt -
          new Date(entry.expiresAt).getTimezoneOffset() * 60_000,
      )
        .toISOString()
        .slice(0, 16)
    : "";
  const form = useForm<APIKeyForm>({
    resolver: zodResolver(apiKeyFormSchema),
    defaultValues: {
      name: entry?.name ?? "",
      expiresAt: initialExpiry,
      knowledgeIDs: group("knowledge")?.resourceIds ?? [],
      knowledgeCapabilities: (group("knowledge")?.capabilities ?? []).filter(
        (
          capability,
        ): capability is APIKeyForm["knowledgeCapabilities"][number] =>
          knowledgeCapabilities.some((key) => key === capability),
      ),
      taskIDs: group("automatic_task")?.resourceIds ?? [],
      taskCapabilities: (group("automatic_task")?.capabilities ?? []).filter(
        (capability): capability is APIKeyForm["taskCapabilities"][number] =>
          taskCapabilities.some((key) => key === capability),
      ),
    },
  });
  const watched = useWatch({ control: form.control });
  const values = { ...form.getValues(), ...watched };
  const save = useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async (fields: APIKeyForm) => {
      const body = {
        name: fields.name,
        grants: toAPIKeyGrants(fields),
        ...(fields.expiresAt
          ? {
              expiresAt:
                fields.expiresAt === initialExpiry && entry?.expiresAt
                  ? entry.expiresAt
                  : Date.parse(fields.expiresAt),
            }
          : {}),
      };
      if (entry?.id) {
        await updateApiKey<true>(
          withLocalErrorSurface({
            path: { keyId: entry.id },
            body: { ...body, expectedRevision: entry.revision ?? 0 },
            throwOnError: true,
          }),
        );
        return undefined;
      }
      return (
        await createApiKey<true>(
          withLocalErrorSurface({
            body: { ...body, requestId },
            throwOnError: true,
          }),
        )
      ).data;
    },
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({ queryKey: apiKeysQueryKey() });
      if (result) setIssuance(result);
      else onClose();
    },
  });
  const close = () => {
    if (save.isPending) return;
    save.reset();
    setIssuance(undefined);
    onClose();
  };
  const label = (cap: string) =>
    t(
      cap === "retrieve"
        ? "read"
        : cap === "ingest"
          ? "ingest"
          : cap === "organize"
            ? "organize"
            : cap === "automatic_task.invoke"
              ? "invoke"
              : "results",
    );
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) close();
      }}
    >
      <DialogContent
        size="lg"
        title={t(issuance ? "secretTitle" : entry ? "editTitle" : "create")}
        description={issuance ? undefined : t("keysHelp")}
        {...(issuance ? { "aria-describedby": `${id}-secret-help` } : {})}
      >
        {issuance ? (
          <APIKeySecret
            issuance={issuance}
            onClose={close}
            helpId={`${id}-secret-help`}
          />
        ) : (
          <form
            className="flex min-h-0 flex-col"
            onSubmit={form.handleSubmit((value) => save.mutate(value))}
          >
            <div className="grid gap-5 overflow-y-auto px-6 pb-6">
              <div className="grid gap-2">
                <FieldLabel htmlFor={`${id}-name`} required>
                  {t("name")}
                </FieldLabel>
                <Input
                  id={`${id}-name`}
                  placeholder={t("namePlaceholder")}
                  {...form.register("name")}
                  maxLength={120}
                  disabled={save.isPending}
                />
              </div>
              <div className="grid gap-2">
                <FieldLabel htmlFor={`${id}-expiry`}>{t("expiry")}</FieldLabel>
                <div className="flex min-w-0 flex-wrap items-center gap-2">
                  <div className="min-w-0 flex-1">
                    <DatePicker
                      id={`${id}-expiry`}
                      aria-label={t("expiryDate")}
                      value={values.expiresAt.split("T")[0] ?? ""}
                      onChange={(date) =>
                        form.setValue(
                          "expiresAt",
                          `${date}T${values.expiresAt.split("T")[1] || "09:00"}`,
                          { shouldValidate: true },
                        )
                      }
                      disabled={save.isPending}
                    />
                  </div>
                  <div className="min-w-0 flex-1">
                    <TimePicker
                      aria-label={t("expiryTime")}
                      value={values.expiresAt.split("T")[1] ?? ""}
                      onChange={(time) =>
                        form.setValue(
                          "expiresAt",
                          `${values.expiresAt.split("T")[0]}T${time}`,
                          { shouldValidate: true },
                        )
                      }
                      disabled={save.isPending || !values.expiresAt}
                    />
                  </div>
                  {values.expiresAt && (
                    <Button
                      type="button"
                      variant="ghost"
                      disabled={save.isPending}
                      onClick={() =>
                        form.setValue("expiresAt", "", { shouldValidate: true })
                      }
                    >
                      {t("noExpiry")}
                    </Button>
                  )}
                </div>
                <p className="type-ui text-content-secondary">
                  {t("expiryHelp")}
                </p>
              </div>
              {(["knowledge", "automatic_task"] as const).map((kind) => {
                const field =
                  kind === "knowledge"
                    ? "knowledgeCapabilities"
                    : "taskCapabilities";
                const resourceField =
                  kind === "knowledge" ? "knowledgeIDs" : "taskIDs";
                const capabilities =
                  kind === "knowledge"
                    ? knowledgeCapabilities
                    : taskCapabilities;
                return (
                  <fieldset
                    className="@container/grants grid min-w-0 gap-3"
                    key={kind}
                  >
                    <legend className="type-body mb-2 font-medium">
                      {t(kind === "knowledge" ? "knowledge" : "tasks")}
                    </legend>
                    <p className="type-ui text-content-secondary">
                      {t(kind === "knowledge" ? "knowledgeHelp" : "taskHelp")}
                    </p>
                    <div className="grid min-w-0 grid-cols-1 items-start gap-3 @md/grants:grid-cols-2">
                      <SearchableMultiSelect
                        size="md"
                        options={capabilities.map((capability) => ({
                          value: capability,
                          label: label(capability),
                        }))}
                        value={values[field]}
                        onValueChange={(selected) => {
                          if (field === "knowledgeCapabilities") {
                            form.setValue(
                              field,
                              knowledgeCapabilities.filter((capability) =>
                                selected.includes(capability),
                              ),
                              { shouldValidate: true },
                            );
                          } else {
                            form.setValue(
                              field,
                              taskCapabilities.filter((capability) =>
                                selected.includes(capability),
                              ),
                              { shouldValidate: true },
                            );
                          }
                        }}
                        disabled={save.isPending}
                        triggerLabel={
                          values[field].length
                            ? values[field].map(label).join("、")
                            : t("permissionSelection")
                        }
                        searchLabel={t("permissionSearch")}
                        emptyLabel={t("noPermissions")}
                        clearSearchLabel={t("clearSearch")}
                        clearSelectionLabel={t("clearPermissions")}
                      />
                      <ResourcePicker
                        kind={kind}
                        value={values[resourceField]}
                        onChange={(value) =>
                          form.setValue(resourceField, value, {
                            shouldValidate: true,
                          })
                        }
                        disabled={save.isPending}
                      />
                    </div>
                  </fieldset>
                );
              })}
              {Object.keys(form.formState.errors).length > 0 && (
                <p role="alert" className="type-body text-destructive">
                  {t("invalid")}
                </p>
              )}
              {save.error && (
                <p role="alert" className="type-body text-destructive">
                  {save.error instanceof ApiError && save.error.status === 409
                    ? t("conflict")
                    : localApiErrorMessage(save.error, t("failed"))}
                </p>
              )}
            </div>
            <DialogFooter>
              <Button
                variant="ghost"
                type="button"
                onClick={close}
                disabled={save.isPending}
              >
                {t("cancel")}
              </Button>
              <Button type="submit" disabled={save.isPending}>
                {t(save.isPending ? "saving" : entry ? "save" : "create")}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
