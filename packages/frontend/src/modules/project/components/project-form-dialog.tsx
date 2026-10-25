import { useMutation, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { Folder, FolderPlus, LoaderCircle, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  createProjectMutation,
  getProjectQueryKey,
  listProjectsQueryKey,
  updateProjectMutation,
} from "../api/project-queries";
import type { Project } from "../model/types";
import { ApiError } from "@/shared/api/errors";
import { selectDirectory } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";
import { Switch } from "@/shared/ui/switch";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved?: (projectId: string) => void;
  project?: Project;
  wakerId: string;
};

type ProjectDirectoryForm = {
  path: string;
  loadInstructions: boolean;
  isDefault: boolean;
};

export function ProjectFormDialog(props: Props) {
  if (!props.open) return null;
  return <OpenProjectFormDialog {...props} />;
}

function OpenProjectFormDialog({
  onOpenChange,
  onSaved,
  open,
  project,
  wakerId,
}: Props) {
  const { t } = useTranslation("project");
  const queryClient = useQueryClient();
  const initialDirectories = (project?.sources ?? []).flatMap((source) =>
    source.path
      ? [
          {
            path: source.path,
            loadInstructions: source.loadInstructions ?? true,
            isDefault: source.isDefault ?? false,
          },
        ]
      : [],
  );
  const [name, setName] = useState(project?.name ?? "");
  const [brief, setBrief] = useState(project?.brief ?? "");
  const [directories, setDirectories] =
    useState<ProjectDirectoryForm[]>(initialDirectories);
  const [localError, setLocalError] = useState("");
  const create = useMutation(withLocalErrorMutation(createProjectMutation()));
  const update = useMutation(withLocalErrorMutation(updateProjectMutation()));
  const pending = create.isPending || update.isPending;

  const addSource = async () => {
    const path = await selectDirectory();
    if (!path) return;
    if (directories.some((directory) => directory.path === path)) {
      setLocalError(t("form.duplicateDirectory"));
      return;
    }
    setDirectories((current) => [
      ...current,
      {
        path,
        loadInstructions: true,
        isDefault: current.length === 0,
      },
    ]);
    setLocalError("");
  };

  const removeSource = (path: string) => {
    if (directories.length <= 1) {
      setLocalError(t("form.keepOneDirectory"));
      return;
    }
    setDirectories((current) => {
      const removed = current.find((directory) => directory.path === path);
      const remaining = current.filter((directory) => directory.path !== path);
      if (!removed?.isDefault || remaining.length === 0) return remaining;
      return remaining.map((directory, index) => ({
        ...directory,
        isDefault: index === 0,
      }));
    });
    setLocalError("");
  };

  const setDefaultDirectory = (path: string) => {
    setDirectories((current) =>
      current.map((directory) => ({
        ...directory,
        isDefault: directory.path === path,
      })),
    );
  };

  const toggleDirectoryInstructions = (path: string) => {
    setDirectories((current) =>
      current.map((directory) =>
        directory.path === path
          ? {
              ...directory,
              loadInstructions: !directory.loadInstructions,
            }
          : directory,
      ),
    );
  };

  const save = async () => {
    const normalizedName = name.trim();
    const normalizedBrief = brief.trim();
    if (!normalizedName) {
      setLocalError(t("form.nameRequired"));
      return;
    }
    if (Array.from(normalizedName).length > 80) {
      setLocalError(t("form.nameTooLong"));
      return;
    }
    if (Array.from(normalizedBrief).length > 500) {
      setLocalError(t("form.briefTooLong"));
      return;
    }
    if (directories.length === 0) {
      setLocalError(t("form.directoryRequired"));
      return;
    }
    const body = {
      name: normalizedName,
      brief: normalizedBrief,
      directories,
    };
    try {
      let savedProjectId = project?.id ?? "";
      if (project?.id) {
        await update.mutateAsync({
          path: { projectId: project.id, wakerId },
          body,
        });
      } else {
        const created = await create.mutateAsync({ path: { wakerId }, body });
        savedProjectId = created.projectId ?? "";
      }
      await queryClient.invalidateQueries({
        queryKey: listProjectsQueryKey({ path: { wakerId } }),
      });
      if (savedProjectId) {
        await queryClient.invalidateQueries({
          queryKey: getProjectQueryKey({
            path: { projectId: savedProjectId, wakerId },
          }),
        });
      }
      onOpenChange(false);
      onSaved?.(savedProjectId);
    } catch {
      // React Query 保存错误，由表单就近展示。
    }
  };

  const mutationError = create.error ?? update.error;
  const errorMessage = mutationError
    ? mutationError instanceof ApiError
      ? mutationError.code === "ProjectNameConflict"
        ? t("form.nameConflict")
        : mutationError.message
      : t("form.saveFailed")
    : localError;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        layout="form"
        size="lg"
        title={project ? t("form.editTitle") : t("form.createTitle")}
        description={t("form.description")}
      >
        <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-5">
          <div className="grid gap-4">
            <label className="type-label grid gap-2 font-medium">
              <span>
                {t("form.name")} <b className="text-destructive">*</b>
              </span>
              <Input
                autoFocus
                placeholder={t("form.namePlaceholder")}
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
              <span className="text-muted-foreground type-caption font-normal">
                {t("form.nameHelp")}
              </span>
            </label>

            <label className="type-label grid gap-2 font-medium">
              {t("form.brief")}
              <Textarea
                placeholder={t("form.briefPlaceholder")}
                value={brief}
                maxLength={500}
                onChange={(event) => setBrief(event.target.value)}
              />
              <span className="text-muted-foreground type-caption text-right font-normal">
                {Array.from(brief).length}/500
              </span>
            </label>

            <section aria-labelledby="project-sources-title">
              <div className="min-h-control-sm flex items-center gap-2">
                <div>
                  <h3
                    id="project-sources-title"
                    className="type-label font-medium"
                  >
                    {t("form.directories")}{" "}
                    <b className="text-destructive">*</b>
                  </h3>
                  <p className="text-muted-foreground type-caption mt-1">
                    {t("form.directoriesHelp")}
                  </p>
                </div>
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  className="ml-auto"
                  onClick={() => void addSource()}
                >
                  <FolderPlus />
                  {t("form.addDirectory")}
                </Button>
              </div>

              <div className="mt-3 grid gap-2">
                {directories.length === 0 ? (
                  <button
                    type="button"
                    onClick={() => void addSource()}
                    className="border-border text-muted-foreground hover:border-border-strong hover:text-foreground type-body min-h-20 rounded-lg border border-dashed px-4 transition-colors"
                  >
                    {t("form.chooseFirstDirectory")}
                  </button>
                ) : (
                  directories.map((directory) => {
                    const { path } = directory;
                    return (
                      <div
                        key={path}
                        className="bg-muted/60 min-h-row-rich grid grid-cols-[20px_minmax(0,1fr)_auto] items-center gap-3 rounded-md px-3 py-2"
                      >
                        <Folder
                          className="text-muted-foreground size-icon-md"
                          strokeWidth={1.5}
                        />
                        <div className="min-w-0">
                          <p className="type-body truncate font-medium">
                            {path.split(/[\\/]/).pop() || path}
                          </p>
                          <p className="text-muted-foreground type-caption truncate">
                            {path}
                          </p>
                        </div>
                        <div className="flex items-center gap-2">
                          {directory.isDefault ? (
                            <span className="bg-primary/10 text-primary type-caption rounded-xs px-1.5 py-0.5">
                              {t("form.defaultDirectory")}
                            </span>
                          ) : (
                            <Button
                              type="button"
                              size="sm"
                              variant="ghost"
                              onClick={() => setDefaultDirectory(path)}
                            >
                              {t("form.setDefaultDirectory")}
                            </Button>
                          )}
                          <div className="type-caption flex items-center gap-2 whitespace-nowrap">
                            {t("form.loadDirectoryInstructions")}
                            <Switch
                              checked={directory.loadInstructions}
                              aria-label={t(
                                "form.loadDirectoryInstructionsFor",
                                { path },
                              )}
                              onClick={() => toggleDirectoryInstructions(path)}
                            />
                          </div>
                          <Button
                            type="button"
                            size="icon-sm"
                            variant="ghost"
                            aria-label={t("form.removeDirectory", { path })}
                            onClick={() => removeSource(path)}
                          >
                            <Trash2 />
                          </Button>
                        </div>
                      </div>
                    );
                  })
                )}
              </div>
            </section>

            {errorMessage ? (
              <p role="alert" className="text-destructive type-body">
                {errorMessage}
              </p>
            ) : null}
          </div>
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="secondary"
            onClick={() => onOpenChange(false)}
          >
            {t("form.cancel")}
          </Button>
          <Button type="button" disabled={pending} onClick={() => void save()}>
            {pending ? <LoaderCircle className="animate-spin" /> : null}
            {t("form.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
