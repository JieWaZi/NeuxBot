import type { ApplicationUpdateState } from "@neuxbot/platform-contracts/application-update";
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
  WorkspaceCommands,
  BrowserCommands,
  EventsCommands,
} from "@neuxbot/platform-contracts/services";
export type DesktopCommands = HostCommands &
  Pick<WorkspaceCommands, "open_workspace_file"> &
  BrowserCommands &
  EventsCommands & {
    application_update_state: {
      args: undefined;
      result: ApplicationUpdateState;
    };
    check_application_update: {
      args: undefined;
      result: ApplicationUpdateState;
    };
    download_application_update: {
      args: undefined;
      result: ApplicationUpdateState;
    };
    open_application_installer: {
      args: undefined;
      result: ApplicationUpdateState;
    };
    core_connection: { args: undefined; result: CoreConnection };
  };
export type DesktopEvents = {
  "application-update": ApplicationUpdateState;
  "core-event": GlobalEventDelivery;
  "browser-navigation": BrowserNavigationEvent;
  "notification-action": { id: string; action: "click" | "close" };
};
export type DesktopBridge = {
  [K in keyof DesktopCommands]: (
    ...args: DesktopCommands[K]["args"] extends undefined
      ? [args?: undefined]
      : [args: DesktopCommands[K]["args"]]
  ) => Promise<DesktopCommands[K]["result"]>;
} & {
  onApplicationUpdate(
    handler: (state: ApplicationUpdateState) => void,
  ): () => void;
  onCoreEvent(handler: (event: GlobalEventDelivery) => void): () => void;
  onBrowserNavigation(
    handler: (event: BrowserNavigationEvent) => void,
  ): () => void;
  onNotificationAction(
    handler: (event: DesktopEvents["notification-action"]) => void,
  ): () => void;
  onFileDrop(handler: (event: FileDropEvent) => void): () => void;
};
