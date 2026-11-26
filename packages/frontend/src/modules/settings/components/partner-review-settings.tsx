import {
  keepPreviousData,
  useIsMutating,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useSearchParams } from "react-router-dom";

import { SemanticSearchSettingsForm } from "@/modules/memory";
import {
  getReviewSettingsOptions,
  getWakerOptions,
  getWakerReviewSettingsOptions,
  getWakerReviewSettingsQueryKey,
  listAiAccountsOptions,
  listWakerReviewSettingsOptions,
  listWakerReviewSettingsQueryKey,
  retryWakerReviewMutation,
  updateReviewSettingsMutation,
  updateWakerReviewSettingsMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import type { ListWakerReviewSettingsResponse } from "@/shared/api/generated/types.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { listRuntimesOptions } from "@/shared/api/runtime-queries";
import {
  useExecutionSelection,
  type ExecutionSelection,
} from "@/shared/hooks/use-execution-selection";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import {
  partnerReviewSettingsHref,
  type PartnerReviewTab,
} from "@/shared/lib/partner-review-settings";
import { Button } from "@/shared/ui/button";
import { ExecutionConfigurationFields } from "@/shared/ui/execution-configuration-fields";
import { PageHeader } from "@/shared/ui/page-header";
import { Pagination } from "@/shared/ui/pagination";
import { ResourcePartnerAvatar } from "@/shared/ui/resource-management-card";
import { SceneBreadcrumbs } from "@/shared/ui/scene-breadcrumbs";
import { SearchField } from "@/shared/ui/search-field";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { Skeleton } from "@/shared/ui/skeleton";
import { StatusNotice } from "@/shared/ui/status-notice";
import { Switch } from "@/shared/ui/switch";
import {
  SettingsCard,
  SettingsContentShell,
  SettingsGroup,
  SettingsNavigationRow,
  SettingsRow,
} from "./settings-section-layout";

const skillModes = ["ask", "automatic"] as const;
const reviewIntervals = [5, 10, 20, 30, 50] as const;
const partnerPageSize = 50;

export function PartnerReviewSettings() {
  const { t } = useTranslation("memory");
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const embeddingOpen = params.get("semanticSearch") === "1";
  const setEmbeddingOpen = (open: boolean) => {
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        if (open) next.set("semanticSearch", "1");
        else next.delete("semanticSearch");
        return next;
      },
      { replace: true },
    );
  };

  return (
    <SettingsGroup title={t("review.title")}>
      <SettingsCard>
        <ReviewAgentSettings />
        <SettingsNavigationRow
          title={t("review.partner")}
          onClick={() => navigate(partnerReviewSettingsHref())}
        />
        <SettingsRow
          grouped
          title={t("review.semanticSearch")}
          content={
            embeddingOpen ? (
              <SemanticSearchSettingsForm
                onClose={() => setEmbeddingOpen(false)}
              />
            ) : null
          }
        >
          {!embeddingOpen ? (
            <Button
              size="settings"
              variant="settings-outline"
              aria-label={t("review.semanticSearch")}
              onClick={() => setEmbeddingOpen(true)}
            >
              {t("review.configure")}
            </Button>
          ) : null}
        </SettingsRow>
      </SettingsCard>
    </SettingsGroup>
  );
}

