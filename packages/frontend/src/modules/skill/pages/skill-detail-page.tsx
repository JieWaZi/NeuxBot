import { resourceListBack } from "@/shared/lib/resource-navigation";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useNavigate,
  useParams,
  useLocation,
  useSearchParams,
} from "react-router-dom";

import { WakerDetailLayout } from "@/modules/waker";
import { DetailSection } from "@/shared/ui/detail-section";
import {
  ResourceCatalogRow,
  ResourceCatalogRows,
  ResourceCatalogStatus,
} from "@/shared/ui/resource-catalog";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Button } from "@/shared/ui/button";
import { ResourcePartnerAvatar } from "@/shared/ui/resource-management-card";
import { QueryState, QueryRefreshError } from "@/shared/ui/query-state";

import { Switch } from "@/shared/ui/switch";
import { useTheme } from "@/shared/theme";

import {
  createSkillVersionMutation,
  getSkillVersionOptions,
  getWakerSkillOptions,
  invalidateVisibleSkillQueries,
  listSkillVersionsOptions,
  pullSkillUpdateMutation,
  restoreSkillVersionMutation,
  setWakerSkillEnabledMutation,
  updateWakerSkillMutation,
} from "../api/skill-queries";
import { SkillPresentation } from "../components/skill-presentation";
import { SkillDeleteDialog } from "../components/skill-delete-dialog";
import { SkillDisplayNameEditor } from "../components/skill-display-name-editor";
import { SkillGitPullDialog } from "../components/skill-git-pull-dialog";
import { SkillDetailHeader } from "../components/skill-detail-header";
import { SkillInstructionsPanel } from "../components/skill-instructions-panel";
import { SkillMetadataCard } from "../components/skill-metadata-card";
import { SkillResourceBrowser } from "../components/skill-resource-browser";
import {
  SkillVersionDiffDialog,
  type DiffSkillVersion,
} from "../components/skill-version-diff-dialog";
import { SkillVersionHistory } from "../components/skill-version-history";
import {
  buildSkillResourceTree,
  countSkillResourceFiles,
} from "../model/skill-detail";

import { skillSourceName } from "../model/skill-source-name";

type DetailTab = "explanation" | "resources" | "version";

