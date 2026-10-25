import type {
  CoreConnection,
  FileDropEvent,
  WorkspaceFileEntry,
  ChatWorkspaceFiles,
  WorkspaceDirectoryEntry,
  WorkspaceFileContent,
  WorkspaceResourceScope,
  WorkspaceResourceStat,
  WorkspaceResourceRead,
  WorkspaceResourceWrite,
  WorkspaceResourceImport,
  WorkspaceResourceChangeEvent,
  WorkspaceGitChanges,
  ScopedGitStatus,
  ScopedGitLogEntry,
  WorkspaceGitBranches,
  WorkspaceGitCheckoutPreview,
  WorkspaceGitCheckoutResult,
  WorkspaceGitRepositories,
  WorkspaceDiffContent,
  FileReferenceRequest,
  FileReferenceResolution,
  BrowserBounds,
  BrowserNavigationEvent,
  BrowserControlAction,
  TerminalSnapshot,
  TerminalStarted,
  TerminalOutputEvent,
  TerminalExitEvent,
} from "@neuxbot/platform-contracts/resources";
import { workspaceSingleFileBytesLimit } from "@neuxbot/platform-contracts/workspace-byte-limits";
export type {
  CoreConnection,
  FileDropEvent,
  WorkspaceFileEntry,
  ChatWorkspaceFiles,
  WorkspaceDirectoryEntry,
  WorkspaceFileContent,
  WorkspaceResourceScope,
  WorkspaceResourceStat,
  WorkspaceResourceRead,
  WorkspaceResourceWrite,
  WorkspaceResourceImport,
  WorkspaceResourceChangeEvent,
  WorkspaceGitChange,
  WorkspaceGitChanges,
  ScopedGitStatus,
  ScopedGitLogEntry,
  WorkspaceGitBranch,
  WorkspaceGitBranches,
  WorkspaceGitCheckoutConflict,
  WorkspaceGitCheckoutPreview,
  WorkspaceGitCheckoutResult,
  WorkspaceGitRepositories,
  WorkspaceGitRepository,
  WorkspaceDiffContent,
  FileReferenceRequest,
  FileReferenceResolution,
  BrowserBounds,
  BrowserNavigationEvent,
  BrowserControlAction,
  TerminalSnapshot,
  TerminalStarted,
  TerminalOutputEvent,
  TerminalExitEvent,
} from "@neuxbot/platform-contracts/resources";
import {
  platform,
  requirePlatformService,
  hasPlatformService,
} from "./provider";
export { configurePlatform, platform, hasPlatformService } from "./provider";

export function isDesktopRuntime(): boolean {
  return platform().kind === "desktop";
}

type DesktopNotificationInput = {
  id: string;
  title: string;
  body: string;
  onClick?: () => void;
};

/** 通知点击通过事件标识返回当前界面，不跨进程传递回调函数。 */
export async function showDesktopNotification({
  id,
  title,
  body,
  onClick,
}: DesktopNotificationInput): Promise<boolean> {
  if (!isDesktopRuntime()) return false;
  const bridge = requirePlatformService("host");
  const stop = bridge.onNotificationAction((event) => {
    if (event.id !== id) return;
    stop();
    if (event.action === "click") onClick?.();
  });
  try {
    const shown = await bridge.show_notification({ id, title, body });
    if (!shown) stop();
    return shown;
  } catch {
    stop();
    return false;
  }
}

export async function getCoreConnection(): Promise<CoreConnection> {
  return platform().getConnection();
}

export async function saveBlob(blob: Blob, fileName: string): Promise<boolean> {
  if (isDesktopRuntime()) {
    const bytes = Array.from(new Uint8Array(await blob.arrayBuffer()));
    return requirePlatformService("host").save_file({ bytes, fileName });
  }

  const objectURL = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = objectURL;
  anchor.download = fileName;
  anchor.hidden = true;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(objectURL), 0);
  return true;
}

export async function selectDirectory(): Promise<string | null> {
  return platform().host.select_directory();
}
export async function selectFiles(): Promise<string[]> {
  return platform().host.select_files();
}
export async function stageAttachment(file: File): Promise<string> {
  return platform().host.stage_attachment({
    name: file.name,
    bytes: new Uint8Array(await file.arrayBuffer()),
  });
}

