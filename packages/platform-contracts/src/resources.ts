export type CoreConnection = {
  baseUrl: string;
  token?: string;
  csrfToken?: string;
  credentials?: RequestCredentials;
};

export type FileDropEvent =
  | {
      type: "enter" | "over" | "drop";
      paths: string[];
      position: { x: number; y: number };
    }
  | { type: "leave" };

export type WorkspaceFileEntry = {
  name: string;
  path: string;
  relativePath: string;
};

export type ChatWorkspaceFiles = {
  generated: WorkspaceFileEntry[];
  removedPaths: string[];
};

export type WorkspaceDirectoryEntry = {
  name: string;
  relativePath: string;
  isDirectory: boolean;
  size?: number;
};

export type WorkspaceFileContent = {
  name: string;
  relativePath: string;
  kind: "text" | "image" | "binary" | "too_large";
  mediaType?: string;
  content?: string;
  dataUrl?: string;
  size: number;
  version?: string;
};

/** Core 根据已接纳 Chat 或显式选择的草稿目录建立受限范围。 */
export type WorkspaceResourceScope = { id: string; root: string };

/** 文件元数据和乐观版本；版本只用于普通外部修改检测。 */
export type WorkspaceResourceStat = {
  relativePath: string;
  size: number;
  version: string;
  modifiedAt: number;
  mediaType: string;
};

/** 有大小上限的完整读取结果，传输时保留 Uint8Array。 */
export type WorkspaceResourceRead = WorkspaceResourceStat & {
  bytes: Uint8Array;
};

/** 保存返回写入版本及对应的编辑器修订号。 */
export type WorkspaceResourceWrite = {
  resource: WorkspaceResourceStat;
  saveRevision: number;
};

/** 文件选择器的逐项导入结果，不通过预览缓存搬运大文件。 */
export type WorkspaceResourceImport = {
  imported: string[];
  failed: { name: string; message: string }[];
};

/** 先注册事件监听，再启动订阅；序号用于处理 ready 与变化竞态。 */
export type WorkspaceResourceChangeEvent = {
  subscriptionId: string;
  sequence: number;
  /** 监听有效性与文件存在性分开；恢复或失败不能伪装为删除。 */
  state?: "ready" | "changed" | "recovering" | "failed";
  resource: WorkspaceResourceStat | null;
};

export type WorkspaceGitChange = {
  path: string;
  name: string;
  status: "modified" | "added" | "deleted" | "untracked";
  addedLines: number;
  deletedLines: number;
  gitRoot: string;
};

export type WorkspaceGitChanges = {
  isGitRepo: boolean;
  /** 当前 HEAD 所在分支；detached HEAD 时为短 commit hash。 */
  branch?: string;
  files: WorkspaceGitChange[];
  totalAddedLines: number;
  totalDeletedLines: number;
};

/** Git porcelain 快照，保留暂存区与工作区各自的状态字母。 */
export type ScopedGitStatus = {
  isRepo: boolean;
  branch?: string;
  entries: {
    path: string;
    xy: string;
    stagedAddedLines: number;
    stagedDeletedLines: number;
    unstagedAddedLines: number;
    unstagedDeletedLines: number;
    lineCountUnavailable?: boolean;
  }[];
  truncated?: boolean;
  root?: string;
  repositories?: string[];
};

export type ScopedGitLogEntry = {
  hash: string;
  hashFull: string;
  subject: string;
  author: string;
  date: string;
  refs: string;
};

export type WorkspaceGitBranch = {
  /** 分支短名，例如 main 或 origin/main。 */
  name: string;
  /** 是否为当前检出的分支。 */
  isCurrent: boolean;
  /** 是否为远程跟踪分支。 */
  isRemote: boolean;
};

export type WorkspaceGitBranches = {
  isGitRepo: boolean;
  /** Git 仓库根目录。 */
  gitRoot?: string;
  branches: WorkspaceGitBranch[];
};

export type WorkspaceGitCheckoutConflict = {
  path: string;
  addedLines: number;
  deletedLines: number;
};

export type WorkspaceGitCheckoutPreview = {
  /** 是否可以直接切换，无需先提交。 */
  canCheckout: boolean;
  conflicts: WorkspaceGitCheckoutConflict[];
};

export type WorkspaceGitCheckoutResult = {
  /** 切换后的当前分支名。 */
  branch: string;
};

export type WorkspaceGitRepository = {
  /** 用于展示与 git -C 的目录路径。 */
  directory: string;
  /** Git 仓库根目录。 */
  gitRoot: string;
  /** 目录展示名，通常为路径最后一段。 */
  label: string;
  /** 当前 HEAD 所在分支；detached HEAD 时为短 commit hash。 */
  branch?: string;
};

export type WorkspaceGitRepositories = {
  repositories: WorkspaceGitRepository[];
};

export type WorkspaceDiffContent = {
  name: string;
  relativePath: string;
  oldContent: string;
  newContent: string;
};

export type FileReferenceRequest = {
  path: string;
  baseDirectory: string;
  workspaceRoot: string;
  sourceRoots: readonly string[];
};

export type FileReferenceResolution =
  | {
      status: "resolved" | "directory";
      canonicalPath: string;
      baseDirectory: string;
      workspaceRoot: string;
      sourceRoots: string[];
    }
  | { status: "missing" | "outside_scope" | "ambiguous" | "unavailable" };

export type BrowserBounds = {
  x: number;
  y: number;
  width: number;
  height: number;
};

export type BrowserNavigationEvent = {
  viewId: string;
  url: string;
  title: string;
  canGoBack: boolean;
  canGoForward: boolean;
};

export type BrowserControlAction = "back" | "forward" | "reload";

export type TerminalStarted = {
  sessionId: string;
  shell: string;
};

export type TerminalSnapshot = TerminalStarted & {
  attachmentId: string;
  screen: string;
  sequence: number;
  state: "running" | "exited";
  exitCode?: number;
};

export type TerminalOutputEvent = {
  sessionId: string;
  sequence: number;
  data: string;
};

export type TerminalExitEvent = {
  sessionId: string;
  code?: number;
};