function ReviewAgentSettings() {
  const { t } = useTranslation(["memory", "common"]);
  const queryClient = useQueryClient();
  const options = getReviewSettingsOptions(withLocalErrorSurface({}));
  const settings = useQuery(options);
  useQueryEvents({
    topic: "resources",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    eventTypes: ["partner.review.settings.changed"],
  });
  const [draft, setDraft] = useState<ExecutionSelection | null>(null);
  const update = useMutation({
    ...withLocalErrorMutation(updateReviewSettingsMutation()),
    onSuccess: async (data, { body }) => {
      await queryClient.cancelQueries({
        queryKey: options.queryKey,
        exact: true,
      });
      queryClient.setQueryData(options.queryKey, data);
      if (body.source) {
        setDraft(null);
        const [{ _id, baseUrl }] = getWakerReviewSettingsQueryKey({
          path: { wakerId: "" },
        });
        await queryClient.invalidateQueries({ queryKey: [{ _id, baseUrl }] });
      }
    },
  });
  const saved = settings.data;
  const source = saved?.source === "global" ? "global" : "partner";
  const execution = useExecutionSelection({
    value: draft ?? saved ?? {},
    enabled: draft !== null,
  });
  const executionState = {
    ...execution,
    selection: { ...execution.selection, model: execution.effectiveModel },
  };
  const { runtimeId, accountId } = execution.selection;
  const model = execution.effectiveModel;
  const saveGlobal = () => {
    if (!runtimeId || !accountId || !model) return;
    update.mutate({ body: { source: "global", runtimeId, accountId, model } });
  };
  const beginGlobalEdit = () => {
    update.reset();
    setDraft({
      runtimeId: saved?.runtimeId,
      accountId: saved?.accountId,
      model: saved?.model,
    });
  };

  return (
    <>
      <SettingsRow
        grouped
        title={t("review.agent")}
        description={
          saved?.source === "global" && !draft ? (
            <ReviewConfigurationLabel value={saved} />
          ) : undefined
        }
        content={
          <>
            {settings.isError ? (
              <ReviewError
                error={settings.error}
                onRetry={() => void settings.refetch()}
              />
            ) : null}
            {draft ? (
              <div className="mt-3 grid gap-3">
                {execution.runtimes.length ? (
                  <ExecutionConfigurationFields
                    state={executionState}
                    disabled={update.isPending}
                    onChange={setDraft}
                    defaults
                    permissionHidden
                    reasoningHidden
                    helpHidden
                  />
                ) : execution.runtimesQuery.isPending ? (
                  <ReviewLoading />
                ) : execution.runtimesQuery.isError ? (
                  <ReviewError
                    error={execution.runtimesQuery.error}
                    onRetry={() => void execution.runtimesQuery.refetch()}
                  />
                ) : (
                  <p className="type-body text-content-secondary">
                    {t("review.noEligibleRuntime")}
                  </p>
                )}
                <div className="flex flex-wrap justify-end gap-2">
                  <Button
                    size="settings"
                    variant="settings-outline"
                    disabled={update.isPending}
                    onClick={() => {
                      setDraft(null);
                      update.reset();
                    }}
                  >
                    {t("form.cancel")}
                  </Button>
                  <Button
                    size="settings"
                    disabled={
                      update.isPending ||
                      !execution.valid ||
                      !runtimeId ||
                      !model
                    }
                    onClick={saveGlobal}
                  >
                    {t(update.isPending ? "form.saving" : "form.save")}
                  </Button>
                </div>
              </div>
            ) : null}
            {update.isError && update.variables?.body.source ? (
              <ReviewError error={update.error} />
            ) : null}
          </>
        }
      >
        {!settings.data ? (
          settings.isLoading ? (
            <ReviewLoading />
          ) : null
        ) : (
          <div className="flex max-w-full min-w-0 items-center gap-2">
            <Select
              value={draft ? "global" : source}
              disabled={update.isPending}
              onValueChange={(source) => {
                if (source === "global") beginGlobalEdit();
                else if (saved?.source === "global")
                  update.mutate({ body: { source: "partner" } });
                else {
                  setDraft(null);
                  update.reset();
                }
              }}
            >
              <SelectTrigger
                size="settings"
                appearance="settings"
                aria-label={t("review.agent")}
                className="w-auto min-w-0"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent align="end">
                <SelectItem value="partner">
                  {t("review.followPartner")}
                </SelectItem>
                <SelectItem value="global">{t("review.dedicated")}</SelectItem>
              </SelectContent>
            </Select>
            {saved?.source === "global" && !draft ? (
              <Button
                size="settings"
                variant="settings-outline"
                className="shrink-0"
                disabled={update.isPending}
                onClick={beginGlobalEdit}
              >
                {t("review.configure")}
              </Button>
            ) : null}
          </div>
        )}
      </SettingsRow>
      <SettingsRow
        grouped
        title={t("review.frequency")}
        content={
          update.isError && update.variables?.body.everyTurns ? (
            <ReviewError error={update.error} />
          ) : null
        }
      >
        {saved ? (
          <Select
            value={String(saved.everyTurns ?? 20)}
            disabled={update.isPending}
            onValueChange={(value) =>
              update.mutate({ body: { everyTurns: Number(value) } })
            }
          >
            <SelectTrigger
              size="settings"
              appearance="settings"
              aria-label={t("review.frequency")}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent align="end">
              {reviewIntervals.map((count) => (
                <SelectItem value={String(count)} key={count}>
                  {t("review.everyTurns", { count })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : settings.isPending ? (
          <ReviewLoading />
        ) : null}
      </SettingsRow>
    </>
  );
}

export function PartnerReviewSettingsDetail({
  showSceneChrome = true,
}: {
  showSceneChrome?: boolean;
}) {
  const { t } = useTranslation(["memory", "waker", "common", "aiAccounts"]);
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const tab: PartnerReviewTab =
    params.get("tab") === "skill" ? "skill" : "memory";
  const focusedId = params.get("wakerId") || "";
  const pageParam = Number(params.get("page"));
  const page = Number.isSafeInteger(pageParam) && pageParam > 0 ? pageParam : 1;
  const search = params.get("query") || "";
  const list = useQuery({
    ...listWakerReviewSettingsOptions(
      withLocalErrorSurface({
        query: {
          pageNum: page,
          pageSize: partnerPageSize,
          query: search.trim(),
        },
      }),
    ),
    placeholderData: keepPreviousData,
  });
  useQueryEvents({
    topic: "resources",
    resource: { kind: "waker" },
    queryKeys: [listWakerReviewSettingsQueryKey()],
    eventTypes: ["partner.review.changed"],
  });
  const resources = list.isPlaceholderData ? [] : (list.data?.resources ?? []);
  const total = list.data?.totalSize ?? 0;
  const lastPage = Math.max(1, Math.ceil(total / partnerPageSize));
  const hasCurrentPage = Boolean(list.data) && !list.isPlaceholderData;
  useEffect(() => {
    if (!hasCurrentPage || page <= lastPage) return;
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        if (lastPage === 1) next.delete("page");
        else next.set("page", String(lastPage));
        return next;
      },
      { replace: true },
    );
  }, [hasCurrentPage, lastPage, page, setParams]);
  const needsFocusedPartner =
    Boolean(
      focusedId &&
      list.data &&
      !list.isPlaceholderData &&
      !search &&
      page === 1,
    ) && !resources.some((row) => row.wakerId === focusedId);
  const focusedPartner = useQuery({
    ...getWakerOptions(withLocalErrorSurface({ path: { wakerId: focusedId } })),
    enabled: needsFocusedPartner,
  });
  const focusedSettings = useQuery({
    ...getWakerReviewSettingsOptions(
      withLocalErrorSurface({
        path: { wakerId: focusedId },
      }),
    ),
    enabled: needsFocusedPartner && Boolean(focusedPartner.data),
  });
  const extra =
    needsFocusedPartner && focusedPartner.data && focusedSettings.data
      ? {
          wakerId: focusedId,
          name: focusedPartner.data.name,
          avatar: focusedPartner.data.avatar,
          memoryEnabled: focusedSettings.data.memoryEnabled,
          skillMode: focusedSettings.data.skillMode,
        }
      : undefined;
  const rows = extra ? [extra, ...resources] : resources;
  const change = (values: Record<string, string>, replace = false) => {
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.set("detail", "partner-review");
        for (const [key, value] of Object.entries(values)) {
          if (value) next.set(key, value);
          else next.delete(key);
        }
        return next;
      },
      { replace },
    );
  };
  const title = t("review.partner");
  const back = () => navigate("/settings/agents");

  return (
    <>
      {showSceneChrome && (
        <SceneBreadcrumbs
          ariaLabel={t("aiAccounts:agents.title")}
          items={[
            { label: t("aiAccounts:agents.title"), href: "/settings/agents" },
            { label: title },
          ]}
        />
      )}
      <SettingsContentShell showHeader={false}>
        <PageHeader
          appearance="compact"
          layout="detail"
          title={title}
          backAction={{ label: t("aiAccounts:agents.back"), onClick: back }}
        />
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <SegmentedControl
            appearance="shell"
            ariaLabel={title}
            value={tab}
            options={[
              { value: "memory", label: t("review.memory") },
              { value: "skill", label: t("review.skill") },
            ]}
            onValueChange={(value) => change({ tab: value })}
          />
          {total > partnerPageSize || search ? (
            <SearchField
              width="toolbar"
              aria-label={t("waker:search")}
              placeholder={t("waker:search")}
              clearLabel={t("common:clearSearch")}
              value={search}
              onChange={(event) =>
                change({ query: event.target.value, page: "" }, true)
              }
            />
          ) : null}
        </div>
        <p className="type-body text-content-secondary">
          {t(
            tab === "memory" ? "review.memoryEvidence" : "review.skillEvidence",
          )}
        </p>
        <SettingsCard>
          {rows.map((row) =>
            row.wakerId ? (
              <PartnerReviewRow
                key={row.wakerId}
                {...row}
                wakerId={row.wakerId}
                tab={tab}
                focused={row.wakerId === focusedId}
                onFocusChange={() =>
                  change(
                    {
                      wakerId:
                        row.wakerId === focusedId ? "" : row.wakerId || "",
                    },
                    true,
                  )
                }
              />
            ) : null,
          )}
          {list.isError ||
          (needsFocusedPartner &&
            (focusedPartner.isError || focusedSettings.isError)) ? (
            <div className="px-4 py-3">
              <ReviewError
                error={
                  list.error ?? focusedPartner.error ?? focusedSettings.error
                }
                onRetry={() => {
                  if (list.isError) void list.refetch();
                  else if (focusedPartner.isError)
                    void focusedPartner.refetch();
                  else void focusedSettings.refetch();
                }}
              />
            </div>
          ) : !list.data ||
            list.isPlaceholderData ||
            (needsFocusedPartner && !extra) ? (
            <div className="px-4 py-3">
              <ReviewLoading />
            </div>
          ) : rows.length === 0 ? (
            <p className="type-body text-content-secondary px-4 py-4">
              {t(search ? "review.noMatches" : "review.noPartner")}
            </p>
          ) : null}
        </SettingsCard>
        {total > partnerPageSize ? (
          <Pagination
            labels={{
              navigation: t("review.partner"),
              previous: t("waker:picker.previous"),
              next: t("waker:picker.next"),
              page: (page) => t("pagination.page", { page }),
              pageSize: (size) => t("pagination.pageSize", { size }),
              pageSizeSelection: t("pagination.pageSizeSelection"),
              total: (count) => t("waker:count", { count }),
            }}
            page={page}
            pageSize={partnerPageSize}
            totalItems={total}
            pending={list.isFetching}
            onPageChange={(value) =>
              change({ page: value > 1 ? String(value) : "" })
            }
          />
        ) : null}
      </SettingsContentShell>
    </>
  );
}

function PartnerReviewRow({
  wakerId,
  name = wakerId,
  avatar,
  memoryEnabled = false,
  skillMode,
  tab,
  focused,
  onFocusChange,
}: {
  wakerId: string;
  name?: string;
  avatar?: string;
  memoryEnabled?: boolean;
  skillMode?: string;
  tab: PartnerReviewTab;
  focused: boolean;
  onFocusChange: () => void;
}) {
  const { t } = useTranslation("memory");
  const queryClient = useQueryClient();
  const rowRef = useRef<HTMLElement>(null);
  useEffect(() => {
    if (focused) rowRef.current?.scrollIntoView?.({ block: "nearest" });
  }, [focused]);
  const mode = skillModes.find((value) => value === skillMode) ?? "off";
  const skillEnabled = mode !== "off";
  const active = tab === "memory" ? memoryEnabled : skillEnabled;
  const path = { wakerId };
  const mutationKey = ["partner-review-settings", wakerId];
  const submitting = useIsMutating({ mutationKey }) > 0;
  const update = useMutation({
    ...withLocalErrorMutation(updateWakerReviewSettingsMutation()),
    mutationKey,
    onSuccess: async (data) => {
      const queryKey = listWakerReviewSettingsQueryKey();
      await queryClient.cancelQueries({ queryKey });
      queryClient.setQueriesData<ListWakerReviewSettingsResponse>(
        { queryKey },
        (current) =>
          current && {
            ...current,
            resources: current.resources?.map((row) =>
              row.wakerId === wakerId
                ? {
                    ...row,
                    memoryEnabled: data.memoryEnabled,
                    skillMode: data.skillMode,
                  }
                : row,
            ),
          },
      );
      const diagnostics = getWakerReviewSettingsOptions(
        withLocalErrorSurface({ path }),
      );
      await queryClient.cancelQueries({
        queryKey: diagnostics.queryKey,
        exact: true,
      });
      queryClient.setQueryData(diagnostics.queryKey, data);
    },
  });
  const identity = (
    <>
      <ResourcePartnerAvatar name={name} avatar={avatar} size="xs" />
      <span className="min-w-0 truncate">{name}</span>
    </>
  );
  return (
    <SettingsRow
      ref={rowRef}
      grouped
      inlineActions
      title={
        active ? (
          <Button
            variant="plain"
            size="settings"
            className="max-w-full px-0"
            aria-expanded={focused}
            onClick={onFocusChange}
          >
            {identity}
          </Button>
        ) : (
          <span className="flex min-w-0 items-center gap-2">{identity}</span>
        )
      }
      content={
        <>
          {update.isError ? <ReviewError error={update.error} /> : null}
          {focused && active ? <PartnerReviewStatus wakerId={wakerId} /> : null}
        </>
      }
    >
      {tab === "skill" && skillEnabled ? (
        <Select
          value={mode}
          disabled={submitting}
          onValueChange={(value) => {
            const next = skillModes.find((item) => item === value);
            if (next)
              update.mutate({ path, body: { memoryEnabled, skillMode: next } });
          }}
        >
          <SelectTrigger
            size="settings"
            appearance="settings"
            className="w-auto min-w-0"
            aria-label={t("review.skillModeFor", { name })}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent align="end">
            {skillModes.map((value) => (
              <SelectItem key={value} value={value}>
                {t(`review.modes.${value}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : null}
      <Switch
        aria-label={t(
          tab === "memory" ? "review.memoryFor" : "review.skillFor",
          { name },
        )}
        checked={active}
        disabled={submitting}
        hitArea="comfortable"
        onClick={() =>
          update.mutate({
            path,
            body: {
              memoryEnabled: tab === "memory" ? !memoryEnabled : memoryEnabled,
              skillMode:
                tab === "skill" ? (skillEnabled ? "off" : "ask") : mode,
            },
          })
        }
      />
    </SettingsRow>
  );
}

function PartnerReviewStatus({ wakerId }: { wakerId: string }) {
  const { t } = useTranslation("memory");
  const queryClient = useQueryClient();
  const path = { wakerId };
  const options = getWakerReviewSettingsOptions(
    withLocalErrorSurface({ path }),
  );
  const review = useQuery(options);
  useQueryEvents({
    topic: "entities",
    resource: { kind: "waker", id: wakerId },
    queryKeys: [options.queryKey],
    eventTypes: ["entities.changed"],
  });
  useQueryEvents({
    topic: "resources",
    resource: { kind: "waker", id: wakerId },
    queryKeys: [options.queryKey],
    eventTypes: ["partner.review.changed"],
  });
  useQueryEvents({
    topic: "resources",
    resource: { kind: "global" },
    queryKeys: [options.queryKey],
    eventTypes: ["partner.review.settings.changed"],
  });
  useQueryEvents({
    topic: "runtimes",
    resource: { kind: "runtime", id: review.data?.runtimeId },
    queryKeys: [options.queryKey],
    enabled: Boolean(review.data?.runtimeId),
    eventTypes: [
      "runtime.installation.changed",
      "runtime.authentication.changed",
    ],
  });
  const retry = useMutation({
    ...withLocalErrorMutation(retryWakerReviewMutation()),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: options.queryKey }),
  });
  const blocked = review.data?.available === false;
  const failed = review.data?.status === "failed";
  if (review.isError)
    return (
      <ReviewError error={review.error} onRetry={() => void review.refetch()} />
    );
  if (!blocked && !failed) return null;
  return (
    <>
      <StatusNotice
        role="alert"
        className="mt-3 flex flex-wrap items-center justify-between gap-2"
      >
        <p>
          {t(
            blocked
              ? review.data?.reason === "runtimeUnsupported"
                ? "review.runtimeUnsupported"
                : "review.runtimeUnavailable"
              : "review.failed",
          )}
        </p>
        {failed && !blocked ? (
          <Button
            size="settings"
            variant="settings-outline"
            disabled={retry.isPending}
            onClick={() => retry.mutate({ path })}
          >
            {t("review.retry")}
          </Button>
        ) : null}
      </StatusNotice>
      {retry.isError ? <ReviewError error={retry.error} /> : null}
    </>
  );
}

function ReviewConfigurationLabel({ value }: { value: ExecutionSelection }) {
  const { t } = useTranslation("common");
  const runtimes = useQuery(listRuntimesOptions(withLocalErrorSurface({})));
  const { runtimeId, accountId, model } = value;
  const accounts = useQuery({
    ...listAiAccountsOptions(withLocalErrorSurface({})),
    enabled: Boolean(accountId && accountId !== "system"),
  });
  const runtime = runtimes.data?.resources?.find(({ id }) => id === runtimeId);
  const account = accounts.data?.resources?.find(({ id }) => id === accountId);
  return [
    runtimeDisplayName(runtime ?? { id: runtimeId }),
    accountId === "system"
      ? t("executionSelection.system")
      : account?.name || accountId,
    model,
  ]
    .filter(Boolean)
    .join(" · ");
}

function ReviewLoading() {
  const { t } = useTranslation("memory");
  return (
    <div role="status" aria-busy="true" aria-label={t("settings.loading")}>
      <Skeleton aria-hidden className="h-4 w-24" />
    </div>
  );
}

function ReviewError({
  error,
  onRetry,
}: {
  error: unknown;
  onRetry?: () => void;
}): ReactNode {
  const { t } = useTranslation(["memory", "common"]);
  return (
    <div className="mt-2 flex flex-wrap items-center gap-2" role="alert">
      <p className="type-body text-destructive break-words">
        {localApiErrorMessage(error, t("review.requestFailed"))}
      </p>
      {onRetry ? (
        <Button size="settings" variant="settings-outline" onClick={onRetry}>
          {t("common:retry")}
        </Button>
      ) : null}
    </div>
  );
}