/** 仅释放尚未接纳的上传，原生路径不在浏览器暂存范围内。 */
export async function discardAttachment(sourcePath: string): Promise<void> {
  await platform().host.discard_attachment?.({ sourcePath });
}

export function subscribeFileDrop(
  onEvent: (event: FileDropEvent) => void,
): () => void {
  return isDesktopRuntime()
    ? requirePlatformService("host").onFileDrop(onEvent)
    : () => undefined;
}

export async function searchWorkspaceFiles(
  root: string,
  query: string,
  limit = 200,
  scopePath?: string,
): Promise<WorkspaceFileEntry[]> {
  return requirePlatformService("workspace").search_workspace_files({
    root,
    query,
    limit,
    scopePath,
  });
}

export async function listWorkspaceDirectory(
  root: string,
  relativePath = "",
): Promise<WorkspaceDirectoryEntry[]> {
  return requirePlatformService("workspace").list_workspace_directory({
    root,
    relativePath,
  });
}

/** 文件系统统一解析入口；浏览器环境不伪造真实文件身份。 */
export async function resolveFileReferencePath(
  request: FileReferenceRequest,
): Promise<FileReferenceResolution> {
  return requirePlatformService("workspace").resolve_file_reference({
    request,
  });
}

export async function readWorkspaceFile(
  root: string,
  relativePath: string,
): Promise<WorkspaceFileContent> {
  return requirePlatformService("workspace").read_workspace_file({
    root,
    relativePath,
  });
}

/** Core 确认 Chat 身份后颁发当前主体的文件范围。 */
export async function openWorkspaceResourceScope(
  chatId: string,
): Promise<WorkspaceResourceScope> {
  return requirePlatformService("workspace").open_workspace_scope({ chatId });
}

export async function listWorkspaceResourceDirectory(
  chatId: string,
  relativePath: string,
  root?: string,
): Promise<WorkspaceDirectoryEntry[]> {
  const scope = await openWorkspaceResourceScope(chatId);
  return requirePlatformService("workspace").list_workspace_resource_directory({
    scopeId: scope.id,
    relativePath,
    root,
  });
}

export async function searchWorkspaceResourceFiles(
  chatId: string,
  query: string,
  limit: number,
  scopePath: string,
  root?: string,
): Promise<WorkspaceFileEntry[]> {
  const scope = await openWorkspaceResourceScope(chatId);
  return requirePlatformService("workspace").search_workspace_resource_files({
    scopeId: scope.id,
    query,
    limit,
    scopePath,
    root,
  });
}

export async function saveWorkspaceResourceAs(
  chatId: string,
  relativePath: string,
  root?: string,
): Promise<boolean> {
  const scope = await openWorkspaceResourceScope(chatId);
  return requirePlatformService("workspace").save_workspace_resource_as({
    scopeId: scope.id,
    relativePath,
    root,
  });
}

export async function renameWorkspaceResource(
  chatId: string,
  relativePath: string,
  nextName: string,
  expectedVersion: string,
  root?: string,
): Promise<WorkspaceResourceStat> {
  const scope = await openWorkspaceResourceScope(chatId);
  return requirePlatformService("workspace").rename_workspace_resource({
    scopeId: scope.id,
    relativePath,
    nextName,
    expectedVersion,
    root,
  });
}

export async function deleteWorkspaceResource(
  chatId: string,
  relativePath: string,
  root?: string,
  expectedVersion?: string,
): Promise<boolean> {
  const scope = await openWorkspaceResourceScope(chatId);
  return requirePlatformService("workspace").delete_workspace_resource({
    scopeId: scope.id,
    relativePath,
    root,
    expectedVersion,
  });
}

export async function renameWorkspaceDirectory(
  chatId: string,
  relativePath: string,
  nextName: string,
  root?: string,
): Promise<string> {
  const scope = await openWorkspaceResourceScope(chatId);
  return requirePlatformService("workspace").rename_workspace_directory({
    scopeId: scope.id,
    relativePath,
    nextName,
    root,
  });
}

