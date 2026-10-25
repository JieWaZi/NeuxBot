import { useTranslation } from "react-i18next";
import { cn } from "@/shared/lib/cn";
import type { Manifest, WorkflowInstance } from "../model/types";
import { instanceTree, type InstanceBranch } from "../model/instance-tree";
import { NodeKind } from "./workflow-canvas";

export function InstanceTree({
  instances,
  manifest,
  selected,
  onSelect,
}: {
  instances: WorkflowInstance[];
  manifest?: Manifest;
  selected?: string;
  onSelect: (id: string) => void;
}) {
  const { t } = useTranslation("workflow");
  const branches = instanceTree(instances, manifest);
  const title = (branch: InstanceBranch) => {
    if (branch.kind === "phase")
      return (
        manifest?.meta?.phases?.find((phase) => phase.id === branch.segment)
          ?.title ||
        branch.segment ||
        t("phase")
      );
    if (branch.kind === "item")
      return t("inputItem", { number: (branch.ordinal ?? 0) + 1 });
    if (branch.kind === "iteration")
      return t("iteration", {
        id: branch.segment,
        number: branch.ordinal,
      });
    const group = manifest?.groups?.find(
      (group) => group.id === branch.segment,
    );
    return `${group?.kind === "pipeline" ? t("pipeline") : t("parallel")} · ${branch.segment}`;
  };
  const renderBranch = (branch: InstanceBranch) => (
    <details key={branch.key} open className="grid gap-2">
      <summary className="text-muted-foreground type-caption focus-visible:ring-ring cursor-pointer rounded-md px-2 py-2 outline-none focus-visible:ring-2">
        {title(branch)}
      </summary>
      <div className="grid gap-2 pl-3">
        {branch.instances.map((instance) => (
          <button
            key={instance.id}
            type="button"
            aria-pressed={selected === instance.id}
            onClick={() => {
              if (instance.id) onSelect(instance.id);
            }}
            className={cn(
              "focus-visible:ring-ring hover:bg-surface-subtle grid gap-2 rounded-lg p-3 text-left outline-none focus-visible:ring-2",
              selected === instance.id && "bg-selection-surface",
            )}
          >
            <span className="flex flex-wrap items-center gap-2">
              <NodeKind kind={instance.kind ?? ""} />
              {manifest?.groups?.some(
                (group) =>
                  group.kind === "pipeline" &&
                  instance.path?.split("/").includes(group.id ?? ""),
              ) ? (
                <span className="text-muted-foreground type-caption">
                  {t("pipelineStage", {
                    number: (instance.stageIndex ?? 0) + 1,
                  })}
                </span>
              ) : null}
            </span>
            <span className="type-body font-medium">{instance.label}</span>
            <span className="text-muted-foreground type-caption">
              {instance.status ? t(`status.${instance.status}`) : ""}
            </span>
            <code className="text-muted-foreground type-caption break-all">
              {instance.path}
            </code>
          </button>
        ))}
        {branch.children.map(renderBranch)}
      </div>
    </details>
  );
  return (
    <div className="bg-surface-panel grid gap-3 rounded-xl p-3">
      {branches.map(renderBranch)}
    </div>
  );
}
