import type { GlobalEventDelivery } from "@neuxbot/platform-contracts/core-event-protocol";
import type {
  FileDropEvent,
  BrowserNavigationEvent,
} from "@neuxbot/platform-contracts/resources";
export type {
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
} from "@neuxbot/platform-contracts/resources";
export type CoreConnection = { baseUrl: string; token: string };

import type {
  HostCommands,
  NotificationPreferenceCommands,
  WorkspaceAttentionOpen,
  WorkspaceCommands,
  BrowserCommands,
  EventsCommands,
} from "@neuxbot/platform-contracts/services";
export type DesktopCommands = HostCommands &
  NotificationPreferenceCommands &
  Pick<WorkspaceCommands, "open_workspace_file"> &
  BrowserCommands &
  EventsCommands & {
    core_connection: { args: undefined; result: CoreConnection };
  };
export type DesktopEvents = {
  "core-event": GlobalEventDelivery;
  "browser-navigation": BrowserNavigationEvent;
  "workspace-attention-open": WorkspaceAttentionOpen;
};
export type DesktopBridge = {
  [K in keyof DesktopCommands]: (
    ...args: DesktopCommands[K]["args"] extends undefined
      ? [args?: undefined]
      : [args: DesktopCommands[K]["args"]]
  ) => Promise<DesktopCommands[K]["result"]>;
} & {
  onCoreEvent(handler: (event: GlobalEventDelivery) => void): () => void;
  onBrowserNavigation(
    handler: (event: BrowserNavigationEvent) => void,
  ): () => void;
  onWorkspaceAttentionOpen(
    handler: (event: WorkspaceAttentionOpen) => void,
  ): () => void;
  onFileDrop(handler: (event: FileDropEvent) => void): () => void;
};
