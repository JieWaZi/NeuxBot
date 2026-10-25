import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useQueryEvents } from "@/shared/api/use-query-events";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useLocation } from "react-router-dom";

import { WakerDetailLayout } from "@/modules/waker";
import { PageHeader } from "@/shared/ui/page-header";
import { PageAction, ResourcePageSurface } from "@/shared/ui/resource-page";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { QueryState } from "@/shared/ui/query-state";
import { Skeleton } from "@/shared/ui/skeleton";
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
import { SkillDeleteDialog } from "../components/skill-delete-dialog";
import { SkillDisplayNameEditor } from "../components/skill-display-name-editor";
import { SkillGitPullDialog } from "../components/skill-git-pull-dialog";
import { SkillIcon } from "../components/skill-icon";
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
  const queryClient = useQueryClient();
  const { resolvedTheme } = useTheme();
  const [tab, setTab] = useState<DetailTab>("explanation");
  const [diffVersionId, setDiffVersionId] = useState<string | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
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
    enabled: Boolean(skillId && currentVersionId),
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
    enabled: Boolean(diffVersionId),
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
    onClick: () => navigate(back),
  };

  const currentVersionNumber = currentVersion?.version ?? "";
  const sourceLabel =
    skill?.source === "git"
      ? t("source.publicGit")
      : skill?.source === "zip"
        ? t("source.zipImport")
        : t("source.allyBuiltIn");
  const scopeLabel =
    skill?.builtIn || skill?.source === "builtin"
      ? t("scope.built_in")
      : skill?.scope === "public"
        ? t("scope.public")
        : t("scope.private");
  const instructions = currentVersionQuery.data?.content ?? "";
  const metadataName = currentVersion?.name ?? "";
  const metadataDescription =
    currentVersion?.description ?? skill?.description ?? "";
  const builtIn = Boolean(skill?.builtIn || skill?.source === "builtin");
  const saveVersion = (
    body: Parameters<typeof createVersion.mutate>[0]["body"],
    onSuccess?: () => void,
  ) =>
    createVersion.mutate({ path: { wakerId, skillId }, body }, { onSuccess });

  return (
    <WakerDetailLayout active="skills">
      <ResourcePageSurface>
        <PageHeader
          layout="detail"
          backAction={backAction}
          visual={<SkillIcon />}
          title={skill?.name ?? t("title")}
          actions={
            skill && (
              <div className="grid max-w-full justify-items-end gap-2">
                <div className="flex max-w-full flex-wrap items-center gap-2">
                  {wakerId && (
                    <>
                      <Switch
                        hitArea="comfortable"
                        checked={detailQuery.data?.enabled ?? false}
                        disabled={toggleSkill.isPending}
                        aria-label={t(
                          detailQuery.data?.enabled
                            ? "detail.disable"
                            : "detail.enable",
                          { name: skill?.name },
                        )}
                        onClick={() =>
                          toggleSkill.mutate({
                            path: { wakerId, skillId },
                            body: {
                              enabled: !(detailQuery.data?.enabled ?? false),
                            },
                          })
                        }
                      />
                      <span className="text-content-secondary type-body">
                        {detailQuery.data?.enabled
                          ? t("enabled")
                          : t("disabled")}
                      </span>
                    </>
                  )}
                  <SkillDisplayNameEditor
                    name={skill?.name ?? ""}
                    pending={updateSkill.isPending}
                    error={updateSkill.error}
                    onSave={(name, onSuccess) =>
                      updateSkill.mutate(
                        { path: { wakerId, skillId }, body: { name } },
                        { onSuccess },
                      )
                    }
                  />
                  {!builtIn ? (
                    <PageAction
                      intent="delete"
                      label={t("detail.uninstall")}
                      onClick={() => setDeleteOpen(true)}
                    >
                      <Trash2 />
                    </PageAction>
                  ) : null}
                  {skill?.source === "git" ? (
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
                  ) : null}
                </div>
                {toggleSkill.isError ? (
                  <p role="alert" className="text-destructive type-caption">
                    {localApiErrorMessage(toggleSkill.error, t("toggleFailed"))}
                  </p>
                ) : null}
              </div>
            )
          }
        >
          {skill && (
            <div className="mt-2 flex items-center gap-2">
              <span className="bg-muted text-muted-foreground type-caption shrink-0 rounded-full px-2 py-1">
                v{currentVersionNumber}
              </span>
              <span className="bg-muted text-muted-foreground type-caption shrink-0 rounded-full px-2 py-1">
                {scopeLabel}
              </span>
            </div>
          )}
        </PageHeader>

        <main className="min-w-0">
          {skill && detailQuery.isError && (
            <QueryState
              error={detailQuery.error}
              onRetry={() => void detailQuery.refetch()}
            />
          )}
          {skill ? (
            <SkillMetadataCard
              name={metadataName}
              description={metadataDescription}
              source={sourceLabel}
            />
          ) : detailQuery.isPending ? (
            <Skeleton className="mt-6 h-40 w-full" />
          ) : null}
          <section className="mt-8">
            <SegmentedControl
              value={tab}
              onValueChange={setTab}
              ariaLabel={t("detail.tabs")}
              size="sm"
              options={[
                { value: "explanation", label: t("detail.explanation") },
                {
                  value: "resources",
                  label: `${t("detail.resources")} ${countSkillResourceFiles(resources)}`,
                },
                {
                  value: "version",
                  label: t("detail.currentVersionTab", {
                    version: currentVersionNumber,
                  }),
                },
              ]}
            />
            {!skill ? (
              <div className="mt-4">
                <QueryState
                  loadingLayout="detail"
                  pending={detailQuery.isPending}
                  error={detailQuery.error}
                  emptyLabel={t("detail.notFound")}
                  onRetry={() => void detailQuery.refetch()}
                />
              </div>
            ) : (
              <div className="mt-4">
                <div hidden={tab !== "explanation"}>
                  {!currentVersionQuery.data ? (
                    <QueryState
                      loadingLayout="content"
                      pending={currentVersionQuery.isPending}
                      error={currentVersionQuery.error}
                      onRetry={() => void currentVersionQuery.refetch()}
                    />
                  ) : (
                    <SkillInstructionsPanel content={instructions} />
                  )}
                </div>
                <div hidden={tab !== "resources"}>
                  {currentVersionQuery.data ? (
                    <SkillResourceBrowser
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
                      pending={currentVersionQuery.isPending}
                      error={currentVersionQuery.error}
                      onRetry={() => void currentVersionQuery.refetch()}
                    />
                  )}
                </div>
                <div hidden={tab !== "version"}>
                  {!versionsQuery.data ? (
                    <QueryState
                      loadingLayout="content"
                      pending={versionsQuery.isPending}
                      error={versionsQuery.error}
                      onRetry={() => void versionsQuery.refetch()}
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
              </div>
            )}
          </section>
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
      {deleteOpen ? (
        <SkillDeleteDialog
          key={skillId}
          open
          skills={[
            {
              affectedWakerCount: detailQuery.data?.affectedWakerCount ?? 1,
              id: skillId,
              name: skill?.name ?? "",
              scope: skill?.scope === "public" ? "public" : "private",
            },
          ]}
          wakerId={wakerId}
          onOpenChange={setDeleteOpen}
          onDeleted={() => navigate(back)}
        />
      ) : null}
    </WakerDetailLayout>
  );
}