export function SkillDetailPage() {
  const { t } = useTranslation("skill");
  const { skillId = "", wakerId = "" } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const active = useRouteSceneActive();
  const back =
    (location.state as { resourceBack?: string } | null)?.resourceBack ??
    (wakerId
      ? `/wakers/${wakerId}/resources?category=skills`
      : "/resources?category=skills");
  const breadcrumbBack = resourceListBack(
    back,
    wakerId
      ? `/wakers/${wakerId}/resources?category=skills`
      : "/resources?category=skills",
  );
  const queryClient = useQueryClient();
  const { resolvedTheme } = useTheme();
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedTab = searchParams.get("tab");
  const tab: DetailTab =
    requestedTab === "resources" || requestedTab === "version"
      ? requestedTab
      : "explanation";
  const setTab = (next: DetailTab) => {
    const params = new URLSearchParams(searchParams);
    if (next === "explanation") params.delete("tab");
    else params.set("tab", next);
    setSearchParams(params, { replace: true, state: location.state });
  };
  const [diffVersionId, setDiffVersionId] = useState<string | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [removingPartner, setRemovingPartner] = useState<{
    id: string;
    name: string;
  } | null>(null);
  const detailOptions = getWakerSkillOptions(
    withLocalErrorSurface({
      path: { wakerId, skillId },
    }),
  );
  const detailQuery = useQuery({
    ...detailOptions,
    enabled: active && Boolean(skillId),
  });
  const versionsOptions = listSkillVersionsOptions(
    withLocalErrorSurface({ path: { wakerId, skillId } }),
  );
  const versionsQuery = useQuery({
    ...versionsOptions,
    enabled: active && Boolean(skillId),
  });
  useQueryEvents({
    topic: "resources",
    resource: { kind: "global" },
    queryKeys: [detailOptions.queryKey, versionsOptions.queryKey],
    active,
  });
  const skill = detailQuery.data?.skill;
  const currentVersion = detailQuery.data?.currentVersion;
  const currentVersionId = currentVersion?.id ?? "";
  const currentVersionQuery = useQuery({
    ...getSkillVersionOptions(
      withLocalErrorSurface({
        path: {
          wakerId,
          skillId,
          versionId: currentVersionId || "pending",
        },
      }),
    ),
    enabled: active && Boolean(skillId && currentVersionId),
  });
  const diffVersionQuery = useQuery({
    ...getSkillVersionOptions(
      withLocalErrorSurface({
        path: {
          wakerId,
          skillId,
          versionId: diffVersionId ?? "pending",
        },
      }),
    ),
    enabled: active && Boolean(diffVersionId),
  });

  const refresh = async () => {
    await invalidateVisibleSkillQueries(queryClient);
  };
  const toggleSkill = useMutation({
    ...withLocalErrorMutation(setWakerSkillEnabledMutation()),
    onSuccess: refresh,
  });
  const createVersion = useMutation({
    ...withLocalErrorMutation(createSkillVersionMutation()),
    onSuccess: refresh,
  });
  const restoreVersion = useMutation({
    ...withLocalErrorMutation(restoreSkillVersionMutation()),
    onSuccess: async () => {
      setDiffVersionId(null);
      await refresh();
    },
  });
  const pullUpdate = useMutation({
    ...withLocalErrorMutation(pullSkillUpdateMutation()),
    onSuccess: refresh,
  });
  const updateSkill = useMutation({
    ...withLocalErrorMutation(updateWakerSkillMutation()),
    onSuccess: refresh,
  });
  const resources = useMemo(
    () => buildSkillResourceTree(currentVersionQuery.data?.resources ?? []),
    [currentVersionQuery.data?.resources],
  );
  const selectedDiffVersion = diffVersionQuery.data
    ? ({
        id: diffVersionQuery.data.version?.id ?? "",
        version: diffVersionQuery.data.version?.version ?? "",
        instructions: diffVersionQuery.data.content ?? "",
        resources: diffVersionQuery.data.resources ?? [],
      } satisfies DiffSkillVersion)
    : null;
  const backAction = {
    label: t("detail.backToList"),
    onClick: () => navigate(breadcrumbBack),
  };

  const currentVersionNumber = currentVersion?.version ?? "";
  const sourceLabel = skill?.gitUrl
    ? skillSourceName({ source: "git", sourceUrl: skill.gitUrl }, t)
    : "";
  const instructions = currentVersionQuery.data?.content ?? "";
  const metadataDescription =
    currentVersion?.description ?? skill?.description ?? "";
  const partners = detailQuery.data?.installedPartners ?? [];
  const currentPartner = partners.find((partner) => partner.id === wakerId);
  const shownPartners = wakerId
    ? [
        {
          id: wakerId,
          name: currentPartner?.name,
          avatar: currentPartner?.avatar,
          enabled: detailQuery.data?.enabled,
        },
      ]
    : partners;
  const saveVersion = (
    body: Parameters<typeof createVersion.mutate>[0]["body"],
    onSuccess?: () => void,
  ) =>
    createVersion.mutate({ path: { wakerId, skillId }, body }, { onSuccess });

  return (
    <WakerDetailLayout
      active="skills"
      breadcrumbItems={[
        { label: t("title"), href: breadcrumbBack },
        { label: skill?.name || t("title") },
      ]}
    >
      <ResourcePageSurface appearance="catalog">
        <SkillDetailHeader
          backAction={backAction}
          title={skill?.name ?? t("title")}
          description={skill?.description}
          categories={skill?.categories}
          status={
            skill && (wakerId || partners.length > 0) ? (
              <ResourceCatalogStatus
                pill
                tone={
                  !wakerId || detailQuery.data?.enabled ? "success" : "neutral"
                }
              >
                {t(
                  wakerId
                    ? detailQuery.data?.enabled
                      ? "enabled"
                      : "disabled"
                    : "installed",
                )}
              </ResourceCatalogStatus>
            ) : undefined
          }
          actions={
            skill && (
              <>
                <SkillDisplayNameEditor
                  name={skill.name ?? ""}
                  categories={skill.categories ?? []}
                  pending={updateSkill.isPending}
                  error={updateSkill.error}
                  onSave={(metadata, onSuccess) =>
                    updateSkill.mutate(
                      { path: { wakerId, skillId }, body: metadata },
                      { onSuccess },
                    )
                  }
                />
                {!wakerId && skill.source === "git" && (
                  <SkillGitPullDialog
                    currentVersion={currentVersionNumber}
                    error={pullUpdate.isError}
                    pending={pullUpdate.isPending}
                    onPull={(version, notes, onSuccess) =>
                      pullUpdate.mutate(
                        {
                          path: { wakerId, skillId },
                          body: { version, notes },
                        },
                        { onSuccess },
                      )
                    }
                  />
                )}
              </>
            )
          }
        />
        <main className="mt-6 min-w-0 space-y-6">
          {skill && detailQuery.isError && (
            <QueryRefreshError
              error={detailQuery.error}
              pending={detailQuery.isFetching}
              onRetry={() => void detailQuery.refetch()}
            />
          )}
          <>
            {skill && (
              <SkillPresentation
                name={skill.name ?? ""}
                context={{
                  kind: "skill",
                  skillId,
                  versionId: currentVersionId,
                }}
                wakerId={wakerId}
                usable={Boolean(
                  wakerId &&
                  currentVersionId &&
                  detailQuery.data?.enabled &&
                  !detailQuery.isError,
                )}
              />
            )}
            {skill && shownPartners.length > 0 && (
              <DetailSection
                appearance="catalog"
                title={t("detail.installedPartners")}
              >
                <ResourceCatalogRows>
                  {shownPartners.map((partner) => {
                    const partnerId = partner.id;
                    if (!partnerId) return null;
                    const name =
                      partner.name || t("common:partnerNavigation.partner");
                    return (
                      <ResourceCatalogRow
                        key={partnerId}
                        className="flex-wrap"
                        role="group"
                        aria-label={name}
                      >
                        <ResourcePartnerAvatar
                          name={name}
                          avatar={partner.avatar}
                          size="md"
                        />
                        <div className="min-w-0 flex-1">
                          <h3
                            className="type-resource-body text-resource-content truncate font-medium"
                            title={name}
                          >
                            {name}
                          </h3>
                          {currentVersionNumber && (
                            <p className="type-resource-caption text-resource-muted">
                              v{currentVersionNumber}
                            </p>
                          )}
                        </div>
                        <div className="ml-auto flex max-w-full flex-wrap items-center gap-2">
                          {wakerId && skill.source === "git" && (
                            <SkillGitPullDialog
                              currentVersion={currentVersionNumber}
                              error={pullUpdate.isError}
                              pending={pullUpdate.isPending}
                              onPull={(version, notes, onSuccess) =>
                                pullUpdate.mutate(
                                  {
                                    path: { wakerId, skillId },
                                    body: { version, notes },
                                  },
                                  { onSuccess },
                                )
                              }
                            />
                          )}
                          {typeof partner.enabled === "boolean" && (
                            <Switch
                              hitArea="comfortable"
                              checked={partner.enabled}
                              disabled={toggleSkill.isPending}
                              aria-label={
                                wakerId
                                  ? t(
                                      partner.enabled
                                        ? "detail.disable"
                                        : "detail.enable",
                                      { name: skill.name },
                                    )
                                  : t("detail.partnerSkill", {
                                      name: skill.name,
                                      partner: name,
                                    })
                              }
                              onClick={() =>
                                toggleSkill.mutate({
                                  path: { wakerId: partnerId, skillId },
                                  body: { enabled: !partner.enabled },
                                })
                              }
                            />
                          )}
                          <Button
                            size="catalog"
                            variant="catalog-danger"
                            disabled={toggleSkill.isPending}
                            onClick={() => {
                              if (wakerId) setDeleteOpen(true);
                              else setRemovingPartner({ id: partnerId, name });
                            }}
                          >
                            {t("detail.remove")}
                          </Button>
                        </div>
                      </ResourceCatalogRow>
                    );
                  })}
                </ResourceCatalogRows>
                {toggleSkill.isError && (
                  <p
                    role="alert"
                    className="text-destructive type-resource-caption px-4 pb-3"
                  >
                    {localApiErrorMessage(toggleSkill.error, t("toggleFailed"))}
                  </p>
                )}
              </DetailSection>
            )}
            {/* 文档、文件与版本共用详情区域，切换保留未提交内容。 */}
            <section className="min-w-0 space-y-4">
              <div className="min-w-0 overflow-x-auto">
                <SegmentedControl
                  value={tab}
                  onValueChange={setTab}
                  ariaLabel={t("detail.tabs")}
                  appearance="shell"
                  size="sm"
                  distribution="fill"
                  options={[
                    { value: "explanation", label: t("detail.content") },
                    {
                      value: "resources",
                      label: `${t("detail.resources")} ${countSkillResourceFiles(resources)}`,
                    },
                    {
                      value: "version",
                      label: t("detail.version"),
                    },
                  ]}
                />
              </div>
              <div hidden={tab !== "explanation"}>
                {currentVersionQuery.data && currentVersionQuery.isError && (
                  <QueryRefreshError
                    error={currentVersionQuery.error}
                    pending={currentVersionQuery.isFetching}
                    onRetry={() =>
                      void (skill
                        ? currentVersionQuery.refetch()
                        : detailQuery.refetch())
                    }
                  />
                )}
                {!currentVersionQuery.data ? (
                  <QueryState
                    loadingLayout="content"
                    pending={
                      detailQuery.isPending ||
                      Boolean(
                        skill &&
                        currentVersionId &&
                        currentVersionQuery.isPending,
                      )
                    }
                    error={detailQuery.error ?? currentVersionQuery.error}
                    emptyLabel={t("detail.notFound")}
                    onRetry={() =>
                      void (skill
                        ? currentVersionQuery.refetch()
                        : detailQuery.refetch())
                    }
                  />
                ) : (
                  <SkillInstructionsPanel
                    content={instructions}
                    name={skill?.name}
                    embedded
                  />
                )}
              </div>
              <div hidden={tab !== "resources"}>
                {currentVersionQuery.data && currentVersionQuery.isError && (
                  <QueryRefreshError
                    error={currentVersionQuery.error}
                    pending={currentVersionQuery.isFetching}
                    onRetry={() =>
                      void (skill
                        ? currentVersionQuery.refetch()
                        : detailQuery.refetch())
                    }
                  />
                )}
                {currentVersionQuery.data ? (
                  <SkillResourceBrowser
                    active={active && tab === "resources"}
                    key={`${skillId}:${currentVersionId}`}
                    resources={resources}
                    instructions={instructions}
                    wakerId={wakerId}
                    skillId={skillId}
                    versionId={currentVersionId}
                    currentVersion={currentVersionNumber}
                    pending={createVersion.isPending}
                    error={createVersion.isError}
                    onSave={saveVersion}
                  />
                ) : (
                  <QueryState
                    loadingLayout="content"
                    pending={
                      detailQuery.isPending ||
                      Boolean(
                        skill &&
                        currentVersionId &&
                        currentVersionQuery.isPending,
                      )
                    }
                    error={detailQuery.error ?? currentVersionQuery.error}
                    emptyLabel={t("detail.notFound")}
                    onRetry={() =>
                      void (skill
                        ? currentVersionQuery.refetch()
                        : detailQuery.refetch())
                    }
                  />
                )}
              </div>
              <div hidden={tab !== "version"}>
                {versionsQuery.data && versionsQuery.isError && (
                  <QueryRefreshError
                    error={versionsQuery.error}
                    pending={versionsQuery.isFetching}
                    onRetry={() => void versionsQuery.refetch()}
                  />
                )}
                {!versionsQuery.data ? (
                  <QueryState
                    loadingLayout="content"
                    pending={
                      detailQuery.isPending ||
                      Boolean(skill && versionsQuery.isPending)
                    }
                    error={detailQuery.error ?? versionsQuery.error}
                    emptyLabel={t("detail.notFound")}
                    onRetry={() =>
                      void (skill
                        ? versionsQuery.refetch()
                        : detailQuery.refetch())
                    }
                  />
                ) : (
                  <SkillVersionHistory
                    currentVersionId={currentVersionId}
                    currentVersion={currentVersionNumber}
                    onVersionClick={(version) =>
                      version.id && setDiffVersionId(version.id)
                    }
                    versions={versionsQuery.data ?? []}
                  />
                )}
              </div>
            </section>
            {skill && (
              <SkillMetadataCard
                category={
                  skill.categories?.join("、") || t("category.uncategorized")
                }
                description={metadataDescription}
                source={sourceLabel}
                sourceURL={skill.gitUrl}
                version={currentVersionNumber}
                updatedAt={skill.updatedAt}
              />
            )}
            {skill && !wakerId && (
              <DetailSection
                appearance="catalog"
                title={t("detail.dangerZone")}
              >
                <ResourceCatalogRow className="flex-wrap">
                  <div className="min-w-0 flex-1 basis-60 space-y-1">
                    <h3 className="type-resource-body text-resource-content font-medium">
                      {t("detail.delete")}
                    </h3>
                    <p className="type-resource-caption text-resource-muted">
                      {t("detail.deleteHelp", {
                        count: detailQuery.data?.affectedWakerCount ?? 0,
                      })}
                    </p>
                  </div>
                  <Button
                    size="catalog"
                    variant="catalog-danger"
                    onClick={() => setDeleteOpen(true)}
                  >
                    {t("detail.delete")}
                  </Button>
                </ResourceCatalogRow>
              </DetailSection>
            )}
          </>
        </main>
      </ResourcePageSurface>

      <SkillVersionDiffDialog
        key={diffVersionId ?? "closed"}
        applying={restoreVersion.isPending}
        currentContent={instructions}
        currentVersionId={currentVersionId}
        currentResources={currentVersionQuery.data?.resources ?? []}
        currentVersion={currentVersionNumber}
        onApply={() => {
          if (!diffVersionId) return;
          restoreVersion.mutate({
            path: {
              wakerId,
              skillId,
              versionId: diffVersionId,
            },
            body: {
              notes: "",
            },
          });
        }}
        onOpenChange={(open) => {
          if (!open) setDiffVersionId(null);
        }}
        open={Boolean(diffVersionId && selectedDiffVersion)}
        selectedVersion={selectedDiffVersion}
        skillId={skillId}
        wakerId={wakerId}
        theme={resolvedTheme}
        t={t}
      />
      {deleteOpen || removingPartner ? (
        <SkillDeleteDialog
          key={skillId}
          open
          skills={[
            {
              affectedWakerCount: detailQuery.data?.affectedWakerCount ?? 1,
              id: skillId,
              name: skill?.name ?? "",
            },
          ]}
          wakerId={removingPartner?.id ?? wakerId}
          partnerName={removingPartner?.name ?? currentPartner?.name}
          onOpenChange={(open) => {
            if (!open) setRemovingPartner(null);
            setDeleteOpen(open);
          }}
          onDeleted={() => {
            if (wakerId || !removingPartner) navigate(breadcrumbBack);
          }}
        />
      ) : null}
    </WakerDetailLayout>
  );
}
