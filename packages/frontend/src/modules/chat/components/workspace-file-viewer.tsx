import { generateDiffFile } from "@git-diff-view/file";
import { DiffModeEnum, DiffView } from "@git-diff-view/react";
import "@git-diff-view/react/styles/diff-view-pure.css";
import { useQuery } from "@tanstack/react-query";
import {
  AlignJustify,
  Check,
  Columns2,
  Copy,
  Download,
  ExternalLink,
  Eye,
  FileQuestion,
  Globe,
  List,
  Bot,
  Pencil,
  RotateCcw,
  Save,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import {
  lazy,
  Suspense,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import { resolveFileViewAdapter } from "./workspace-file-adapters";
import {
  fileDirectory,
  isAbsoluteFilePath,
  type ResolvedFileReference,
} from "@/shared/workspace/file-reference";
import { FileReferenceProvider } from "@/shared/workspace/file-reference-link";
import { SelectableCodeViewer } from "./selectable-code-viewer";
import { readWorkspaceTextSelection } from "./workspace-text-selection";
import { getChatMessageFileChangeOptions } from "../api/chat-queries";
import {
  hasPlatformService,
  isDesktopRuntime,
  openWorkspaceResourceScope,
  openWorkspaceResourceExternal,
  openWorkspaceFile,
  readWorkspaceDiff,
  readWorkspaceFile,
  saveWorkspaceResourceAs,
  writeWorkspaceResource,
  writeWorkspaceFile,
} from "@/shared/platform";
import { readScopedFilePreview } from "../workbench/scoped-file-loader";
import { workbenchDocumentBuffers } from "../workbench/document-buffers";
import { WorkspaceMarkdownOutline } from "../workbench/editor/workspace-markdown-outline";
import { WorkspaceMarkdownPreview } from "../workbench/editor/workspace-markdown-preview";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { NativeOverlayPresence } from "@/shared/ui/native-overlay";
import { cn } from "@/shared/lib/cn";
import { Spinner } from "@/shared/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { useWorkspaceQueryEvents } from "@/shared/api/workspace-events";
import { useTheme } from "@/shared/theme";
import type {
  WorkspaceFileView,
  WorkspaceExecutionDiffSource,
} from "@/shared/workspace/workspace-tab";

const MAX_PREVIEW_CHARS = 500_000;
const MIN_IMAGE_SCALE = 0.25;
const MAX_IMAGE_SCALE = 4;
const CodeMirrorEditor = lazy(() =>
  import("@/shared/code-editor/code-mirror-editor").then((module) => ({
    default: module.CodeMirrorEditor,
  })),
);

type WorkspaceFileViewerProps = {
  active?: boolean;
  readOnly?: boolean;
  toolbarAccessory?: ReactNode;
  toolbarTarget?: HTMLElement | null;
  fileView?: WorkspaceFileView;
  fileReference?: ResolvedFileReference;
  onOpenFile?: (file: ResolvedFileReference) => void;
  onOpenURL?: (url: string) => unknown;
  onFileViewChange?: (view: WorkspaceFileView) => void;
  sourceRoots?: readonly string[];
  mode?: "preview" | "diff";
  onClose?: () => void;
  path: string;
  rootPath: string;
  draftKey?: string;
  scopeChatId?: string;
  executionDiff?: WorkspaceExecutionDiffSource;
  onAddCodeReference?: (reference: {
    id: string;
    path: string;
    startLine: number;
    endLine: number;
    content: string;
  }) => void;
  highlightLines?: { startLine: number; endLine: number };
};

// File loading remains outside the renderer and one viewer handles code, text,
// and images.
export function WorkspaceFileViewer({
  active = true,
  readOnly = false,
  toolbarAccessory,
  toolbarTarget,
  mode = "preview",
  fileView,
  fileReference,
  onOpenFile,
  onOpenURL,
  onFileViewChange,
  sourceRoots,
  onClose,
  path,
  rootPath,
  draftKey,
  scopeChatId,
  executionDiff,
  onAddCodeReference,
  highlightLines,
}: WorkspaceFileViewerProps) {
  const { t } = useTranslation("chat");
  const { resolvedTheme } = useTheme();
  const [imageScale, setImageScale] = useState(1);
  const [editing, setEditing] = useState(() =>
    Boolean(draftKey && workbenchDocumentBuffers.get(draftKey)),
  );
  const [draft, setDraft] = useState(
    () => (draftKey ? workbenchDocumentBuffers.get(draftKey)?.text : "") ?? "",
  );
  const draftRevision = useRef(
    draftKey ? (workbenchDocumentBuffers.get(draftKey)?.revision ?? 0) : 0,
  );
  const [editBaseVersion, setEditBaseVersion] = useState<string | undefined>(
    () =>
      draftKey
        ? workbenchDocumentBuffers.get(draftKey)?.baseVersion
        : undefined,
  );
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState(false);
  const [saveErrorText, setSaveErrorText] = useState("");
  const [saveAsError, setSaveAsError] = useState("");
  const [discardOpen, setDiscardOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const [tocOpen, setTocOpen] = useState(false);
  const [diffMode, setDiffMode] = useState(DiffModeEnum.Unified);
  const codeContainerRef = useRef<HTMLDivElement>(null);
  const pointerSelectingRef = useRef(false);
  const [textSelection, setTextSelection] = useState<{
    content: string;
    fileKey: string;
    filePath: string;
    view: WorkspaceFileView;
    range: Range;
    startLine: number;
    endLine: number;
    x: number;
    y: number;
    below: boolean;
  }>();
  const fileQueryKey = ["workspace-file", scopeChatId ?? "", rootPath, path];
  const fileQuery = useQuery({
    queryKey: fileQueryKey,
    queryFn: ({ signal }) =>
      scopeChatId && hasPlatformService("workspace")
        ? readScopedFilePreview(scopeChatId, rootPath, path, signal)
        : readWorkspaceFile(rootPath, path),
    enabled: active && mode === "preview" && Boolean(rootPath) && Boolean(path),
  });
  const workspaceDiffQuery = useQuery({
    queryKey: ["workspace-diff", rootPath, path],
    queryFn: () => readWorkspaceDiff(rootPath, path),
    enabled:
      mode === "diff" && !executionDiff && Boolean(rootPath) && Boolean(path),
  });
  const executionDiffQuery = useQuery({
    ...getChatMessageFileChangeOptions({
      path: {
        chatId: executionDiff?.chatId ?? "",
        messageId: executionDiff?.messageId ?? "",
      },
      query: { path },
    }),
    enabled:
      mode === "diff" &&
      Boolean(executionDiff?.chatId) &&
      Boolean(executionDiff?.messageId) &&
      Boolean(path),
  });
  const file = fileQuery.data;
  const externalConflict = Boolean(
    editing &&
    file?.version &&
    editBaseVersion &&
    file.version !== editBaseVersion,
  );

  const fileEvents = useWorkspaceQueryEvents(
    scopeChatId,
    [rootPath],
    ["workspace-file"],
    active && mode === "preview",
    path,
  );
  const workspaceDiff = workspaceDiffQuery.data;
  const messageDiff = executionDiffQuery.data;
  const diff = useMemo(
    () =>
      executionDiff
        ? messageDiff
          ? {
              name: path.split("/").at(-1) ?? path,
              relativePath: messageDiff.path ?? path,
              oldContent: messageDiff.oldContent ?? "",
              newContent: messageDiff.newContent ?? "",
            }
          : undefined
        : workspaceDiff,
    [executionDiff, messageDiff, path, workspaceDiff],
  );
  const activeDiffQuery = executionDiff
    ? executionDiffQuery
    : workspaceDiffQuery;
  const loading =
    mode === "diff" ? activeDiffQuery.isPending : fileQuery.isPending;
  const loadError =
    mode === "diff" ? activeDiffQuery.isError : fileQuery.isError;
  const extension = path.split(".").at(-1)?.toLowerCase() ?? "";
  const adapter = resolveFileViewAdapter(path, file);
  const fileKey =
    fileReference?.canonicalPath ??
    (isAbsoluteFilePath(path) ? path : `${rootPath}/${path}`);
  const [viewChoice, setViewChoice] = useState<{
    fileKey: string;
    view: WorkspaceFileView;
  }>();
  const preferredView =
    fileView ??
    (viewChoice?.fileKey === fileKey
      ? viewChoice.view
      : editing && /\.(?:md|markdown)$/i.test(path)
        ? "code"
        : undefined);
  const currentView =
    preferredView && adapter.views.includes(preferredView)
      ? preferredView
      : highlightLines && adapter.views.includes("code")
        ? "code"
        : adapter.defaultView;
  const renderingPreview = currentView === "preview";
  const isHtmlPath = /\.(?:html?|xhtml)$/i.test(path);
  const [htmlPreviewChoice, setHtmlPreviewChoice] = useState({
    fileKey,
    interactive: true,
  });
  if (htmlPreviewChoice.fileKey !== fileKey) {
    setHtmlPreviewChoice({ fileKey, interactive: true });
  }
  const htmlInteractive =
    hasPlatformService("browser") &&
    (htmlPreviewChoice.fileKey !== fileKey || htmlPreviewChoice.interactive);
  const htmlPreviewActive =
    mode === "preview" && renderingPreview && isHtmlPath && !editing;
  const markdownDraftPreview =
    editing && renderingPreview && /\.(?:md|markdown)$/i.test(path);
  const selectFileView = (view: WorkspaceFileView) => {
    setTextSelection(undefined);
    setViewChoice({ fileKey, view });
    onFileViewChange?.(view);
  };
  const hasOutline = Boolean(
    adapter.hasOutline && renderingPreview && mode === "preview",
  );
  const diffFile = useMemo(() => {
    if (!diff) return null;
    const generated = generateDiffFile(
      `${diff.relativePath} (HEAD)`,
      diff.oldContent,
      diff.relativePath,
      diff.newContent,
      extension,
      extension,
    );
    generated.initTheme(resolvedTheme);
    generated.init();
    if (diffMode === DiffModeEnum.Split) generated.buildSplitDiffLines();
    else generated.buildUnifiedDiffLines();
    return generated;
  }, [diff, diffMode, extension, resolvedTheme]);
  const saveDraft = async () => {
    if (!file || saving) return;
    const savedRevision = draftRevision.current;
    const savedText = draft;
    setSaving(true);
    setSaveError(false);
    setSaveErrorText("");
    try {
      if (scopeChatId && hasPlatformService("workspace")) {
        if (!editBaseVersion)
          throw new Error(t("workspace.fileVersionUnavailable"));
        const scope = await openWorkspaceResourceScope(scopeChatId);
        const result = await writeWorkspaceResource(
          scope.id,
          path,
          savedText,
          editBaseVersion,
          savedRevision,
          rootPath,
        );
        setEditBaseVersion(result.resource.version);
        if (draftKey)
          workbenchDocumentBuffers.saved(
            draftKey,
            savedRevision,
            savedText,
            result.resource.version,
          );
      } else {
        await writeWorkspaceFile(rootPath, path, savedText);
        if (draftKey)
          workbenchDocumentBuffers.saved(draftKey, savedRevision, savedText);
      }
      await fileQuery.refetch();
      if (draftRevision.current === savedRevision) {
        setEditBaseVersion(undefined);
        setEditing(false);
      }
    } catch (cause) {
      setSaveError(true);
      setSaveErrorText(
        cause instanceof Error ? cause.message : t("workspace.fileSaveError"),
      );
    } finally {
      setSaving(false);
    }
  };
  const discardDraft = () => {
    setEditBaseVersion(undefined);
    if (draftKey) workbenchDocumentBuffers.discard(draftKey);
    setDraft(file?.content ?? "");
    setEditing(false);
    setSaveError(false);
    setSaveErrorText("");
    setDiscardOpen(false);
  };
  const requestDiscardDraft = () => {
    if (
      (draftKey && workbenchDocumentBuffers.isDirty(draftKey)) ||
      (!draftKey && draft !== (file?.content ?? ""))
    ) {
      setDiscardOpen(true);
      return;
    }
    discardDraft();
  };
  const copyContent = async () => {
    const content = editing
      ? draft
      : mode === "diff"
        ? (diff?.newContent ?? "")
        : (file?.content ?? "");
    await navigator.clipboard.writeText(content);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1500);
  };
  const refreshContent = () => {
    fileEvents.retry();
    return mode === "diff" ? activeDiffQuery.refetch() : fileQuery.refetch();
  };
  const currentName = mode === "diff" ? (diff?.name ?? path) : file?.name;
  const currentPath =
    mode === "diff" ? (diff?.relativePath ?? path) : file?.relativePath;

  useEffect(() => {
    if (!highlightLines || !codeContainerRef.current) return;
    let frame = 0;
    let attempts = 0;
    const findLine = (root: ParentNode): HTMLElement | null => {
      const direct = root.querySelector<HTMLElement>(
        `[data-line="${highlightLines.startLine}"]`,
      );
      if (direct) return direct;
      return null;
    };
    const scroll = () => {
      const container = codeContainerRef.current;
      if (!container) return;
      const line = findLine(container);
      if (line) {
        const top =
          container.scrollTop +
          line.getBoundingClientRect().top -
          container.getBoundingClientRect().top -
          container.clientHeight / 2 +
          line.clientHeight / 2;
        container.scrollTo({ top: Math.max(0, top) });
        return;
      }
      attempts += 1;
      if (attempts < 30) frame = window.requestAnimationFrame(scroll);
    };
    frame = window.requestAnimationFrame(scroll);
    return () => window.cancelAnimationFrame(frame);
  }, [highlightLines, currentView, file?.content]);

  useEffect(() => {
    const captureTextSelection = () => {
      if (!onAddCodeReference || mode !== "preview" || editing) {
        return;
      }
      const container = codeContainerRef.current;
      const selection = document.getSelection();
      if (
        !container ||
        !selection ||
        selection.isCollapsed ||
        selection.rangeCount !== 1
      ) {
        setTextSelection(undefined);
        return;
      }
      const range = selection.getRangeAt(0);
      if (!container.contains(range.commonAncestorContainer)) {
        setTextSelection(undefined);
        return;
      }
      const snapshot = readWorkspaceTextSelection(
        container,
        range,
        file?.content ?? "",
      );
      const rect = getVisibleRangeRect(range, container);
      if (!snapshot || !rect) {
        setTextSelection(undefined);
        return;
      }
      const containerRect = container.getBoundingClientRect();
      const center = rect.left + rect.width / 2;
      const x =
        containerRect.width >= 192
          ? Math.min(
              containerRect.right - 96,
              Math.max(containerRect.left + 96, center),
            )
          : center;
      const below = rect.top < 48;
      setTextSelection({
        ...snapshot,
        fileKey,
        filePath: fileReference?.canonicalPath ?? currentPath ?? path,
        view: currentView,
        range,
        x: Math.min(window.innerWidth - 96, Math.max(96, x)),
        y: below ? rect.bottom + 8 : rect.top - 8,
        below,
      });
    };
    if (!onAddCodeReference || mode !== "preview" || editing) return;
    let captureTimer = 0;
    pointerSelectingRef.current = false;
    const dismissSelection = () => {
      window.clearTimeout(captureTimer);
      setTextSelection(undefined);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") dismissSelection();
    };
    const scheduleCapture = () => {
      window.clearTimeout(captureTimer);
      captureTimer = window.setTimeout(captureTextSelection, 80);
    };
    const beginSelection = (event: MouseEvent) => {
      if (event.button !== 0) return;
      const target = event.target;
      if (
        target instanceof Element &&
        target.closest("[data-selection-action-popover]")
      )
        return;
      pointerSelectingRef.current = true;
      setTextSelection(undefined);
    };
    const finishSelection = () => {
      if (!pointerSelectingRef.current) return;
      pointerSelectingRef.current = false;
      scheduleCapture();
    };
    const handleSelectionChange = () => {
      if (pointerSelectingRef.current) return;
      const selection = document.getSelection();
      if (!selection || selection.isCollapsed) {
        dismissSelection();
        return;
      }
      scheduleCapture();
    };
    const container = codeContainerRef.current;
    container?.addEventListener("mousedown", beginSelection, true);
    document.addEventListener("mouseup", finishSelection, true);
    document.addEventListener("selectionchange", handleSelectionChange);
    document.addEventListener("keydown", handleKeyDown);
    container?.addEventListener("scroll", dismissSelection, true);
    window.addEventListener("scroll", dismissSelection, true);
    window.addEventListener("resize", dismissSelection);
    const resizeObserver =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(dismissSelection);
    if (container) resizeObserver?.observe(container);
    return () => {
      window.clearTimeout(captureTimer);
      pointerSelectingRef.current = false;
      document.removeEventListener("keydown", handleKeyDown);
      container?.removeEventListener("scroll", dismissSelection, true);
      window.removeEventListener("scroll", dismissSelection, true);
      window.removeEventListener("resize", dismissSelection);
      resizeObserver?.disconnect();
      container?.removeEventListener("mousedown", beginSelection, true);
      document.removeEventListener("mouseup", finishSelection, true);
      document.removeEventListener("selectionchange", handleSelectionChange);
    };
  }, [
    file?.content,
    fileReference?.canonicalPath,
    currentPath,
    path,
    currentView,
    fileKey,
    editing,
    mode,
    onAddCodeReference,
  ]);

  if (!rootPath) {
    return (
      <WorkspaceViewerState>
        <FileQuestion className="size-icon-xl" strokeWidth={1.5} />
        <span>{t("workspace.noProjectDirectory")}</span>
      </WorkspaceViewerState>
    );
  }
  if (loading) {
    return (
      <WorkspaceViewerState loadingLabel={t("workspace.fileLoading")}>
        <Spinner />
        <span>{t("workspace.fileLoading")}</span>
      </WorkspaceViewerState>
    );
  }
  if (loadError || (mode === "preview" ? !file : !diff)) {
    return (
      <WorkspaceViewerState>
        <FileQuestion className="size-icon-xl" strokeWidth={1.5} />
        <span>{t("workspace.fileLoadError")}</span>
        <Button
          size="sm"
          variant="secondary"
          onClick={() => void refreshContent()}
        >
          {t("workspace.retry")}
        </Button>
      </WorkspaceViewerState>
    );
  }

  const header = (
    <div
      className={cn(
        "scrollbar-hidden flex min-w-0 items-center gap-1 overflow-x-auto",
        !toolbarTarget &&
          "border-border bg-surface h-row-navigation shrink-0 border-b px-3",
      )}
      data-workspace-file-header
    >
      {!toolbarTarget ? (
        <span
          title={fileKey}
          className="type-ui min-w-0 flex-1 truncate font-medium"
        >
          {fileReference?.displayPath ?? currentPath ?? path}
        </span>
      ) : null}
      {mode === "preview" && adapter.views.length > 1 ? (
        <WorkspaceViewerAction
          label={t(
            currentView === "code"
              ? "workspace.renderedPreview"
              : "workspace.sourceCode",
          )}
          disabled={editing && !/\.(?:md|markdown)$/i.test(path)}
          onClick={() =>
            selectFileView(currentView === "code" ? "preview" : "code")
          }
        >
          <Eye />
        </WorkspaceViewerAction>
      ) : null}
      {htmlPreviewActive && hasPlatformService("browser") ? (
        <WorkspaceViewerAction
          label={t(
            htmlInteractive
              ? "workspace.htmlStaticPreview"
              : "workspace.htmlInteractivePreview",
          )}
          hint={
            htmlInteractive
              ? t("workspace.htmlStaticHint")
              : t("workspace.htmlInteractiveHint")
          }
          pressed={htmlInteractive}
          onClick={() =>
            setHtmlPreviewChoice({ fileKey, interactive: !htmlInteractive })
          }
        >
          <Globe />
        </WorkspaceViewerAction>
      ) : null}
      {mode === "diff" ? (
        <WorkspaceViewerAction
          label={
            diffMode === DiffModeEnum.Split
              ? t("workspace.diffUnified")
              : t("workspace.diffSplit")
          }
          onClick={() =>
            setDiffMode(
              diffMode === DiffModeEnum.Split
                ? DiffModeEnum.Unified
                : DiffModeEnum.Split,
            )
          }
        >
          {diffMode === DiffModeEnum.Split ? (
            <Columns2 strokeWidth={1.5} />
          ) : (
            <AlignJustify strokeWidth={1.5} />
          )}
        </WorkspaceViewerAction>
      ) : file ? (
        <span className="text-muted-foreground type-caption shrink-0 tabular-nums">
          {formatFileSize(file.size)}
        </span>
      ) : null}
      {mode === "preview" &&
      !readOnly &&
      adapter.canEdit &&
      file?.kind === "text" &&
      !editing ? (
        <WorkspaceViewerAction
          label={
            /\.(?:md|markdown)$/i.test(path)
              ? t("workspace.editMarkdown")
              : t("workspace.editText")
          }
          onClick={() => {
            draftRevision.current = 0;
            setEditBaseVersion(file.version);
            setDraft(file.content ?? "");
            if (draftKey)
              workbenchDocumentBuffers.begin(
                draftKey,
                file.content ?? "",
                file.version,
              );
            if (/\.(?:md|markdown)$/i.test(path)) selectFileView("code");
            setEditing(true);
          }}
        >
          <Pencil />
        </WorkspaceViewerAction>
      ) : null}
      {editing ? (
        <>
          <WorkspaceViewerAction
            label={t("workspace.cancelEdit")}
            disabled={saving}
            onClick={requestDiscardDraft}
          >
            <X />
          </WorkspaceViewerAction>
          <WorkspaceViewerAction
            label={
              saveError ? t("workspace.retrySave") : t("workspace.saveFile")
            }
            disabled={saving}
            onClick={() => void saveDraft()}
          >
            <Save className={saveError ? "text-destructive" : undefined} />
          </WorkspaceViewerAction>
        </>
      ) : null}
      <WorkspaceViewerAction
        label={t("workspace.copyFileContent")}
        onClick={() => void copyContent()}
      >
        {copied ? <Check className="text-success" /> : <Copy />}
      </WorkspaceViewerAction>
      {hasOutline && (!editing || markdownDraftPreview) ? (
        <WorkspaceViewerAction
          label={
            tocOpen ? t("workspace.hideOutline") : t("workspace.showOutline")
          }
          onClick={() => setTocOpen((current) => !current)}
        >
          <List />
        </WorkspaceViewerAction>
      ) : null}
      {mode === "preview" && adapter.canZoom && renderingPreview ? (
        <>
          <WorkspaceViewerAction
            label={t("workspace.zoomOut")}
            disabled={imageScale <= MIN_IMAGE_SCALE}
            onClick={() =>
              setImageScale((current) =>
                Math.max(MIN_IMAGE_SCALE, current - 0.25),
              )
            }
          >
            <ZoomOut />
          </WorkspaceViewerAction>
          <span className="type-caption w-10 text-center tabular-nums">
            {Math.round(imageScale * 100)}%
          </span>
          <WorkspaceViewerAction
            label={t("workspace.zoomIn")}
            disabled={imageScale >= MAX_IMAGE_SCALE}
            onClick={() =>
              setImageScale((current) =>
                Math.min(MAX_IMAGE_SCALE, current + 0.25),
              )
            }
          >
            <ZoomIn />
          </WorkspaceViewerAction>
          <WorkspaceViewerAction
            label={t("workspace.resetZoom")}
            onClick={() => setImageScale(1)}
          >
            <RotateCcw />
          </WorkspaceViewerAction>
        </>
      ) : null}
      {isDesktopRuntime() ? (
        <WorkspaceViewerAction
          label={t("workspace.openDefaultApp")}
          onClick={() => {
            void (
              scopeChatId && hasPlatformService("workspace")
                ? openWorkspaceResourceExternal(scopeChatId, path, rootPath)
                : openWorkspaceFile(rootPath, path)
            ).catch((cause: unknown) =>
              setSaveAsError(
                cause instanceof Error
                  ? cause.message
                  : t("workspace.fileLoadError"),
              ),
            );
          }}
        >
          <ExternalLink />
        </WorkspaceViewerAction>
      ) : null}
      {scopeChatId && hasPlatformService("workspace") && mode === "preview" ? (
        <WorkspaceViewerAction
          label={t("workspace.downloadFile")}
          onClick={() => {
            setSaveAsError("");
            void saveWorkspaceResourceAs(scopeChatId, path, rootPath).catch(
              (cause: unknown) =>
                setSaveAsError(
                  cause instanceof Error
                    ? cause.message
                    : t("workspace.downloadFileError"),
                ),
            );
          }}
        >
          <Download />
        </WorkspaceViewerAction>
      ) : null}
      {onClose ? (
        <WorkspaceViewerAction
          label={t("workspace.closePreview")}
          onClick={onClose}
        >
          <X />
        </WorkspaceViewerAction>
      ) : null}
      {toolbarAccessory}
    </div>
  );

  return (
    <section className="flex h-full min-h-0 flex-col" aria-label={currentName}>
      {toolbarTarget ? createPortal(header, toolbarTarget) : header}
      {saveErrorText || saveAsError || fileEvents.error || externalConflict ? (
        <p
          role="alert"
          className="border-border-subtle bg-destructive/8 text-destructive type-caption border-b px-3 py-2"
        >
          {saveErrorText ||
            fileEvents.error?.message ||
            saveAsError ||
            (externalConflict ? t("workspace.fileExternalConflict") : "")}
        </p>
      ) : null}
      <div className="bg-surface flex min-h-0 flex-1 overflow-hidden">
        {hasOutline && tocOpen && (!editing || markdownDraftPreview) ? (
          <WorkspaceMarkdownOutline scrollContainer={codeContainerRef} />
        ) : null}
        <div
          ref={codeContainerRef}
          className={cn(
            "relative min-h-0 min-w-0 flex-1",
            htmlPreviewActive
              ? "overflow-hidden"
              : "overflow-auto overscroll-contain",
          )}
          onMouseDown={(event) => {
            if (
              event.target instanceof Element &&
              event.target.closest("[data-selection-action-popover]")
            )
              return;
            setTextSelection(undefined);
          }}
        >
          {textSelection &&
          textSelection.fileKey === fileKey &&
          textSelection.view === currentView &&
          !textSelection.range.collapsed &&
          textSelection.range.startContainer.isConnected &&
          mode === "preview" &&
          !editing
            ? createPortal(
                <div
                  data-selection-action-popover
                  className={cn(
                    "border-border bg-surface-elevated text-foreground fixed z-50 w-max max-w-[calc(100vw-24px)] -translate-x-1/2 rounded-md border p-1 shadow-sm",
                    !textSelection.below && "-translate-y-full",
                  )}
                  style={{
                    left: textSelection.x,
                    top: textSelection.y,
                  }}
                  onMouseDown={(event) => event.preventDefault()}
                >
                  <NativeOverlayPresence />
                  <Button
                    type="button"
                    className="whitespace-nowrap"
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      onAddCodeReference?.({
                        id: crypto.randomUUID(),
                        path: textSelection.filePath,
                        startLine: textSelection.startLine,
                        endLine: textSelection.endLine,
                        content: textSelection.content,
                      });
                      window.getSelection()?.removeAllRanges();
                      setTextSelection(undefined);
                    }}
                  >
                    <Bot />
                    {t("workspace.addSelectionToChat")}
                  </Button>
                </div>,
                document.body,
              )
            : null}
          {mode === "diff" && diffFile ? (
            <DiffView
              diffFile={diffFile}
              diffViewMode={diffMode}
              diffViewTheme={resolvedTheme}
              diffViewHighlight
              diffViewWrap={false}
              diffViewFontSize={12}
              className="workspace-diff-view"
            />
          ) : editing ? (
            <div className="relative min-h-full">
              <div
                aria-hidden={markdownDraftPreview || undefined}
                className={cn(
                  "absolute inset-0",
                  markdownDraftPreview && "pointer-events-none invisible",
                )}
              >
                <Suspense fallback={<Spinner />}>
                  <CodeMirrorEditor
                    key={draftKey ?? fileKey}
                    initialValue={draft}
                    path={currentPath ?? path}
                    label={t("workspace.fileEditor")}
                    dark={resolvedTheme === "dark"}
                    onChange={(value) => {
                      draftRevision.current += 1;
                      setDraft(value);
                      if (draftKey)
                        workbenchDocumentBuffers.change(draftKey, value);
                    }}
                    onSave={() => void saveDraft()}
                    onCancel={requestDiscardDraft}
                  />
                </Suspense>
              </div>
              {markdownDraftPreview ? (
                <FileReferenceProvider
                  context={{
                    chatId: scopeChatId,
                    baseDirectory: fileDirectory(fileKey),
                    workspaceRoot: rootPath,
                    sourceRoots: sourceRoots ?? [],
                  }}
                  onOpenFile={onOpenFile}
                  onOpenURL={onOpenURL}
                >
                  <WorkspaceMarkdownPreview
                    source={draft}
                    path={path}
                    rootPath={rootPath}
                    chatId={scopeChatId}
                  />
                </FileReferenceProvider>
              ) : null}
            </div>
          ) : file?.kind === "too_large" ? (
            <WorkspaceViewerState>
              <FileQuestion className="size-icon-xl" strokeWidth={1.5} />
              <span>{t("workspace.fileTooLarge")}</span>
            </WorkspaceViewerState>
          ) : file?.kind === "binary" && /\.(?:doc|ppt)$/i.test(path) ? (
            <WorkspaceViewerState>
              <FileQuestion className="size-icon-xl" strokeWidth={1.5} />
              <span>{t("workspace.legacyOfficeUnavailable")}</span>
            </WorkspaceViewerState>
          ) : file?.kind === "binary" &&
            !/\.(?:pdf|docx|pptx|xlsx|xls|csv|tsv)$/i.test(path) ? (
            <WorkspaceViewerState>
              <FileQuestion className="size-icon-xl" strokeWidth={1.5} />
              <span>{t("workspace.binaryFileUnsupported")}</span>
            </WorkspaceViewerState>
          ) : (file?.content?.length ?? 0) > MAX_PREVIEW_CHARS ? (
            <pre className="text-foreground type-body p-3 font-mono break-words whitespace-pre-wrap">
              {file?.content?.slice(0, MAX_PREVIEW_CHARS)}
              <span className="text-muted-foreground mt-3 block font-sans">
                {t("workspace.fileTruncated", {
                  count: MAX_PREVIEW_CHARS.toLocaleString(),
                })}
              </span>
            </pre>
          ) : renderingPreview && adapter.renderPreview && file ? (
            <FileReferenceProvider
              context={{
                chatId: scopeChatId,
                baseDirectory: fileDirectory(fileKey),
                workspaceRoot: rootPath,
                sourceRoots: sourceRoots ?? [],
              }}
              onOpenFile={onOpenFile}
              onOpenURL={onOpenURL}
            >
              {adapter.renderPreview({
                active,
                file,
                rootPath,
                path,
                refreshToken: fileQuery.dataUpdatedAt,
                imageScale,
                scopeChatId,
                htmlInteractive,
              })}
            </FileReferenceProvider>
          ) : file?.content ? (
            <SelectableCodeViewer
              content={file.content}
              fileName={currentPath ?? path}
              highlightLines={highlightLines}
              theme={resolvedTheme}
              wrap={false}
            />
          ) : (
            <WorkspaceViewerState>
              <span>{t("workspace.fileEmpty")}</span>
            </WorkspaceViewerState>
          )}
        </div>
      </div>
      <Dialog open={discardOpen} onOpenChange={setDiscardOpen}>
        <DialogContent
          size="sm"
          title={t("workbench.unsavedTitle")}
          description={t("workbench.discardEditDescription")}
        >
          <DialogFooter>
            <Button variant="secondary" onClick={() => setDiscardOpen(false)}>
              {t("workbench.keepEditing")}
            </Button>
            <Button variant="danger" onClick={discardDraft}>
              {t("workbench.discardEdit")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function WorkspaceViewerAction({
  children,
  disabled,
  hint,
  label,
  onClick,
  pressed,
}: {
  children: ReactNode;
  disabled?: boolean;
  hint?: string;
  label: string;
  onClick: () => void;
  pressed?: boolean;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          aria-label={label}
          aria-pressed={pressed}
          disabled={disabled}
          className="shrink-0"
          size="icon-sm"
          title={hint}
          variant={pressed ? "secondary" : "ghost"}
          onClick={onClick}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{hint ?? label}</TooltipContent>
    </Tooltip>
  );
}

function WorkspaceViewerState({
  children,
  loadingLabel,
}: {
  children: ReactNode;
  loadingLabel?: string;
}) {
  return (
    <div
      aria-busy={loadingLabel ? "true" : undefined}
      aria-label={loadingLabel}
      className="text-muted-foreground type-body flex h-full min-h-44 flex-col items-center justify-center gap-3 px-8 text-center"
      role={loadingLabel ? "status" : undefined}
    >
      {children}
    </div>
  );
}

function formatFileSize(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}

function getVisibleRangeRect(
  range: Range,
  container: HTMLElement,
): DOMRect | null {
  const bounds = container.getBoundingClientRect();
  const fragments = Array.from(range.getClientRects()).filter(
    (rect) =>
      rect.width > 0 &&
      rect.height > 0 &&
      (bounds.width === 0 ||
        (rect.right > bounds.left &&
          rect.left < bounds.right &&
          rect.bottom > bounds.top &&
          rect.top < bounds.bottom)),
  );
  if (fragments.length) return fragments.at(-1) ?? null;
  const rect = range.getBoundingClientRect();
  return rect.width > 0 || rect.height > 0 ? rect : null;
}