export async function deleteWorkspaceDirectory(
  chatId: string,
  relativePath: string,
  root?: string,
): Promise<boolean> {
  const scope = await openWorkspaceResourceScope(chatId);
  return requirePlatformService("workspace").delete_workspace_directory({
    scopeId: scope.id,
    relativePath,
    root,
  });
}

export async function importWorkspaceResources(
  chatId: string,
  directoryPath: string,
  root?: string,
): Promise<WorkspaceResourceImport> {
  const scope = await openWorkspaceResourceScope(chatId);
  return requirePlatformService("workspace").import_workspace_resources({
    scopeId: scope.id,
    directoryPath,
    root,
  });
}

export async function importDroppedWorkspaceFile(
  chatId: string,
  directoryPath: string,
  file: File,
  root?: string,
): Promise<string> {
  if (file.size > workspaceSingleFileBytesLimit)
    throw new Error("拖入文件超过 64 MiB，请使用导入按钮");
  const scope = await openWorkspaceResourceScope(chatId);
  const bytes = new Uint8Array(await file.arrayBuffer());
  return requirePlatformService("workspace").import_workspace_resource_bytes({
    scopeId: scope.id,
    directoryPath,
    root,
    name: file.name,
    bytes,
  });
}

export async function importDroppedWorkspaceFiles(
  chatId: string,
  directoryPath: string,
  files: readonly File[],
  root?: string,
): Promise<WorkspaceResourceImport> {
  if (files.length > 32) throw new Error("一次最多导入 32 个文件");
  const imported: string[] = [];
  const failed: WorkspaceResourceImport["failed"] = [];
  for (const file of files) {
    try {
      imported.push(
        await importDroppedWorkspaceFile(chatId, directoryPath, file, root),
      );
    } catch (cause) {
      failed.push({
        name: file.name,
        message: cause instanceof Error ? cause.message : "导入失败",
      });
    }
  }
  return { imported, failed };
}

export async function openWorkspaceResourceExternal(
  chatId: string,
  relativePath: string,
  root?: string,
): Promise<boolean> {
  const scope = await openWorkspaceResourceScope(chatId);
  return requirePlatformService("workspace").open_workspace_resource_external({
    scopeId: scope.id,
    relativePath,
    root,
  });
}

export async function resolveWorkspaceResourceReference(
  chatId: string,
  path: string,
  baseDirectory?: string,
): Promise<FileReferenceResolution> {
  const scope = await openWorkspaceResourceScope(chatId);
  return requirePlatformService(
    "workspace",
  ).resolve_workspace_resource_reference({
    scopeId: scope.id,
    path,
    baseDirectory,
  });
}

export async function statWorkspaceResource(
  scopeId: string,
  relativePath: string,
  root?: string,
): Promise<WorkspaceResourceStat | null> {
  return requirePlatformService("workspace").stat_workspace_resource({
    scopeId,
    relativePath,
    root,
  });
}

/** 读取取消由当前资源传输执行；关闭标签后旧结果仍须由调用方按 generation 忽略。 */
export async function readWorkspaceResource(
  scopeId: string,
  relativePath: string,
  signal?: AbortSignal,
  root?: string,
): Promise<WorkspaceResourceRead> {
  if (signal?.aborted) throw new DOMException("文件读取已取消", "AbortError");
  const bridge = requirePlatformService("workspace");
  const requestId = crypto.randomUUID();
  const cancel = () => {
    void bridge
      .cancel_workspace_resource_read({ scopeId, requestId })
      .catch(() => {});
  };
  signal?.addEventListener("abort", cancel, { once: true });
  try {
    const result = await bridge.read_workspace_resource({
      scopeId,
      relativePath,
      requestId,
      root,
    });
    if (signal?.aborted) throw new DOMException("文件读取已取消", "AbortError");
    return result;
  } finally {
    signal?.removeEventListener("abort", cancel);
  }
}

export async function writeWorkspaceResource(
  scopeId: string,
  relativePath: string,
  content: string,
  expectedVersion: string,
  saveRevision: number,
  root?: string,
): Promise<WorkspaceResourceWrite> {
  return requirePlatformService("workspace").write_workspace_resource({
    scopeId,
    relativePath,
    content,
    expectedVersion,
    saveRevision,
    root,
  });
}

