import { SkillResourceRow } from "./skill-resource-row";
import { useQuery } from "@tanstack/react-query";
import {
  Download,
  FilePlus2,
  FolderPlus,
  Move,
  Pencil,
  Save,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type {
  GithubComJieWaZiNeuxBotServerApisSkillsCreateSkillVersionRequest as CreateSkillVersionRequest,
  GithubComJieWaZiNeuxBotServerApisSkillsSkillResourceOperation as SkillResourceOperation,
} from "@/shared/api/generated/types.gen";
import { saveBlob } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { ResourcePageFooter } from "@/shared/ui/resource-page";
import { Input } from "@/shared/ui/input";
import { ResourceLoadingState } from "@/shared/ui/resource-loading-state";
import { Textarea } from "@/shared/ui/textarea";

import { getSkillVersionFileOptions } from "../api/skill-queries";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import {
  countSkillResourceFiles,
  isVersionGreater,
  suggestNextPatchVersion,
} from "../model/skill-detail";
import type { SkillResource } from "../model/types";
import {
  SkillResourceDeleteDialog,
  SkillResourcePathDialog,
} from "./skill-resource-dialogs";

type SkillResourceBrowserProps = {
  active?: boolean;
  currentVersion: string;
  error: boolean;
  instructions: string;
  onSave: (body: CreateSkillVersionRequest, onSuccess?: () => void) => void;
  pending: boolean;
  resources: SkillResource[];
  skillId: string;
  versionId: string;
  wakerId: string;
};

type WorkingEntry = {
  binary?: boolean;
  contentBase64?: string;
  executable?: boolean;
  path: string;
  size?: number;
  sourcePath?: string;
  type: "directory" | "file";
};

type PathAction = "directory" | "file" | "move" | null;

const MAX_RESOURCE_FILE_BYTES = 50 * 1024 * 1024;

export function SkillResourceBrowser({
  currentVersion,
  error,
  instructions,
  onSave,
  pending,
  resources,
  skillId,
  versionId,
  wakerId,
  active = true,
}: SkillResourceBrowserProps) {
  const { t } = useTranslation("skill");
  const baseEntries = useMemo(
    () =>
      flattenResources(resources).map((entry) =>
        entry.path === "SKILL.md"
          ? {
              ...entry,
              contentBase64: bytesToBase64(
                new TextEncoder().encode(instructions),
              ),
              binary: false,
            }
          : entry,
      ),
    [resources, instructions],
  );
  const [entries, setEntries] = useState<WorkingEntry[]>(baseEntries);
  const [operations, setOperations] = useState<SkillResourceOperation[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [editedContent, setEditedContent] = useState("");
  const [pathAction, setPathAction] = useState<PathAction>(null);
  const [pendingPath, setPendingPath] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [saveVersion, setSaveVersion] = useState(() =>
    suggestNextPatchVersion(currentVersion),
  );
  const [saveNotes, setSaveNotes] = useState("");
  const [uploadTooLarge, setUploadTooLarge] = useState(false);
  const uploadInputRef = useRef<HTMLInputElement>(null);

  const workingInstructions = entries.find(
    (entry) => entry.path === "SKILL.md",
  );
  const instructionsContent =
    workingInstructions?.contentBase64 !== undefined
      ? new TextDecoder().decode(
          base64ToBytes(workingInstructions.contentBase64),
        )
      : instructions;
  const instructionsChanged = instructionsContent !== instructions;
  const changeCount = operations.length + (instructionsChanged ? 1 : 0);

  const tree = useMemo(() => buildWorkingTree(entries), [entries]);
  const selectedEntry = selected
    ? entries.find((entry) => entry.path === selected)
    : undefined;
  const fileQuery = useQuery({
    ...getSkillVersionFileOptions(
      withLocalErrorSurface({
        path: { wakerId, skillId, versionId: versionId || "pending" },
        query: { path: selectedEntry?.sourcePath ?? "pending" },
      }),
    ),
    enabled: Boolean(
      selectedEntry?.type === "file" &&
      selectedEntry.sourcePath &&
      selectedEntry.contentBase64 === undefined &&
      versionId,
    ),
  });
  const localBytes =
    selectedEntry?.contentBase64 !== undefined
      ? base64ToBytes(selectedEntry.contentBase64)
      : null;
  const fileBinary = selectedEntry?.binary ?? fileQuery.data?.binary ?? false;
  const fileContent = localBytes
    ? fileBinary
      ? ""
      : new TextDecoder().decode(localBytes)
    : (fileQuery.data?.content ?? "");
  const fileReady = Boolean(localBytes || fileQuery.data);
  const selectedInstructions = selectedEntry?.path === "SKILL.md";
  const normalizedPendingPath = normalizeResourcePath(pendingPath);
  const pathError = Boolean(
    pendingPath.trim() &&
    (!isValidResourcePath(normalizedPendingPath) ||
      normalizedPendingPath === "SKILL.md" ||
      hasFileAncestor(entries, normalizedPendingPath) ||
      (pathAction !== "move" &&
        entries.some((entry) => entry.path === normalizedPendingPath)) ||
      (pathAction === "move" &&
        (!selected ||
          normalizedPendingPath === selected ||
          normalizedPendingPath.startsWith(`${selected}/`) ||
          entries.some((entry) => entry.path === normalizedPendingPath)))),
  );
  const saveVersionValid = isVersionGreater(saveVersion, currentVersion);

  const queueOperation = (operation: SkillResourceOperation) => {
    setOperations((current) => [...current, operation]);
  };
  const openCreateDialog = (action: "directory" | "file") => {
    const parent = selectedEntry
      ? selectedEntry.type === "directory"
        ? selectedEntry.path
        : parentPath(selectedEntry.path)
      : "";
    setPendingPath(parent ? `${parent}/` : "");
    setPathAction(action);
  };
  const confirmPathAction = () => {
    if (!normalizedPendingPath || pathError) return;
    if (pathAction === "directory") {
      setEntries((current) => [
        ...current,
        { path: normalizedPendingPath, type: "directory" },
      ]);
      queueOperation({ action: "mkdir", path: normalizedPendingPath });
      setSelected(normalizedPendingPath);
    } else if (pathAction === "file") {
      setEntries((current) => [
        ...current,
        {
          path: normalizedPendingPath,
          type: "file",
          contentBase64: "",
          binary: false,
          size: 0,
        },
      ]);
      queueOperation({
        action: "write",
        path: normalizedPendingPath,
        contentBase64: "",
      });
      setSelected(normalizedPendingPath);
    } else if (pathAction === "move" && selected) {
      const source = selected;
      setEntries((current) =>
        current.map((entry) =>
          entry.path === source || entry.path.startsWith(`${source}/`)
            ? {
                ...entry,
                path: `${normalizedPendingPath}${entry.path.slice(source.length)}`,
              }
            : entry,
        ),
      );
      queueOperation({
        action: "move",
        path: source,
        targetPath: normalizedPendingPath,
      });
      setSelected(normalizedPendingPath);
    }
    setEditing(false);
    setPathAction(null);
  };
  const deleteSelected = () => {
    if (!selected) return;
    const path = selected;
    setEntries((current) =>
      current.filter(
        (entry) => entry.path !== path && !entry.path.startsWith(`${path}/`),
      ),
    );
    queueOperation({ action: "delete", path });
    setSelected(null);
    setEditing(false);
    setDeleteOpen(false);
  };
  const writeSelectedText = () => {
    if (!selectedEntry || selectedEntry.type !== "file") return;
    if (editedContent === fileContent) {
      setEditing(false);
      return;
    }
    const bytes = new TextEncoder().encode(editedContent);
    const contentBase64 = bytesToBase64(bytes);
    setEntries((current) =>
      current.map((entry) =>
        entry.path === selectedEntry.path
          ? {
              ...entry,
              binary: false,
              contentBase64,
              size: bytes.byteLength,
            }
          : entry,
      ),
    );
    if (!selectedInstructions)
      queueOperation({
        action: "write",
        path: selectedEntry.path,
        contentBase64,
        ...(selectedEntry.executable ? { executable: true } : {}),
      });
    setEditing(false);
  };
  const uploadFile = async (file: File) => {
    if (file.size > MAX_RESOURCE_FILE_BYTES) {
      setUploadTooLarge(true);
      return;
    }
    setUploadTooLarge(false);
    const bytes = new Uint8Array(await file.arrayBuffer());
    const replacing = selectedEntry?.type === "file";
    const target = replacing
      ? selectedEntry.path
      : joinResourcePath(
          selectedEntry?.type === "directory" ? selectedEntry.path : "",
          file.name,
        );
    const existing = entries.find((entry) => entry.path === target);
    if (
      !isValidResourcePath(target) ||
      target === "SKILL.md" ||
      hasFileAncestor(entries, target) ||
      existing?.type === "directory"
    ) {
      return;
    }
    const binary = isBinaryContent(bytes);
    const contentBase64 = bytesToBase64(bytes);
    const executable = existing?.executable ?? false;
    setEntries((current) => {
      if (existing) {
        return current.map((entry) =>
          entry.path === target
            ? {
                ...entry,
                binary,
                contentBase64,
                executable,
                size: bytes.byteLength,
              }
            : entry,
        );
      }
      return [
        ...current,
        {
          path: target,
          type: "file",
          binary,
          contentBase64,
          executable,
          size: bytes.byteLength,
        },
      ];
    });
    queueOperation({
      action: "write",
      path: target,
      contentBase64,
      ...(executable ? { executable: true } : {}),
    });
    setSelected(target);
    setEditing(false);
  };
  const downloadSelected = async () => {
    if (!selectedEntry || selectedEntry.type !== "file" || !fileReady) return;
    const bytes = localBytes
      ? localBytes
      : fileQuery.data?.binary
        ? base64ToBytes(fileQuery.data.contentBase64 ?? "")
        : new TextEncoder().encode(fileQuery.data?.content ?? "");
    await saveBlob(
      new Blob([Uint8Array.from(bytes).buffer]),
      resourceName(selectedEntry.path),
    );
  };
  const resetChanges = () => {
    setOperations([]);
    setEntries(baseEntries);
    setSelected(null);
    setEditing(false);
    setEditedContent("");
    setSaveVersion(suggestNextPatchVersion(currentVersion));
    setSaveNotes("");
    setUploadTooLarge(false);
    setPathAction(null);
    setDeleteOpen(false);
  };
  const saveChanges = () => {
    if (
      !changeCount ||
      !saveVersionValid ||
      (instructionsChanged && !instructionsContent.trim())
    )
      return;
    onSave(
      {
        files: [],
        resourceOperations: operations,
        ...(instructionsChanged ? { content: instructionsContent } : {}),
        version: saveVersion,
        notes: saveNotes,
      },
      resetChanges,
    );
  };

  return (
    <section className="bg-resource-surface text-resource-content @container/skill-files overflow-hidden rounded-lg">
      <div className="border-border min-h-control-md flex flex-wrap items-center justify-between gap-2 border-b px-4 py-2">
        <span className="text-muted-foreground type-body">
          {t("detail.resourceCount", {
            count: countSkillResourceFiles(tree),
          })}
        </span>
        <div className="flex items-center gap-1">
          <Button
            size="icon-sm"
            variant="ghost"
            title={t("detail.newFile")}
            aria-label={t("detail.newFile")}
            onClick={() => openCreateDialog("file")}
          >
            <FilePlus2 />
          </Button>
          <Button
            size="icon-sm"
            variant="ghost"
            title={t("detail.newDirectory")}
            aria-label={t("detail.newDirectory")}
            onClick={() => openCreateDialog("directory")}
          >
            <FolderPlus />
          </Button>
          <Button
            size="icon-sm"
            variant="ghost"
            disabled={selectedInstructions}
            title={
              selectedEntry?.type === "file"
                ? t("detail.replaceFile")
                : t("detail.uploadFile")
            }
            aria-label={
              selectedEntry?.type === "file"
                ? t("detail.replaceFile")
                : t("detail.uploadFile")
            }
            onClick={() => uploadInputRef.current?.click()}
          >
            <Upload />
          </Button>
          <input
            ref={uploadInputRef}
            type="file"
            className="hidden"
            aria-label={t("detail.selectUploadFile")}
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) void uploadFile(file);
              event.target.value = "";
            }}
          />
        </div>
      </div>
      {uploadTooLarge ? (
        <p
          role="alert"
          className="text-destructive type-caption border-border border-b px-4 py-2"
        >
          {t("detail.uploadTooLarge")}
        </p>
      ) : null}
      <div className="grid min-h-96 grid-cols-1 @[32rem]/skill-files:grid-cols-[minmax(180px,240px)_minmax(0,1fr)]">
        <div className="border-resource-border border-b p-2 @[32rem]/skill-files:border-r @[32rem]/skill-files:border-b-0">
          {tree.length ? (
            tree.map((resource) => (
              <SkillResourceRow
                key={resource.path}
                resource={resource}
                depth={0}
                selected={selected}
                onSelect={(path) => {
                  setSelected(path);
                  setEditing(false);
                }}
              />
            ))
          ) : (
            <div className="grid min-h-80 place-items-center px-3 text-center">
              <p className="text-muted-foreground type-caption">
                {t("detail.resourceEmpty")}
              </p>
            </div>
          )}
        </div>
        <div className="min-w-0 p-4">
          {!selectedEntry ? (
            <div className="text-muted-foreground type-body grid min-h-80 place-items-center text-center">
              {t("detail.selectResource")}
            </div>
          ) : (
            <div className="grid gap-3">
              <header className="min-h-control-md flex min-w-0 items-center justify-between gap-3">
                <div className="min-w-0">
                  <h3 className="type-label truncate font-medium">
                    {selectedEntry.path}
                  </h3>
                  <p className="text-muted-foreground type-caption">
                    {selectedEntry.type === "directory"
                      ? t("detail.directory")
                      : `${fileQuery.data?.size ?? selectedEntry.size ?? 0} B`}
                  </p>
                </div>
                <div className="flex items-center gap-1">
                  {selectedEntry.type === "file" && fileReady ? (
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      title={t("detail.downloadResource")}
                      aria-label={t("detail.downloadResource")}
                      onClick={() => void downloadSelected()}
                    >
                      <Download />
                    </Button>
                  ) : null}
                  {selectedEntry.type === "file" && fileReady && !fileBinary ? (
                    editing ? (
                      <>
                        <Button
                          size="icon-sm"
                          variant="ghost"
                          title={t("detail.cancel")}
                          aria-label={t("detail.cancel")}
                          onClick={() => setEditing(false)}
                        >
                          <X />
                        </Button>
                        <Button
                          size="sm"
                          disabled={pending}
                          onClick={writeSelectedText}
                        >
                          <Save />
                          {t("detail.applyFileChange")}
                        </Button>
                      </>
                    ) : (
                      <Button
                        size="icon-sm"
                        variant="ghost"
                        aria-label={t("detail.editResource", {
                          path: selectedEntry.path,
                        })}
                        onClick={() => {
                          setEditedContent(fileContent);
                          setEditing(true);
                        }}
                      >
                        <Pencil />
                      </Button>
                    )
                  ) : null}
                  {!selectedInstructions ? (
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      title={t("detail.renameMove")}
                      aria-label={t("detail.renameMove")}
                      onClick={() => {
                        setPendingPath(selectedEntry.path);
                        setPathAction("move");
                      }}
                    >
                      <Move />
                    </Button>
                  ) : null}
                  {!selectedInstructions ? (
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      title={t("detail.deleteResource")}
                      aria-label={t("detail.deleteResource")}
                      onClick={() => setDeleteOpen(true)}
                    >
                      <Trash2 />
                    </Button>
                  ) : null}
                </div>
              </header>
              {selectedEntry.type === "file" &&
              !fileReady &&
              fileQuery.isPending ? (
                <ResourceLoadingState className="min-h-80" />
              ) : null}
              {fileQuery.isError ? (
                <p
                  role="alert"
                  className="text-destructive type-body py-6 text-center"
                >
                  {t("detail.resourceLoadFailed")}
                </p>
              ) : null}
              {selectedEntry.type === "directory" ? (
                <div className="text-muted-foreground type-body grid min-h-72 place-items-center text-center">
                  {t("detail.directorySelected")}
                </div>
              ) : null}
              {selectedEntry.type === "file" && fileBinary && fileReady ? (
                <p className="text-muted-foreground type-body py-6 text-center">
                  {t("detail.binaryResource")}
                </p>
              ) : null}
              {selectedEntry.type === "file" && fileReady && !fileBinary ? (
                editing ? (
                  <Textarea
                    value={editedContent}
                    className="min-h-96 font-mono"
                    aria-label={t("detail.editResource", {
                      path: selectedEntry.path,
                    })}
                    onChange={(event) => setEditedContent(event.target.value)}
                  />
                ) : (
                  <pre className="border-border bg-muted/40 text-foreground type-body max-h-[min(680px,calc(100dvh-320px))] overflow-auto rounded-md border p-4 font-mono whitespace-pre-wrap">
                    {fileContent}
                  </pre>
                )
              ) : null}
            </div>
          )}
        </div>
      </div>
      {active && changeCount ? (
        <ResourcePageFooter aria-label={t("detail.resourceSaveStatus")}>
          <div className="grid w-full min-w-0 gap-3">
            <div className="flex flex-wrap items-end gap-3">
              <label className="grid min-w-0 flex-1 basis-40 gap-1.5">
                <span className="type-label font-medium">
                  {t("detail.newVersion")}
                </span>
                <Input
                  appearance="catalog"
                  value={saveVersion}
                  placeholder={suggestNextPatchVersion(currentVersion)}
                  disabled={pending}
                  aria-invalid={!saveVersionValid}
                  onChange={(event) => setSaveVersion(event.target.value)}
                />
              </label>
              <label className="grid min-w-0 flex-1 basis-48 gap-1.5">
                <span className="type-label font-medium">
                  {t("detail.versionNotes")}
                </span>
                <Input
                  appearance="catalog"
                  value={saveNotes}
                  placeholder={t("detail.versionNotesPlaceholder")}
                  disabled={pending}
                  onChange={(event) => setSaveNotes(event.target.value)}
                />
              </label>
              <Button
                type="button"
                size="catalog"
                variant="ghost"
                disabled={pending}
                onClick={resetChanges}
              >
                {t("detail.resetResourceChanges")}
              </Button>
              <Button
                type="button"
                size="catalog"
                disabled={
                  !saveVersionValid ||
                  pending ||
                  (instructionsChanged && !instructionsContent.trim())
                }
                onClick={saveChanges}
              >
                <Save />
                {pending
                  ? t("detail.savingVersion")
                  : t("detail.saveResourceChanges")}
              </Button>
            </div>
            <p className="text-muted-foreground type-caption">
              {t("detail.pendingResourceOperations", {
                count: changeCount,
              })}
            </p>
            {error ? (
              <p role="alert" className="text-destructive type-caption">
                {t("detail.saveVersionFailed")}
              </p>
            ) : null}
          </div>
        </ResourcePageFooter>
      ) : null}

      <SkillResourcePathDialog
        open={pathAction !== null}
        title={
          pathAction === "file"
            ? t("detail.newFile")
            : pathAction === "directory"
              ? t("detail.newDirectory")
              : t("detail.renameMove")
        }
        label={
          pathAction === "move"
            ? t("detail.targetPath")
            : t("detail.resourcePath")
        }
        confirmLabel={
          pathAction === "file"
            ? t("detail.createFile")
            : pathAction === "directory"
              ? t("detail.createDirectory")
              : t("detail.confirmMove")
        }
        value={pendingPath}
        error={pathError}
        onValueChange={setPendingPath}
        onOpenChange={(open) => {
          if (!open) setPathAction(null);
        }}
        onConfirm={confirmPathAction}
      />
      <SkillResourceDeleteDialog
        open={deleteOpen}
        path={selected ?? ""}
        onOpenChange={setDeleteOpen}
        onConfirm={deleteSelected}
      />
    </section>
  );
}

