import { useQuery } from "@tanstack/react-query";
import { FileText } from "lucide-react";
import { lazy, Suspense, useMemo, useState } from "react";

import type { GithubComJieWaZiNeuxBotServerApisSkillsSkillResourceEntry as SkillResourceEntry } from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
} from "@/shared/ui/dialog";
import { ResourceLoadingState } from "@/shared/ui/resource-loading-state";
import { getSkillVersionFileOptions } from "../api/skill-queries";

const SkillVersionDiffView = lazy(() =>
  import("./skill-version-diff-view").then((module) => ({
    default: module.SkillVersionDiffView,
  })),
);

type Translation = (key: string, options?: Record<string, unknown>) => string;
type Theme = "light" | "dark";

export type DiffSkillVersion = {
  id: string;
  instructions: string;
  resources: SkillResourceEntry[];
  version: string;
};

type Props = {
  currentContent: string;
  currentResources: SkillResourceEntry[];
  currentVersion: string;
  currentVersionId: string;
  applying?: boolean;
  onApply: () => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  selectedVersion: DiffSkillVersion | null;
  skillId: string;
  theme: Theme;
  t: Translation;
  wakerId: string;
};

export function SkillVersionDiffDialog({
  applying,
  currentContent,
  currentResources,
  currentVersion,
  currentVersionId,
  onApply,
  onOpenChange,
  open,
  selectedVersion,
  skillId,
  theme,
  t,
  wakerId,
}: Props) {
  const changedFiles = useMemo(
    () =>
      selectedVersion
        ? changedFilePaths(currentResources, selectedVersion.resources)
        : [],
    [currentResources, selectedVersion],
  );
  const [requestedPath, setRequestedPath] = useState("SKILL.md");
  const selectedPath = changedFiles.includes(requestedPath)
    ? requestedPath
    : (changedFiles[0] ?? "SKILL.md");

  const currentExists = resourceExists(currentResources, selectedPath);
  const selectedExists = resourceExists(
    selectedVersion?.resources ?? [],
    selectedPath,
  );
  const currentFileQuery = useQuery({
    ...getSkillVersionFileOptions({
      path: { wakerId, skillId, versionId: currentVersionId || "pending" },
      query: { path: selectedPath },
    }),
    enabled: Boolean(
      open && selectedPath !== "SKILL.md" && currentVersionId && currentExists,
    ),
  });
  const selectedFileQuery = useQuery({
    ...getSkillVersionFileOptions({
      path: {
        wakerId,
        skillId,
        versionId: selectedVersion?.id || "pending",
      },
      query: { path: selectedPath },
    }),
    enabled: Boolean(
      open &&
      selectedPath !== "SKILL.md" &&
      selectedVersion?.id &&
      selectedExists,
    ),
  });

  const currentFileContent =
    selectedPath === "SKILL.md"
      ? currentContent
      : currentExists
        ? currentFileQuery.data?.content
        : "";
  const selectedFileContent =
    selectedPath === "SKILL.md"
      ? selectedVersion?.instructions
      : selectedExists
        ? selectedFileQuery.data?.content
        : "";
  const loading =
    (selectedPath !== "SKILL.md" &&
      currentExists &&
      currentFileQuery.isPending) ||
    (selectedPath !== "SKILL.md" &&
      selectedExists &&
      selectedFileQuery.isPending);

  if (!selectedVersion) {
    return <Dialog open={false} onOpenChange={onOpenChange} />;
  }

  const isCurrent = selectedVersion.version === currentVersion;
  const nextVersion = suggestNextPatchVersion(currentVersion);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={t("detail.diffTitle")}
        description={t("detail.diffDescription", {
          current: currentVersion,
          selected: selectedVersion.version,
        })}
        size="xl"
        layout="form"
      >
        <div className="grid min-h-0 flex-1 grid-cols-[minmax(180px,240px)_minmax(0,1fr)] overflow-hidden border-y">
          <aside className="border-border min-h-0 overflow-y-auto border-r p-3">
            <h3 className="text-muted-foreground type-caption px-2 pb-2 font-medium">
              {t("detail.changedFiles")}
            </h3>
            {changedFiles.length ? (
              <div className="grid gap-1">
                {changedFiles.map((path) => (
                  <button
                    key={path}
                    type="button"
                    className={cn(
                      "type-body min-h-row-default flex w-full items-center gap-2 rounded-md px-2 text-left",
                      selectedPath === path
                        ? "bg-muted text-foreground"
                        : "hover:bg-muted/60",
                    )}
                    onClick={() => setRequestedPath(path)}
                  >
                    <FileText className="text-muted-foreground size-icon-sm shrink-0" />
                    <span className="min-w-0 break-all">{path}</span>
                  </button>
                ))}
              </div>
            ) : (
              <p className="text-muted-foreground type-caption px-2 py-4">
                {t("detail.noChangedFiles")}
              </p>
            )}
          </aside>
          <div className="min-h-0 overflow-y-auto p-4">
            <Suspense
              fallback={
                <ResourceLoadingState
                  label={t("common:loading")}
                  className="h-96"
                />
              }
            >
              {loading ? (
                <ResourceLoadingState
                  label={t("common:loading")}
                  className="h-96"
                />
              ) : open &&
                selectedVersion &&
                currentFileContent !== undefined &&
                selectedFileContent !== undefined ? (
                <SkillVersionDiffView
                  currentContent={currentFileContent}
                  currentVersion={currentVersion}
                  selectedContent={selectedFileContent}
                  selectedVersion={selectedVersion.version}
                  path={selectedPath}
                  theme={theme}
                />
              ) : null}
            </Suspense>
            {isCurrent ? (
              <p className="text-muted-foreground type-caption mt-3">
                {t("detail.currentVersionDiffHint")}
              </p>
            ) : null}
          </div>
        </div>

        <DialogFooter>
          <DialogClose asChild>
            <Button size="md" variant="secondary">
              {t("common:cancel")}
            </Button>
          </DialogClose>
          {!isCurrent ? (
            <Button size="md" disabled={applying} onClick={onApply}>
              {applying
                ? t("detail.applyingVersion")
                : t("detail.restoreVersion", { version: nextVersion })}
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function changedFilePaths(
  current: SkillResourceEntry[],
  selected: SkillResourceEntry[],
): string[] {
  const currentByPath = fileDigestMap(current);
  const selectedByPath = fileDigestMap(selected);
  return [...new Set([...currentByPath.keys(), ...selectedByPath.keys()])]
    .filter((path) => currentByPath.get(path) !== selectedByPath.get(path))
    .sort((left, right) => {
      if (left === "SKILL.md") return -1;
      if (right === "SKILL.md") return 1;
      return left.localeCompare(right);
    });
}

function fileDigestMap(resources: SkillResourceEntry[]): Map<string, string> {
  return new Map(
    resources.flatMap((resource) =>
      resource.path && !resource.directory
        ? [[resource.path, resource.digest ?? `size:${resource.size ?? 0}`]]
        : [],
    ),
  );
}

function resourceExists(
  resources: SkillResourceEntry[],
  path: string,
): boolean {
  return resources.some(
    (resource) => resource.path === path && !resource.directory,
  );
}

function suggestNextPatchVersion(version: string): string {
  const match = /^(\d+)\.(\d+)\.(\d+)$/.exec(version.trim());
  if (!match) return "1.0.0";
  return `${Number(match[1])}.${Number(match[2])}.${Number(match[3]) + 1}`;
}