/** 监听先于 ready 请求安装，并按序号合并竞态中的变化事件。 */
export async function observeWorkspaceResource(
  scopeId: string,
  relativePath: string,
  onChange: (event: WorkspaceResourceChangeEvent) => void,
  signal?: AbortSignal,
  root?: string,
  directory = false,
): Promise<() => Promise<void>> {
  if (signal?.aborted) return async () => {};
  const bridge = requirePlatformService("workspace");
  let active = true;
  let subscriptionId: string | undefined;
  let lastSequence = -1;
  const buffered: WorkspaceResourceChangeEvent[] = [];
  const accept = (event: WorkspaceResourceChangeEvent) => {
    if (!active || event.sequence <= lastSequence) return;
    lastSequence = event.sequence;
    onChange(event);
  };
  const stopEvents = bridge.onWorkspaceResourceChange((event) => {
    if (!subscriptionId) {
      buffered.push(event);
      return;
    }
    if (event.subscriptionId === subscriptionId) accept(event);
  });
  const stop = async () => {
    if (!active) return;
    active = false;
    stopEvents();
    signal?.removeEventListener("abort", abort);
    if (subscriptionId)
      await bridge
        .unwatch_workspace_resource({ scopeId, subscriptionId })
        .catch(() => false);
  };
  const abort = () => {
    void stop();
  };
  signal?.addEventListener("abort", abort, { once: true });
  try {
    const ready = await bridge.watch_workspace_resource({
      scopeId,
      relativePath,
      root,
      directory,
    });
    subscriptionId = ready.subscriptionId;
    if (!active) {
      await bridge
        .unwatch_workspace_resource({ scopeId, subscriptionId })
        .catch(() => false);
      return stop;
    }
    accept(ready);
    for (const event of buffered
      .filter((event) => event.subscriptionId === subscriptionId)
      .sort((a, b) => a.sequence - b.sequence))
      accept(event);
    return stop;
  } catch (error) {
    await stop();
    throw error;
  }
}

export async function writeWorkspaceFile(
  root: string,
  relativePath: string,
  content: string,
): Promise<boolean> {
  return requirePlatformService("workspace").write_workspace_file({
    root,
    relativePath,
    content,
  });
}

export async function listChatWorkspaceFiles(
  chatId: string,
  root: string,
): Promise<ChatWorkspaceFiles> {
  return requirePlatformService("workspace").list_chat_workspace_files({
    chatId,
    root,
  });
}

export async function downloadChatWorkspaceFile(
  chatId: string,
  root: string,
  relativePath: string,
): Promise<boolean> {
  return requirePlatformService("workspace").download_chat_workspace_file({
    chatId,
    root,
    relativePath,
  });
}

export async function deleteChatWorkspaceFile(
  chatId: string,
  root: string,
  relativePath: string,
): Promise<boolean> {
  return requirePlatformService("workspace").delete_chat_workspace_file({
    chatId,
    root,
    relativePath,
  });
}

export async function getWorkspaceGitChanges(
  root: string,
): Promise<WorkspaceGitChanges> {
  return requirePlatformService("git").get_workspace_git_changes({ root });
}

export async function getScopedGitStatus(
  chatId: string,
  root: string,
  repository?: string,
): Promise<ScopedGitStatus> {
  return requirePlatformService("git").scoped_git_status({
    chatId,
    root,
    repository,
  });
}

export async function getScopedGitDiff(
  chatId: string,
  root: string,
  repository: string,
  relativePath: string,
  staged: boolean,
): Promise<string> {
  return requirePlatformService("git").scoped_git_diff({
    chatId,
    root,
    repository,
    relativePath,
    staged,
  });
}

export async function getScopedGitLog(
  chatId: string,
  root: string,
  repository: string,
  count = 20,
  skip = 0,
): Promise<ScopedGitLogEntry[]> {
  return requirePlatformService("git").scoped_git_log({
    chatId,
    root,
    repository,
    count,
    skip,
  });
}