function flattenResources(resources: SkillResource[]): WorkingEntry[] {
  return resources.flatMap((resource) => [
    {
      path: resource.path,
      type: resource.type,
      size: resource.size,
      ...(resource.type === "file" ? { sourcePath: resource.path } : {}),
      ...(resource.executable ? { executable: true } : {}),
    } satisfies WorkingEntry,
    ...flattenResources(resource.children ?? []),
  ]);
}

function buildWorkingTree(entries: WorkingEntry[]): SkillResource[] {
  const roots: SkillResource[] = [];
  const directories = new Map<string, SkillResource>();
  for (const entry of entries) {
    const parts = entry.path.split("/").filter(Boolean);
    let children = roots;
    let currentPath = "";
    for (let index = 0; index < parts.length; index += 1) {
      const name = parts[index];
      if (!name) continue;
      currentPath = currentPath ? `${currentPath}/${name}` : name;
      let resource = directories.get(currentPath);
      if (!resource) {
        const source = entries.find(
          (candidate) => candidate.path === currentPath,
        );
        const directory =
          index < parts.length - 1 || source?.type === "directory";
        resource = {
          name,
          path: currentPath,
          type: directory ? "directory" : "file",
          ...(source
            ? {
                size: source.size,
                ...(source.executable ? { executable: true } : {}),
              }
            : {}),
        };
        children.push(resource);
        if (directory) {
          resource.children = [];
          directories.set(currentPath, resource);
        }
      }
      children = resource.children ?? children;
    }
  }
  return roots;
}

