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
  WorkspaceGitChange,
  WorkspaceGitChanges,
  ScopedGitStatus,
  ScopedGitLogEntry,
  WorkspaceGitBranch,
  WorkspaceGitBranches,
  WorkspaceGitCheckoutConflict,
  WorkspaceGitCheckoutPreview,
  WorkspaceGitCheckoutResult,
  WorkspaceGitRepository,
  WorkspaceGitRepositories,
  WorkspaceDiffContent,
  FileReferenceRequest,
  FileReferenceResolution,
  BrowserBounds,
  BrowserNavigationEvent,
  BrowserControlAction,
  TerminalStarted,
  TerminalSnapshot,
  TerminalOutputEvent,
  TerminalExitEvent,
} from "./resources";
import type {
  GlobalEventSubscription,
  GlobalEventDelivery,
} from "./core-event-protocol";

export type HostCommands = {
  save_file: { args: { bytes: number[]; fileName: string }; result: boolean };
  select_directory: { args: undefined; result: string | null };
  select_files: { args: undefined; result: string[] };
  stage_attachment: {
    args: { name: string; bytes: Uint8Array };
    result: string;
  };
  open_external_url: { args: { url: string }; result: void };
  show_notification: {
    args: { id: string; title: string; body: string };
    result: boolean;
  };
};

export type WorkspaceCommands = {
  search_workspace_files: {
    args: { root: string; query: string; limit: number; scopePath?: string };
    result: WorkspaceFileEntry[];
  };
  list_workspace_directory: {
    args: { root: string; relativePath: string };
    result: WorkspaceDirectoryEntry[];
  };
  resolve_file_reference: {
    args: { request: FileReferenceRequest };
    result: FileReferenceResolution;
  };
  read_workspace_file: {
    args: { root: string; relativePath: string };
    result: WorkspaceFileContent;
  };
  open_workspace_scope: {
    args: { chatId: string };
    result: WorkspaceResourceScope;
  };
  list_workspace_resource_directory: {
    args: { scopeId: string; relativePath: string; root?: string };
    result: WorkspaceDirectoryEntry[];
  };
  search_workspace_resource_files: {
    args: {
      scopeId: string;
      query: string;
      limit: number;
      scopePath: string;
      root?: string;
    };
    result: WorkspaceFileEntry[];
  };
  save_workspace_resource_as: {
    args: { scopeId: string; relativePath: string; root?: string };
    result: boolean;
  };
  rename_workspace_resource: {
    args: {
      scopeId: string;
      relativePath: string;
      nextName: string;
      expectedVersion: string;
      root?: string;
    };
    result: WorkspaceResourceStat;
  };
  delete_workspace_resource: {
    args: {
      scopeId: string;
      relativePath: string;
      root?: string;
      expectedVersion?: string;
    };
    result: boolean;
  };
  rename_workspace_directory: {
    args: {
      scopeId: string;
      relativePath: string;
      nextName: string;
      root?: string;
    };
    result: string;
  };
  delete_workspace_directory: {
    args: { scopeId: string; relativePath: string; root?: string };
    result: boolean;
  };
  import_workspace_resources: {
    args: { scopeId: string; directoryPath: string; root?: string };
    result: WorkspaceResourceImport;
  };
  import_workspace_resource_bytes: {
    args: {
      scopeId: string;
      directoryPath: string;
      root?: string;
      name: string;
      bytes: Uint8Array;
    };
    result: string;
  };
  open_workspace_resource_external: {
    args: { scopeId: string; relativePath: string; root?: string };
    result: boolean;
  };
  resolve_workspace_resource_reference: {
    args: { scopeId: string; path: string; baseDirectory?: string };
    result: FileReferenceResolution;
  };
  stat_workspace_resource: {
    args: { scopeId: string; relativePath: string; root?: string };
    result: WorkspaceResourceStat | null;
  };
  read_workspace_resource: {
    args: {
      scopeId: string;
      relativePath: string;
      requestId: string;
      root?: string;
    };
    result: WorkspaceResourceRead;
  };
  write_workspace_resource: {
    args: {
      scopeId: string;
      root?: string;
      relativePath: string;
      content: string;
      expectedVersion: string;
      saveRevision: number;
    };
    result: WorkspaceResourceWrite;
  };
  cancel_workspace_resource_read: {
    args: { scopeId: string; requestId: string };
    result: boolean;
  };
  watch_workspace_resource: {
    args: {
      scopeId: string;
      relativePath: string;
      root?: string;
      directory?: boolean;
    };
    result: WorkspaceResourceChangeEvent;
  };
  unwatch_workspace_resource: {
    args: { scopeId: string; subscriptionId: string };
    result: boolean;
  };
  write_workspace_file: {
    args: { root: string; relativePath: string; content: string };
    result: boolean;
  };
  list_chat_workspace_files: {
    args: { chatId: string; root: string };
    result: ChatWorkspaceFiles;
  };
  download_chat_workspace_file: {
    args: { chatId: string; root: string; relativePath: string };
    result: boolean;
  };
  delete_chat_workspace_file: {
    args: { chatId: string; root: string; relativePath: string };
    result: boolean;
  };
  open_workspace_file: {
    args: { root: string; relativePath: string };
    result: boolean;
  };
};