export async function getScopedGitFileAtRevision(
  chatId: string,
  root: string,
  repository: string,
  revision: string,
  relativePath: string,
): Promise<string | null> {
  return requirePlatformService("git").scoped_git_show({
    chatId,
    root,
    repository,
    revision,
    relativePath,
  });
}

export async function getScopedGitCommitDiff(
  chatId: string,
  root: string,
  repository: string,
  hash: string,
): Promise<string> {
  return requirePlatformService("git").scoped_git_commit_diff({
    chatId,
    root,
    repository,
    hash,
  });
}

export async function stageScopedGitFile(
  chatId: string,
  root: string,
  repository: string,
  relativePath: string,
): Promise<void> {
  await requirePlatformService("git").scoped_git_stage({
    chatId,
    root,
    repository,
    relativePath,
  });
}

export async function unstageScopedGitFile(
  chatId: string,
  root: string,
  repository: string,
  relativePath: string,
): Promise<void> {
  await requirePlatformService("git").scoped_git_unstage({
    chatId,
    root,
    repository,
    relativePath,
  });
}

export async function commitScopedGitFiles(
  chatId: string,
  root: string,
  repository: string,
  message: string,
): Promise<void> {
  await requirePlatformService("git").scoped_git_commit({
    chatId,
    root,
    repository,
    message,
  });
}

export async function discardScopedGitFile(
  chatId: string,
  root: string,
  repository: string,
  relativePath: string,
): Promise<boolean> {
  return requirePlatformService("git").scoped_git_discard({
    chatId,
    root,
    repository,
    relativePath,
  });
}

export async function listWorkspaceGitBranches(
  root: string,
): Promise<WorkspaceGitBranches> {
  return requirePlatformService("git").list_workspace_git_branches({ root });
}

export async function previewWorkspaceGitCheckout(
  root: string,
  branch: string,
): Promise<WorkspaceGitCheckoutPreview> {
  return requirePlatformService("git").preview_workspace_git_checkout({
    root,
    branch,
  });
}

export async function checkoutWorkspaceGitBranch(
  root: string,
  branch: string,
  commitMessage?: string,
): Promise<WorkspaceGitCheckoutResult> {
  return requirePlatformService("git").checkout_workspace_git_branch({
    root,
    branch,
    commitMessage,
  });
}

export async function listWorkspaceGitRepositories(
  roots: readonly string[],
): Promise<WorkspaceGitRepositories> {
  return requirePlatformService("git").list_workspace_git_repositories({
    roots: [...roots],
  });
}

export async function readWorkspaceDiff(
  root: string,
  relativePath: string,
): Promise<WorkspaceDiffContent> {
  return requirePlatformService("git").read_workspace_diff({
    root,
    relativePath,
  });
}

export async function openWorkspaceFile(
  root: string,
  relativePath: string,
): Promise<boolean> {
  return requirePlatformService("workspace").open_workspace_file({
    root,
    relativePath,
  });
}

export function normalizeBrowserURL(value: string): string | null {
  const candidate = value.trim();
  if (!candidate) return null;
  try {
    const url = new URL(
      /^[a-z][a-z\d+.-]*:/i.test(candidate)
        ? candidate
        : `https://${candidate}`,
    );
    return url.protocol === "http:" || url.protocol === "https:"
      ? url.toString()
      : null;
  } catch {
    return null;
  }
}

export async function createBrowserView(
  url: string,
  bounds: BrowserBounds,
  localRoot?: string,
  chatId?: string,
): Promise<string> {
  if (!hasPlatformService("browser")) {
    throw new Error("内置浏览器仅在桌面应用中可用");
  }
  return requirePlatformService("browser").browser_create({
    url,
    ...bounds,
    localRoot,
    chatId,
  });
}

export async function prepareBrowserWebview(
  url: string,
  localRoot?: string,
  chatId?: string,
): Promise<string> {
  if (!hasPlatformService("browser")) {
    throw new Error("内置浏览器仅在 NeuxBot 桌面应用中可用");
  }
  return requirePlatformService("browser").browser_prepare_webview({
    url,
    localRoot,
    chatId,
  });
}

export async function navigateBrowserView(
  viewId: string,
  url: string,
): Promise<void> {
  if (!hasPlatformService("browser")) return;
  await requirePlatformService("browser").browser_navigate({ viewId, url });
}

