import type {
  HostService,
  WorkspaceService,
  GitService,
} from "@neuxbot/platform-contracts/services";
import type { WorkspaceResourceChangeEvent } from "@neuxbot/platform-contracts/resources";

/** 文件/Git 按首次业务调用载入；注册事件回调本身不拉入实现或建立连接。 */
export function createCoreWorkspaceServices(
  host: HostService,
  options: {
    openFile?: (root: string, relativePath: string) => Promise<boolean>;
  } = {},
): { workspace: WorkspaceService; git: GitService } {
  const listeners = new Set<(event: WorkspaceResourceChangeEvent) => void>();
  let pending:
    Promise<{ workspace: WorkspaceService; git: GitService }> | undefined;
  function load() {
    return (pending ??= import("./core-workspace")
      .then((module) => {
        const services = module.createCoreWorkspaceServices(host, options);
        services.workspace.onWorkspaceResourceChange((event) => {
          for (const listener of listeners) listener(event);
        });
        return services;
      })
      .catch((error) => {
        pending = undefined;
        throw error;
      }));
  }
  return {
    workspace: {
      search_workspace_files: (...args) =>
        load().then((services) =>
          services.workspace.search_workspace_files(...args),
        ),
      list_workspace_directory: (...args) =>
        load().then((services) =>
          services.workspace.list_workspace_directory(...args),
        ),
      resolve_file_reference: (...args) =>
        load().then((services) =>
          services.workspace.resolve_file_reference(...args),
        ),
      read_workspace_file: (...args) =>
        load().then((services) =>
          services.workspace.read_workspace_file(...args),
        ),
      open_workspace_scope: (...args) =>
        load().then((services) =>
          services.workspace.open_workspace_scope(...args),
        ),
      list_workspace_resource_directory: (...args) =>
        load().then((services) =>
          services.workspace.list_workspace_resource_directory(...args),
        ),
      search_workspace_resource_files: (...args) =>
        load().then((services) =>
          services.workspace.search_workspace_resource_files(...args),
        ),
      save_workspace_resource_as: (...args) =>
        load().then((services) =>
          services.workspace.save_workspace_resource_as(...args),
        ),
      rename_workspace_resource: (...args) =>
        load().then((services) =>
          services.workspace.rename_workspace_resource(...args),
        ),
      delete_workspace_resource: (...args) =>
        load().then((services) =>
          services.workspace.delete_workspace_resource(...args),
        ),
      rename_workspace_directory: (...args) =>
        load().then((services) =>
          services.workspace.rename_workspace_directory(...args),
        ),
      delete_workspace_directory: (...args) =>
        load().then((services) =>
          services.workspace.delete_workspace_directory(...args),
        ),
      import_workspace_resources: (...args) =>
        load().then((services) =>
          services.workspace.import_workspace_resources(...args),
        ),
      import_workspace_resource_bytes: (...args) =>
        load().then((services) =>
          services.workspace.import_workspace_resource_bytes(...args),
        ),
      open_workspace_resource_external: (...args) =>
        load().then((services) =>
          services.workspace.open_workspace_resource_external(...args),
        ),
      resolve_workspace_resource_reference: (...args) =>
        load().then((services) =>
          services.workspace.resolve_workspace_resource_reference(...args),
        ),
      stat_workspace_resource: (...args) =>
        load().then((services) =>
          services.workspace.stat_workspace_resource(...args),
        ),
      read_workspace_resource: (...args) =>
        load().then((services) =>
          services.workspace.read_workspace_resource(...args),
        ),
      write_workspace_resource: (...args) =>
        load().then((services) =>
          services.workspace.write_workspace_resource(...args),
        ),
      cancel_workspace_resource_read: (...args) =>
        load().then((services) =>
          services.workspace.cancel_workspace_resource_read(...args),
        ),
      watch_workspace_resource: (...args) =>
        load().then((services) =>
          services.workspace.watch_workspace_resource(...args),
        ),
      unwatch_workspace_resource: (...args) =>
        load().then((services) =>
          services.workspace.unwatch_workspace_resource(...args),
        ),
      write_workspace_file: (...args) =>
        load().then((services) =>
          services.workspace.write_workspace_file(...args),
        ),
      list_chat_workspace_files: (...args) =>
        load().then((services) =>
          services.workspace.list_chat_workspace_files(...args),
        ),
      download_chat_workspace_file: (...args) =>
        load().then((services) =>
          services.workspace.download_chat_workspace_file(...args),
        ),
      delete_chat_workspace_file: (...args) =>
        load().then((services) =>
          services.workspace.delete_chat_workspace_file(...args),
        ),
      open_workspace_file: (...args) =>
        load().then((services) =>
          services.workspace.open_workspace_file(...args),
        ),
      onWorkspaceResourceChange(handler) {
        listeners.add(handler);
        return () => {
          listeners.delete(handler);
        };
      },
    },
    git: {
      get_workspace_git_changes: (...args) =>
        load().then((services) =>
          services.git.get_workspace_git_changes(...args),
        ),
      list_workspace_git_branches: (...args) =>
        load().then((services) =>
          services.git.list_workspace_git_branches(...args),
        ),
      preview_workspace_git_checkout: (...args) =>
        load().then((services) =>
          services.git.preview_workspace_git_checkout(...args),
        ),
      checkout_workspace_git_branch: (...args) =>
        load().then((services) =>
          services.git.checkout_workspace_git_branch(...args),
        ),
      list_workspace_git_repositories: (...args) =>
        load().then((services) =>
          services.git.list_workspace_git_repositories(...args),
        ),
      read_workspace_diff: (...args) =>
        load().then((services) => services.git.read_workspace_diff(...args)),
      scoped_git_status: (...args) =>
        load().then((services) => services.git.scoped_git_status(...args)),
      scoped_git_diff: (...args) =>
        load().then((services) => services.git.scoped_git_diff(...args)),
      scoped_git_log: (...args) =>
        load().then((services) => services.git.scoped_git_log(...args)),
      scoped_git_show: (...args) =>
        load().then((services) => services.git.scoped_git_show(...args)),
      scoped_git_commit_diff: (...args) =>
        load().then((services) => services.git.scoped_git_commit_diff(...args)),
      scoped_git_stage: (...args) =>
        load().then((services) => services.git.scoped_git_stage(...args)),
      scoped_git_unstage: (...args) =>
        load().then((services) => services.git.scoped_git_unstage(...args)),
      scoped_git_commit: (...args) =>
        load().then((services) => services.git.scoped_git_commit(...args)),
      scoped_git_discard: (...args) =>
        load().then((services) => services.git.scoped_git_discard(...args)),
    },
  };
}
