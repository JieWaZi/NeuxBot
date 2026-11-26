/** Desktop capabilities for handing an authorized file to a known application. */
export const workspaceFileOpenTargets = [
  "vscode",
  "vscode-insiders",
  "vscodium",
  "cursor",
  "zed",
  "file-manager",
] as const;

export type WorkspaceFileOpenTarget = (typeof workspaceFileOpenTargets)[number];

export type WorkspaceFileOpenRequest = {
  target: WorkspaceFileOpenTarget;
  chatId?: string;
  root: string;
  relativePath: string;
  line?: number;
};

export type WorkspaceFileEditorCommands = {
  list_workspace_file_open_targets: {
    args: undefined;
    result: WorkspaceFileOpenTarget[];
  };
  open_workspace_file_target: {
    args: WorkspaceFileOpenRequest;
    result: boolean;
  };
};