export async function controlBrowserView(
  viewId: string,
  action: BrowserControlAction,
): Promise<void> {
  if (!hasPlatformService("browser")) return;
  await requirePlatformService("browser").browser_control({ viewId, action });
}

export async function setBrowserViewBounds(
  viewId: string,
  bounds: BrowserBounds,
): Promise<void> {
  if (!hasPlatformService("browser")) return;
  await requirePlatformService("browser").browser_set_bounds({
    viewId,
    ...bounds,
  });
}

export async function setBrowserViewVisible(
  viewId: string,
  visible: boolean,
): Promise<void> {
  if (!hasPlatformService("browser")) return;
  await requirePlatformService("browser").browser_set_visible({
    viewId,
    visible,
  });
}

export async function closeBrowserView(viewId: string): Promise<boolean> {
  if (!hasPlatformService("browser")) return false;
  return requirePlatformService("browser").browser_close({ viewId });
}

export async function openExternalURL(url: string): Promise<void> {
  if (!isDesktopRuntime()) {
    window.open(url, "_blank", "noreferrer");
    return;
  }
  await requirePlatformService("host").open_external_url({ url });
}

export function subscribeBrowserNavigation(
  onEvent: (event: BrowserNavigationEvent) => void,
): () => void {
  return hasPlatformService("browser")
    ? requirePlatformService("browser").onBrowserNavigation(onEvent)
    : () => undefined;
}

export async function startTerminal(
  root: string,
  chatId: string,
  ownerKey: string,
  createWhenMissing: boolean,
): Promise<TerminalStarted> {
  if (!hasPlatformService("terminal")) {
    throw new Error("当前服务未提供终端能力");
  }
  return requirePlatformService("terminal").terminal_start({
    root,
    chatId,
    ownerKey,
    createWhenMissing,
  });
}

export async function attachTerminal(
  sessionId: string,
): Promise<TerminalSnapshot> {
  return requirePlatformService("terminal").terminal_attach({ sessionId });
}

export async function detachTerminal(
  sessionId: string,
  attachmentId: string,
): Promise<void> {
  await requirePlatformService("terminal").terminal_detach({
    sessionId,
    attachmentId,
  });
}

export async function writeTerminal(
  sessionId: string,
  attachmentId: string,
  data: string,
): Promise<void> {
  if (!hasPlatformService("terminal")) return;
  await requirePlatformService("terminal").terminal_write({
    sessionId,
    attachmentId,
    data,
  });
}

export async function resizeTerminal(
  sessionId: string,
  attachmentId: string,
  size: { rows: number; cols: number },
): Promise<void> {
  if (!hasPlatformService("terminal")) return;
  await requirePlatformService("terminal").terminal_resize({
    sessionId,
    attachmentId,
    size,
  });
}

export async function closeTerminal(ownerKey: string): Promise<boolean> {
  if (!hasPlatformService("terminal")) return false;
  return requirePlatformService("terminal").terminal_close({ ownerKey });
}

export function subscribeTerminalOutput(
  onEvent: (event: TerminalOutputEvent) => void,
): () => void {
  return hasPlatformService("terminal")
    ? requirePlatformService("terminal").onTerminalOutput(onEvent)
    : () => undefined;
}

export function subscribeTerminalExit(
  onEvent: (event: TerminalExitEvent) => void,
): () => void {
  return hasPlatformService("terminal")
    ? requirePlatformService("terminal").onTerminalExit(onEvent)
    : () => undefined;
}

export function sanitizeDownloadFileName(fileName: string): string {
  return Array.from(fileName)
    .map((character) => (character.charCodeAt(0) < 32 ? "_" : character))
    .join("")
    .replaceAll(/[<>:"/\\|?*]/g, "_")
    .trim();
}

/** 连接恢复交付权威屏幕，不重放输入。 */
export function subscribeTerminalSnapshot(
  handler: (snapshot: TerminalSnapshot) => void,
): () => void {
  return (
    requirePlatformService("terminal").onTerminalSnapshot?.(handler) ??
    (() => undefined)
  );
}
