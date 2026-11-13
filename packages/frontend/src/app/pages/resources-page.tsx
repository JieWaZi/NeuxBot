import { WakerDetailLayout } from "@/modules/waker";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Package, Sparkles, Cable, Plus } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { PluginResourceCollection, PartnerPicker } from "@/modules/plugin";
import { SkillResourceCollection } from "@/modules/skill";
import { WorkflowResourceCollection } from "@/modules/workflow";
import { ConnectorResourceCollection } from "@/modules/connector";
import {
  ResourcePageSurface,
  ResourcePageHeader,
} from "@/shared/ui/resource-page";
import { SearchField } from "@/shared/ui/search-field";
import { SceneBreadcrumbs } from "@/shared/ui/scene-breadcrumbs";
import { StatusNotice } from "@/shared/ui/status-notice";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { QueryRefreshError, QueryState } from "@/shared/ui/query-state";
import {
  getLibrarySkillOptions,
  installLibrarySkillMutation,
  installLibraryConnectorMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  withLocalErrorSurface,
  withLocalErrorMutation,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";

import {
  changeResourceCategory,
  resourceCategoryFromParams,
} from "./resources-navigation";

type Installation = { kind: "skill" | "connector"; id: string; name: string };
export function ResourcesPage() {
  const { t } = useTranslation("plugin");
  const { wakerId = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const location = useLocation();
  const category = resourceCategoryFromParams(params, wakerId);
  const search = params.get("search") ?? "";
  const skillCategory = params.get("skillCategory");
  const skillCategoryName = skillCategory || t("skill:category.uncategorized");
  const skillSearchLabel =
    skillCategory === null
      ? t("skill:search")
      : t("skill:category.searchIn", { name: skillCategoryName });
  const skillLibraryParams = new URLSearchParams(params);
  skillLibraryParams.delete("skillCategory");
  skillLibraryParams.delete("source");
  const skillLibraryHref = `${wakerId ? `/wakers/${wakerId}/resources` : "/resources"}?${skillLibraryParams}`;
  const [installation, setInstallation] = useState<Installation | null>(null);
  const setParam = (key: string, value: string) =>
    setParams((current) => {
      const next = new URLSearchParams(current);
      if (value) next.set(key, value);
      else next.delete(key);
      next.delete("page");
      next.delete("componentPage");
      next.delete("source");
      return next;
    });

  const searchField = (
    <SearchField
      appearance="catalog"
      size="md"
      width="full"
      value={search}
      aria-label={
        category === "skills"
          ? skillSearchLabel
          : t(
              category === "connectors"
                ? "connector:search"
                : category === "workflows"
                  ? "workflow:search"
                  : "searchPlugins",
            )
      }
      placeholder={
        category === "skills"
          ? skillSearchLabel
          : t(
              category === "connectors"
                ? "connector:search"
                : category === "workflows"
                  ? "workflow:search"
                  : "searchPlugins",
            )
      }
      onChange={(event) => setParam("search", event.target.value)}
    />
  );
  const skillAddAction = (
    <Button
      variant="catalog-primary"
      size="catalog"
      className="shrink-0 whitespace-nowrap"
      onClick={() =>
        navigate(
          `${wakerId ? `/wakers/${wakerId}/resources` : "/resources"}/skills/new?${new URLSearchParams({ returnTo: location.pathname + location.search })}`,
        )
      }
    >
      <Plus />
      {t("addSkill")}
    </Button>
  );
  const connectorAddAction = (
    <Button
      variant="catalog-primary"
      size="catalog"
      className="shrink-0 whitespace-nowrap"
      onClick={() =>
        navigate(
          `${wakerId ? `/wakers/${wakerId}/resources` : "/resources"}/connectors/new?${new URLSearchParams({ returnTo: location.pathname + location.search })}`,
        )
      }
    >
      <Plus />
      {t("addConnector")}
    </Button>
  );
  const workflowCreateAction = (
    <Button
      variant="catalog-primary"
      size="catalog"
      className="shrink-0 whitespace-nowrap"
      onClick={() =>
        navigate(`/workflows/new?draftId=${crypto.randomUUID()}`, {
          state: { resourceBack: location.pathname + location.search },
        })
      }
    >
      <Plus />
      {t("workflow:create")}
    </Button>
  );
  const targetPartner = params.get("partner") || undefined;
  return (
    <WakerDetailLayout
      active="resources"
      showBreadcrumbs={
        Boolean(wakerId) && !(category === "skills" && skillCategory !== null)
      }
      breadcrumbItems={[
        { label: t(category === "workflows" ? "workflow:title" : category) },
      ]}
    >
      {(!wakerId || (category === "skills" && skillCategory !== null)) && (
        <SceneBreadcrumbs
          ariaLabel={t(category === "workflows" ? "workflow:title" : category)}
          items={
            category === "skills" && skillCategory !== null
              ? [
                  { label: t("skills"), href: skillLibraryHref },
                  { label: skillCategoryName },
                ]
              : [
                  {
                    label: t(
                      category === "workflows" ? "workflow:title" : category,
                    ),
                  },
                ]
          }
        />
      )}
      <ResourcePageSurface appearance="catalog">
        <ResourcePageHeader
          appearance="catalog"
          title={
            category === "skills" && skillCategory !== null
              ? skillCategory || t("skill:category.uncategorized")
              : wakerId
                ? t("partnerResources")
                : t(category === "workflows" ? "workflow:title" : category)
          }
          description={
            category === "plugins"
              ? t("browseDescription")
              : category === "skills"
                ? skillCategory === null
                  ? t("skill:libraryDescription")
                  : undefined
                : category === "connectors"
                  ? t(
                      wakerId
                        ? "connector:description"
                        : "connector:libraryDescription",
                    )
                  : t("workflow:subtitle")
          }
        />
        {wakerId ? (
          <div className="mb-5">
            <SegmentedControl
              appearance="resources"
              ariaLabel={t("title")}
              value={category}
              options={[
                {
                  value: "plugins",
                  label: t("plugins"),
                  icon: <Package />,
                  description: t("pluginsHelp"),
                },
                {
                  value: "skills",
                  label: t("skills"),
                  icon: <Sparkles />,
                  description: t("skillsHelp"),
                },
                {
                  value: "connectors",
                  label: t("connectors"),
                  icon: <Cable />,
                  description: t("connectorsHelp"),
                },
              ]}
              onValueChange={(value) =>
                setParams((current) => {
                  const next = changeResourceCategory(current, value);
                  return next;
                })
              }
            />
          </div>
        ) : null}
        <div key={category} className="space-y-5">
          {category === "plugins" ? (
            <PluginResourceCollection
              wakerId={wakerId}
              targetPartnerId={targetPartner}
              searchField={searchField}
              search={search}
            />
          ) : category === "skills" ? (
            <SkillResourceCollection
              wakerId={wakerId}
              search={search}
              filters={
                <>
                  <div className="basis-field-search-min min-w-0 grow">
                    {searchField}
                  </div>
                  {skillCategory === null && skillAddAction}
                </>
              }
              category={skillCategory}
              onCategoryChange={(value) =>
                setParams((current) => {
                  const next = new URLSearchParams(current);
                  next.delete("source");
                  if (value === null) next.delete("skillCategory");
                  else next.set("skillCategory", value);
                  return next;
                })
              }
              onInstall={(id, name) =>
                setInstallation({ kind: "skill", id, name })
              }
            />
          ) : category === "workflows" ? (
            <WorkflowResourceCollection
              search={search}
              filters={
                <>
                  <div className="basis-field-search-min min-w-0 grow">
                    {searchField}
                  </div>
                  {workflowCreateAction}
                </>
              }
            />
          ) : (
            <ConnectorResourceCollection
              wakerId={wakerId}
              search={search}
              filters={
                <>
                  <div className="basis-field-search-min min-w-0 grow">
                    {searchField}
                  </div>
                  {connectorAddAction}
                </>
              }
              onInstall={(id, name) =>
                setInstallation({ kind: "connector", id, name })
              }
            />
          )}
        </div>
        {installation && (
          <IndependentInstallDialog
            resource={installation}
            targetPartner={targetPartner}
            onClose={() => setInstallation(null)}
          />
        )}
      </ResourcePageSurface>
    </WakerDetailLayout>
  );
}
function IndependentInstallDialog({
  resource,
  targetPartner,
  onClose,
}: {
  resource: Installation;
  targetPartner?: string;
  onClose: () => void;
}) {
  const { t } = useTranslation("plugin");
  const client = useQueryClient();
  const [selected, setSelected] = useState<string[]>(
    targetPartner ? [targetPartner] : [],
  );
  const [done, setDone] = useState(false);
  const isSkill = resource.kind === "skill";
  const skillDetail = useQuery({
    ...getLibrarySkillOptions(
      withLocalErrorSurface({ path: { skillId: resource.id } }),
    ),
    enabled: isSkill && !done,
    refetchOnMount: "always",
  });
  const skill = useMutation(
    withLocalErrorMutation(installLibrarySkillMutation()),
  );
  const connector = useMutation(
    withLocalErrorMutation(installLibraryConnectorMutation()),
  );
  const pending = skill.isPending || connector.isPending;
  const error = skill.error ?? connector.error;
  const installed = isSkill
    ? (skillDetail.data?.installedPartners?.flatMap(({ id }) =>
        id ? [id] : [],
      ) ?? [])
    : [];
  const wakerIds = selected.filter((id) => !installed.includes(id));
  const hasPartnerStatus =
    !isSkill ||
    (skillDetail.isFetchedAfterMount && skillDetail.data !== undefined);
  const canChoosePartners =
    !pending &&
    hasPartnerStatus &&
    (!isSkill || (!skillDetail.isError && skillDetail.fetchStatus === "idle"));
  const canInstall = canChoosePartners && wakerIds.length > 0;
  const install = async () => {
    if (!canInstall) return;
    try {
      if (isSkill)
        await skill.mutateAsync({
          path: { skillId: resource.id },
          body: { wakerIds },
        });
      else
        await connector.mutateAsync({
          path: { connectorId: resource.id },
          body: { wakerIds },
        });
      await client.invalidateQueries({
        predicate: (query) =>
          /Skill|Connector/.test(
            (query.queryKey[0] as { _id?: string } | undefined)?._id ?? "",
          ),
      });
      setDone(true);
    } catch {
      /* 错误由安装对话框就近显示。 */
    }
  };
  return (
    <Dialog
      open
      onOpenChange={(value) => {
        if (!value && !pending) onClose();
      }}
    >
      <DialogContent
        title={t("installIndependent")}
        description={resource.name}
      >
        <div className="min-h-0 space-y-4 overflow-y-auto px-6 pb-5">
          {done ? (
            <StatusNotice tone="success">{t("succeeded")}</StatusNotice>
          ) : !hasPartnerStatus ? (
            <QueryState
              loadingLayout="content"
              pending={
                !skillDetail.isFetchedAfterMount || skillDetail.isPending
              }
              error={skillDetail.error}
              onRetry={() => void skillDetail.refetch()}
            />
          ) : (
            <>
              <PartnerPicker
                selected={wakerIds}
                onChange={setSelected}
                onlyPartner={targetPartner}
                installed={installed}
                disabled={isSkill && !canChoosePartners}
              />
              {isSkill && skillDetail.isError && (
                <QueryRefreshError
                  error={skillDetail.error}
                  pending={skillDetail.isFetching}
                  onRetry={() => void skillDetail.refetch()}
                />
              )}
            </>
          )}
          {error && (
            <p role="alert" className="type-body text-destructive">
              {localApiErrorMessage(error, t("failed"))}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" disabled={pending} onClick={onClose}>
            {t(done ? "close" : "cancel")}
          </Button>
          {!done && (
            <Button disabled={!canInstall} onClick={() => void install()}>
              {t("install")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