function normalizeResourcePath(value: string): string {
  return value
    .trim()
    .replaceAll("\\", "/")
    .replace(/^\/+|\/+$/g, "");
}

function isValidResourcePath(path: string): boolean {
  const parts = path.split("/");
  return Boolean(
    path && parts.every((part) => part && part !== "." && part !== ".."),
  );
}

function hasFileAncestor(entries: WorkingEntry[], path: string): boolean {
  const parts = path.split("/");
  let current = "";
  for (let index = 0; index + 1 < parts.length; index += 1) {
    current = current ? `${current}/${parts[index]}` : (parts[index] ?? "");
    if (
      entries.some((entry) => entry.path === current && entry.type === "file")
    ) {
      return true;
    }
  }
  return false;
}

function parentPath(path: string): string {
  return path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";
}

function resourceName(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

function joinResourcePath(parent: string, name: string): string {
  return normalizeResourcePath(parent ? `${parent}/${name}` : name);
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 0x8000;
  for (let index = 0; index < bytes.length; index += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize));
  }
  return btoa(binary);
}

function base64ToBytes(value: string): Uint8Array {
  const binary = atob(value);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

function isBinaryContent(bytes: Uint8Array): boolean {
  try {
    new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    return false;
  } catch {
    return true;
  }
}