export type GitCommands = {
  get_workspace_git_changes: {
    args: { root: string };
    result: WorkspaceGitChanges;
  };
  list_workspace_git_branches: {
    args: { root: string };
    result: WorkspaceGitBranches;
  };
  preview_workspace_git_checkout: {
    args: { root: string; branch: string };
    result: WorkspaceGitCheckoutPreview;
  };
  checkout_workspace_git_branch: {
    args: { root: string; branch: string; commitMessage?: string };
    result: WorkspaceGitCheckoutResult;
  };
  list_workspace_git_repositories: {
    args: { roots: string[] };
    result: WorkspaceGitRepositories;
  };
  read_workspace_diff: {
    args: { root: string; relativePath: string };
    result: WorkspaceDiffContent;
  };
  scoped_git_status: {
    args: { chatId: string; root: string; repository?: string };
    result: ScopedGitStatus;
  };
  scoped_git_diff: {
    args: {
      chatId: string;
      root: string;
      repository: string;
      relativePath: string;
      staged: boolean;
    };
    result: string;
  };
  scoped_git_log: {
    args: {
      chatId: string;
      root: string;
      repository: string;
      count: number;
      skip: number;
    };
    result: ScopedGitLogEntry[];
  };
  scoped_git_show: {
    args: {
      chatId: string;
      root: string;
      repository: string;
      revision: string;
      relativePath: string;
    };
    result: string | null;
  };
  scoped_git_commit_diff: {
    args: { chatId: string; root: string; repository: string; hash: string };
    result: string;
  };
  scoped_git_stage: {
    args: {
      chatId: string;
      root: string;
      repository: string;
      relativePath: string;
    };
    result: void;
  };
  scoped_git_unstage: {
    args: {
      chatId: string;
      root: string;
      repository: string;
      relativePath: string;
    };
    result: void;
  };
  scoped_git_commit: {
    args: { chatId: string; root: string; repository: string; message: string };
    result: void;
  };
  scoped_git_discard: {
    args: {
      chatId: string;
      root: string;
      repository: string;
      relativePath: string;
    };
    result: boolean;
  };
};

export type BrowserCommands = {
  browser_create: {
    args: BrowserBounds & {
      url: string;
      localRoot?: string;
      chatId?: string;
    };
    result: string;
  };
  browser_prepare_webview: {
    args: { url: string; localRoot?: string; chatId?: string };
    result: string;
  };
  browser_navigate: { args: { viewId: string; url: string }; result: void };
  browser_control: {
    args: { viewId: string; action: BrowserControlAction };
    result: void;
  };
  browser_set_bounds: {
    args: BrowserBounds & { viewId: string };
    result: void;
  };
  browser_set_visible: {
    args: { viewId: string; visible: boolean };
    result: void;
  };
  browser_close: { args: { viewId: string }; result: boolean };
};

export type TerminalCommands = {
  terminal_start: {
    args: {
      root: string;
      chatId: string;
      ownerKey: string;
      createWhenMissing: boolean;
    };
    result: TerminalStarted;
  };
  terminal_attach: {
    args: { sessionId: string };
    result: TerminalSnapshot;
  };
  terminal_detach: {
    args: { sessionId: string; attachmentId: string };
    result: void;
  };
  terminal_write: {
    args: { sessionId: string; attachmentId: string; data: string };
    result: void;
  };
  terminal_resize: {
    args: {
      sessionId: string;
      attachmentId: string;
      size: { rows: number; cols: number };
    };
    result: void;
  };
  terminal_close: { args: { ownerKey: string }; result: boolean };
};

export type EventsCommands = {
  subscribe_core_events: { args: GlobalEventSubscription; result: boolean };
  resync_core_events: {
    args: { subscriptionId: string; after?: number | string };
    result: boolean;
  };
  unsubscribe_core_events: {
    args: { subscriptionId: string };
    result: boolean;
  };
};

export type CommandMethods<T> = {
  [K in keyof T]: T[K] extends { args: infer A; result: infer R }
    ? (
        ...args: A extends undefined ? [args?: undefined] : [args: A]
      ) => Promise<R>
    : never;
};

export type HostService = CommandMethods<HostCommands> & {
  discard_attachment?(args: { sourcePath: string }): Promise<void>;
  onFileDrop(handler: (event: FileDropEvent) => void): () => void;
  onNotificationAction(
    handler: (event: { id: string; action: "click" | "close" }) => void,
  ): () => void;
};

export type WorkspaceService = CommandMethods<WorkspaceCommands> & {
  onWorkspaceResourceChange(
    handler: (event: WorkspaceResourceChangeEvent) => void,
  ): () => void;
};

export type GitService = CommandMethods<GitCommands> & {};

export type BrowserService = CommandMethods<BrowserCommands> & {
  onBrowserNavigation(
    handler: (event: BrowserNavigationEvent) => void,
  ): () => void;
};

export type TerminalService = CommandMethods<TerminalCommands> & {
  onTerminalSnapshot?(handler: (event: TerminalSnapshot) => void): () => void;
  onTerminalOutput(handler: (event: TerminalOutputEvent) => void): () => void;
  onTerminalExit(handler: (event: TerminalExitEvent) => void): () => void;
};

export type EventsService = CommandMethods<EventsCommands> & {
  onCoreEvent(handler: (event: GlobalEventDelivery) => void): () => void;
};
