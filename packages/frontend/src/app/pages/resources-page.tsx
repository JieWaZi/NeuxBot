import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Package, Sparkles, Cable, Plus, Workflow } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import {
  PluginResourceCollection,
  PluginComponentCollection,
  usePluginComponents,
  PartnerPicker,
  resourceCategory,
} from "@/modules/plugin";
import { SkillResourceCollection } from "@/modules/skill";
import { WorkflowResourceCollection } from "@/modules/workflow";
import { ConnectorResourceCollection } from "@/modules/connector";
import {
  ResourcePageSurface,
  ResourcePageHeader,
  ResourcePageSearchField,
  PageActionLabel,
  PageAction,
} from "@/shared/ui/resource-page";
import { ResourceFilterSelect } from "@/shared/ui/resource-filter-select";
import { StatusNotice } from "@/shared/ui/status-notice";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import {
  installLibrarySkillMutation,
  installLibraryConnectorMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  withLocalErrorMutation,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";

type Installation = { kind: "skill" | "connector"; id: string; name: string };
export function ResourcesPage() {
  const { t } = useTranslation("plugin");
  const { wakerId = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const category =
    !wakerId && params.get("category") === "workflows"
      ? "workflows"
      : resourceCategory(params.get("category"));
  const search = params.get("search") ?? "";
  const source: "all" | "plugins" | "independent" =
    params.get("source") === "plugins"
      ? "plugins"
      : params.get("source") === "independent"
        ? "independent"
        : "all";
  const view = params.get("view") === "mine" ? "mine" : "catalog";
  const [adding, setAdding] = useState(false);
  const components = usePluginComponents(
    category === "connectors" ? "connectors" : "skills",
    wakerId,
    search,
    category === "skills" || category === "connectors",
  );
  const [installation, setInstallation] = useState<Installation | null>(null);
  const setParam = (key: string, value: string) =>
    setParams((current) => {
      const next = new URLSearchParams(current);
      if (value) next.set(key, value);
      else next.delete(key);
      next.delete("page");
      next.delete("componentPage");
      return next;
    });

  const searchField = (
    <ResourcePageSearchField
      value={search}
      aria-label={category === "workflows" ? t("workflow:search") : t("search")}
      placeholder={
        category === "workflows" ? t("workflow:search") : t("search")
      }
      onChange={(event) => setParam("search", event.target.value)}
    />
  );
  const setSource = (value: "all" | "plugins" | "independent") =>
    setParam("source", value === "all" ? "" : value);
  const capabilityProps = {
    source,
    onSourceChange: setSource,
    filters: (
      <>
        <ResourceFilterSelect
          value={source}
          onValueChange={setSource}
          ariaLabel={t("sourceFilter")}
          options={[
            { value: "all", label: t("allSources") },
            { value: "plugins", label: t("fromPlugins") },
            { value: "independent", label: t("independentResources") },
          ]}
        />
        {searchField}
      </>
    ),
    pluginCount: components.items.length,
    pluginEmpty: components.isSuccess && components.items.length === 0,
    pluginContent: source !== "independent" && (
      <PluginComponentCollection
        key={`${category}:${search}:${source}`}
        kind={category === "skills" ? "skills" : "connectors"}
        wakerId={wakerId}
        resources={components}
        preview={source === "all"}
        onSourceSelect={() => setSource("plugins")}
      />
    ),
  };
  const targetPartner = params.get("partner") || undefined;
  return (
    <main
      className="bg-surface-scene h-full min-w-0 overflow-hidden"
      data-partner-section={wakerId ? "resources" : undefined}
    >
      <ResourcePageSurface>
        <ResourcePageHeader
          title={wakerId ? t("partnerResources") : t("title")}
          description={t(wakerId ? "description" : "libraryDescription")}
          actions={
            category !== "plugins" && (
              <PageAction
                intent="primary"
                aria-label={t(
                  category === "workflows"
                    ? "workflow:create"
                    : category === "skills"
                      ? "addSkill"
                      : "addConnector",
                )}
                onClick={() =>
                  category === "workflows"
                    ? navigate(`/workflows/new?draftId=${crypto.randomUUID()}`)
                    : setAdding(true)
                }
              >
                <Plus />
                <PageActionLabel>
                  {t(
                    category === "workflows"
                      ? "workflow:create"
                      : category === "skills"
                        ? "addSkill"
                        : "addConnector",
                  )}
                </PageActionLabel>
              </PageAction>
            )
          }
        />
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
              ...(!wakerId
                ? [
                    {
                      value: "workflows",
                      label: t("workflow:title"),
                      icon: <Workflow />,
                      description: t("workflowsHelp"),
                    },
                  ]
                : []),
            ]}
            onValueChange={(value) =>
              setParams((current) => {
                const next = new URLSearchParams(current);
                next.set("category", value);
                next.delete("page");
                next.delete("search");
                next.delete("view");
                next.delete("componentPage");
                next.delete("source");
                setAdding(false);
                return next;
              })
            }
          />
        </div>
        <div key={category} className="space-y-5">
          {category === "plugins" ? (
            <PluginResourceCollection
              wakerId={wakerId}
              targetPartnerId={targetPartner}
              searchField={searchField}
              search={search}
              view={view}
              onViewChange={(value) => setParam("view", value)}
            />
          ) : category === "skills" ? (
            <SkillResourceCollection
              {...capabilityProps}
              wakerId={wakerId}
              adding={adding}
              onAddingChange={setAdding}
              search={search}
              onInstall={(id, name) =>
                setInstallation({ kind: "skill", id, name })
              }
            />
          ) : category === "workflows" ? (
            <WorkflowResourceCollection search={search} filters={searchField} />
          ) : (
            <ConnectorResourceCollection
              {...capabilityProps}
              wakerId={wakerId}
              adding={adding}
              onAddingChange={setAdding}
              search={search}
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
    </main>
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
  const skill = useMutation(
    withLocalErrorMutation(installLibrarySkillMutation()),
  );
  const connector = useMutation(
    withLocalErrorMutation(installLibraryConnectorMutation()),
  );
  const pending = skill.isPending || connector.isPending;
  const error = skill.error ?? connector.error;
  const install = async () => {
    try {
      if (resource.kind === "skill")
        await skill.mutateAsync({
          path: { skillId: resource.id },
          body: { wakerIds: selected },
        });
      else
        await connector.mutateAsync({
          path: { connectorId: resource.id },
          body: { wakerIds: selected },
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
          ) : (
            <PartnerPicker
              selected={selected}
              onChange={setSelected}
              onlyPartner={targetPartner}
            />
          )}{" "}
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
            <Button
              disabled={pending || selected.length === 0}
              onClick={() => void install()}
            >
              {t("install")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
