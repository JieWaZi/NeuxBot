import { z } from "zod";
import type {
  HostService,
  WorkspaceService,
  GitService,
} from "@neuxbot/platform-contracts/services";
import type {
  WorkspaceResourceScope,
  WorkspaceResourceRead,
  WorkspaceResourceChangeEvent,
  WorkspaceFileContent,
  WorkspaceGitChanges,
  FileReferenceResolution,
} from "@neuxbot/platform-contracts/resources";
import * as api from "@/shared/api/generated/sdk.gen";
import {
  subscribeGlobalEvents,
  registerCoreConnectionRecovery,
} from "@/shared/api/global-events";

const resourceSchema = z.object({
  relativePath: z.string(),
  size: z.number(),
  version: z.string(),
  modifiedAt: z.number(),
  mediaType: z.string(),
});
const scopeSchema = z.object({
  id: z.string().min(1),
  root: z.string().min(1),
});
const entrySchema = z.object({
  name: z.string(),
  relativePath: z.string(),
  isDirectory: z.boolean(),
  size: z.number().optional(),
});
const matchSchema = z.object({
  name: z.string(),
  path: z.string(),
  relativePath: z.string(),
});
const entriesSchema = entrySchema
  .array()
  .nullable()
  .transform((value) => value ?? []);
const matchesSchema = matchSchema
  .array()
  .nullable()
  .transform((value) => value ?? []);
const pathSchema = z.object({ relativePath: z.string() });
const writeSchema = z.object({
  resource: resourceSchema,
  saveRevision: z.number(),
});
const chatFilesSchema = z.object({
  generated: matchesSchema,
  removedPaths: z
    .string()
    .array()
    .nullable()
    .transform((value) => value ?? []),
});
const gitStatusSchema = z.object({
  isRepo: z.boolean(),
  branch: z.string().optional(),
  root: z.string().optional(),
  repositories: z
    .string()
    .array()
    .nullable()
    .transform((value) => value ?? []),
  entries: z
    .array(
      z.object({
        path: z.string(),
        xy: z.string(),
        stagedAddedLines: z.number(),
        stagedDeletedLines: z.number(),
        unstagedAddedLines: z.number(),
        unstagedDeletedLines: z.number(),
        lineCountUnavailable: z.boolean().optional(),
      }),
    )
    .nullable()
    .transform((value) => value ?? []),
});
const gitLogSchema = z
  .array(
    z.object({
      hash: z.string(),
      hashFull: z.string(),
      subject: z.string(),
      author: z.string(),
      date: z.string(),
      refs: z.string(),
    }),
  )
  .nullable()
  .transform((value) => value ?? []);
const branchesSchema = z.object({
  isGitRepo: z.boolean(),
  gitRoot: z.string().optional(),
  branches: z
    .array(
      z.object({
        name: z.string(),
        isCurrent: z.boolean(),
        isRemote: z.boolean(),
      }),
    )
    .nullable()
    .transform((value) => value ?? []),
});
const checkoutPreviewSchema = z.object({
  canCheckout: z.boolean(),
  conflicts: z
    .array(
      z.object({
        path: z.string(),
        addedLines: z.number(),
        deletedLines: z.number(),
      }),
    )
    .nullable()
    .transform((value) => value ?? []),
});
const checkoutSchema = z.object({ branch: z.string() });
const changeSchema = z.object({
  subscriptionId: z.string(),
  sequence: z.number().int().nonnegative(),
  state: z.enum(["ready", "changed", "recovering", "failed"]),
  resource: resourceSchema.nullable(),
});
const resolutionSchema = z.union([
  z.object({
    status: z.enum(["resolved", "directory"]),
    canonicalPath: z.string(),
    baseDirectory: z.string(),
    workspaceRoot: z.string(),
    sourceRoots: z.array(z.string()).optional().default([]),
  }),
  z.object({
    status: z.enum(["missing", "outside_scope", "ambiguous", "unavailable"]),
  }),
]);
function decodeBytes(value: string): Uint8Array {
  return Uint8Array.from(atob(value), (char) => char.charCodeAt(0));
}
function encodeBytes(value: Uint8Array): string {
  let result = "";
  for (let at = 0; at < value.length; at += 32768)
    result += String.fromCharCode(...value.subarray(at, at + 32768));
  return btoa(result);
}
function basename(value: string): string {
  return value.split(/[\\/]/).pop() || "download";
}
function pickFiles(): Promise<File[]> {
  return new Promise((resolve) => {
    const input = document.createElement("input");
    input.type = "file";
    input.multiple = true;
    input.onchange = () => resolve(Array.from(input.files ?? []));
    input.oncancel = () => resolve([]);
    input.click();
  });
}
/** 文件/Git 的事实与授权在 Core，宿主只负责选择文件、下载和系统打开。 */
export function createCoreWorkspaceServices(
  host: HostService,
  options: {
    openFile?: (root: string, relativePath: string) => Promise<boolean>;
  } = {},
): { workspace: WorkspaceService; git: GitService } {
  const scopes = new Map<
    string,
    { at: number; value: Promise<WorkspaceResourceScope> }
  >();
  const known = new Map<string, WorkspaceResourceScope>();
  const reads = new Map<string, AbortController>();
  const watches = new Map<string, () => void>();
  const listeners = new Set<(event: WorkspaceResourceChangeEvent) => void>();
  registerCoreConnectionRecovery(() => {
    scopes.clear();
    known.clear();
  });
  function scope(
    key: string,
    create: () => Promise<WorkspaceResourceScope>,
  ): Promise<WorkspaceResourceScope> {
    const cached = scopes.get(key);
    if (cached && Date.now() - cached.at < 600000) return cached.value;
    const value = create()
      .then((value) => {
        if (known.size >= 256) {
          const oldest = known.keys().next().value;
          if (oldest !== undefined) known.delete(oldest);
        }
        known.set(value.id, value);
        return value;
      })
      .catch((error) => {
        scopes.delete(key);
        throw error;
      });
    if (scopes.size >= 128) {
      const oldest = scopes.keys().next().value;
      if (oldest !== undefined) scopes.delete(oldest);
    }
    scopes.set(key, { at: Date.now(), value });
    return value;
  }
  const chatScope = (chatId: string) =>
    scope(`chat:${chatId}`, async () => {
      const { data } = await api.openScope({
        body: { chatId },
        throwOnError: true,
      });
      return scopeSchema.parse(data);
    });
  const draftScope = (root: string, additionalRoots: string[] = []) =>
    scope(`draft:${JSON.stringify([root, additionalRoots])}`, async () => {
      const { data } = await api.openDraftScope({
        body: { root, additionalRoots },
        throwOnError: true,
      });
      return scopeSchema.parse(data);
    });
  async function read(
    args: { scopeId: string; relativePath: string; root?: string },
    signal?: AbortSignal,
  ): Promise<WorkspaceResourceRead> {
    const { data } = await api.readFile({
      query: args,
      signal,
      throwOnError: true,
    });
    const response = z
      .object({ resource: resourceSchema, bytesBase64: z.string() })
      .parse(data);
    return { ...response.resource, bytes: decodeBytes(response.bytesBase64) };
  }
  async function save(args: {
    scopeId: string;
    relativePath: string;
    root?: string;
  }): Promise<boolean> {
    const file = await read(args);
    return host.save_file({
      bytes: Array.from(file.bytes),
      fileName: basename(file.relativePath),
    });
  }
  async function preview(
    root: string,
    relativePath: string,
  ): Promise<WorkspaceFileContent> {
    const scope = await draftScope(root);
    const file = await read({ scopeId: scope.id, root, relativePath });
    const base = {
      name: basename(relativePath),
      relativePath,
      size: file.size,
      version: file.version,
      mediaType: file.mediaType,
    };
    if (file.mediaType.startsWith("image/"))
      return {
        ...base,
        kind: "image",
        dataUrl: `data:${file.mediaType};base64,${encodeBytes(file.bytes)}`,
      };
    if (file.bytes.includes(0)) return { ...base, kind: "binary" };
    if (file.size > 10 * 1024 * 1024) return { ...base, kind: "too_large" };
    return {
      ...base,
      kind: "text",
      content: new TextDecoder().decode(file.bytes),
    };
  }
  async function external(args: {
    scopeId: string;
    relativePath: string;
    root?: string;
  }) {
    const root = args.root ?? known.get(args.scopeId)?.root;
    if (!root || !options.openFile)
      throw new Error("当前环境不支持用系统应用打开文件，请下载后打开");
    return options.openFile(root, args.relativePath);
  }
  const workspace: WorkspaceService = {
    open_workspace_scope: ({ chatId }) => chatScope(chatId),
    async list_workspace_resource_directory(args) {
      const { data } = await api.listDirectory({
        query: args,
        throwOnError: true,
      });
      return entriesSchema.parse(data);
    },
    async search_workspace_resource_files(args) {
      const { data } = await api.searchFiles({
        query: args,
        throwOnError: true,
      });
      return matchesSchema.parse(data);
    },
    save_workspace_resource_as: save,
    async rename_workspace_resource(args) {
      const { data } = await api.renameFile({
        body: { ...args, root: args.root ?? "" },
        throwOnError: true,
      });
      return resourceSchema.parse(data);
    },
    async delete_workspace_resource(args) {
      const expectedVersion =
        args.expectedVersion ??
        (await workspace.stat_workspace_resource(args))?.version;
      if (!expectedVersion) throw new Error("文件不存在或已变化");
      const body = { ...args, root: args.root ?? "", expectedVersion };
      await api.deleteFile({ body, throwOnError: true });
      return true;
    },
    async rename_workspace_directory(args) {
      const { data } = await api.renameDirectory({
        body: { ...args, root: args.root ?? "" },
        throwOnError: true,
      });
      return pathSchema.parse(data).relativePath;
    },
    async delete_workspace_directory(args) {
      await api.deleteDirectory({
        body: { ...args, root: args.root ?? "" },
        throwOnError: true,
      });
      return true;
    },
    async import_workspace_resources(args) {
      const files = await pickFiles();
      const imported: string[] = [];
      const failed: { name: string; message: string }[] = [];
      for (const file of files) {
        try {
          if (file.size > 64 * 1024 * 1024) throw new Error("文件超过64 MiB");
          imported.push(
            await workspace.import_workspace_resource_bytes({
              ...args,
              name: file.name,
              bytes: new Uint8Array(await file.arrayBuffer()),
            }),
          );
        } catch (error) {
          failed.push({
            name: file.name,
            message: error instanceof Error ? error.message : "导入失败",
          });
        }
      }
      return { imported, failed };
    },
    async import_workspace_resource_bytes({ bytes, ...args }) {
      const { data } = await api.importFile({
        body: {
          ...args,
          root: args.root ?? "",
          bytesBase64: encodeBytes(bytes),
        },
        throwOnError: true,
      });
      return pathSchema.parse(data).relativePath;
    },
    open_workspace_resource_external: external,
    async resolve_workspace_resource_reference(args) {
      const { data } = await api.resolveReference({
        body: { ...args, baseDirectory: args.baseDirectory ?? "" },
        throwOnError: true,
      });
      return resolutionSchema.parse(data);
    },
    async stat_workspace_resource(args) {
      const { data } = await api.statFile({ query: args, throwOnError: true });
      return resourceSchema.nullish().parse(data) ?? null;
    },
    async read_workspace_resource({ requestId, ...args }) {
      const key = `${args.scopeId}:${requestId}`;
      const controller = new AbortController();
      reads.set(key, controller);
      try {
        return await read(args, controller.signal);
      } finally {
        reads.delete(key);
      }
    },
    async cancel_workspace_resource_read({ scopeId, requestId }) {
      const key = `${scopeId}:${requestId}`;
      const current = reads.get(key);
      current?.abort();
      reads.delete(key);
      return Boolean(current);
    },
    async write_workspace_resource(args) {
      const { data } = await api.writeFile({
        body: { ...args, root: args.root ?? "" },
        throwOnError: true,
      });
      return writeSchema.parse(data);
    },
    async watch_workspace_resource(args) {
      const { data } = await api.watchResource({
        body: {
          ...args,
          root: args.root ?? "",
          directory: args.directory ?? false,
        },
        throwOnError: true,
      });
      const ready = changeSchema.parse(data);
      let sequence = 0;
      const stop = subscribeGlobalEvents(
        "workspace",
        { kind: "workspace_watch", id: ready.subscriptionId },
        {
          onEvent(frame) {
            if (frame.type !== "workspace_changed") return;
            const parsed = changeSchema.safeParse(frame.payload);
            if (parsed.success) {
              const value = { ...parsed.data, sequence: ++sequence };
              for (const listener of listeners) listener(value);
            }
          },
          onState(state) {
            if (state === "unavailable") {
              const value = {
                ...ready,
                state: "failed" as const,
                sequence: ++sequence,
              };
              for (const listener of listeners) listener(value);
            }
          },
        },
      );
      watches.set(ready.subscriptionId, stop);
      return ready;
    },
    async unwatch_workspace_resource({ subscriptionId }) {
      watches.get(subscriptionId)?.();
      watches.delete(subscriptionId);
      await api.unwatchResource({
        query: { subscriptionId },
        throwOnError: true,
      });
      return true;
    },
    onWorkspaceResourceChange(handler) {
      listeners.add(handler);
      return () => {
        listeners.delete(handler);
      };
    },
    async search_workspace_files({ root, ...args }) {
      const current = await draftScope(root);
      return workspace.search_workspace_resource_files({
        ...args,
        root,
        scopeId: current.id,
        scopePath: args.scopePath ?? "",
      });
    },
    async list_workspace_directory({ root, ...args }) {
      const current = await draftScope(root);
      return workspace.list_workspace_resource_directory({
        ...args,
        root,
        scopeId: current.id,
      });
    },
    async resolve_file_reference({
      request,
    }): Promise<FileReferenceResolution> {
      const current = await draftScope(request.workspaceRoot, [
        ...request.sourceRoots,
      ]);
      return workspace.resolve_workspace_resource_reference({
        scopeId: current.id,
        path: request.path,
        baseDirectory: request.baseDirectory,
      });
    },
    read_workspace_file: ({ root, relativePath }) =>
      preview(root, relativePath),
    async write_workspace_file({ root, relativePath, content }) {
      const current = await draftScope(root);
      const args = { scopeId: current.id, root, relativePath };
      const file = await workspace.stat_workspace_resource(args);
      if (!file) throw new Error("文件不存在");
      await workspace.write_workspace_resource({
        ...args,
        content,
        expectedVersion: file.version,
        saveRevision: 0,
      });
      return true;
    },
    async list_chat_workspace_files({ chatId, root }) {
      const current = await chatScope(chatId);
      const { data } = await api.listChatFiles({
        query: { scopeId: current.id, root },
        throwOnError: true,
      });
      return chatFilesSchema.parse(data);
    },
    async download_chat_workspace_file({ chatId, root, relativePath }) {
      const current = await chatScope(chatId);
      return save({ scopeId: current.id, root, relativePath });
    },
    async delete_chat_workspace_file({ chatId, root, relativePath }) {
      const current = await chatScope(chatId);
      await api.deleteChatFile({
        query: { scopeId: current.id, root, relativePath },
        throwOnError: true,
      });
      return true;
    },
    async open_workspace_file({ root, relativePath }) {
      const current = await draftScope(root);
      return external({ scopeId: current.id, root, relativePath });
    },
  };
  async function gitArgs(args: {
    chatId: string;
    root: string;
    repository?: string;
  }) {
    const { chatId, ...rest } = args;
    return { ...rest, scopeId: (await chatScope(chatId)).id };
  }
  const git: GitService = {
    async scoped_git_status(args) {
      const { data } = await api.gitStatus({
        query: await gitArgs(args),
        throwOnError: true,
      });
      return gitStatusSchema.parse(data);
    },
    async scoped_git_diff(args) {
      const { data } = await api.gitDiff({
        query: { ...args, ...(await gitArgs(args)) },
        throwOnError: true,
      });
      return data.content ?? "";
    },
    async scoped_git_log(args) {
      const { data } = await api.gitLog({
        query: { ...args, ...(await gitArgs(args)) },
        throwOnError: true,
      });
      return gitLogSchema.parse(data);
    },
    async scoped_git_show(args) {
      const { data } = await api.gitShow({
        query: { ...args, ...(await gitArgs(args)) },
        throwOnError: true,
      });
      return data.content ?? null;
    },
    async scoped_git_commit_diff(args) {
      const { data } = await api.gitCommitDiff({
        query: { ...args, ...(await gitArgs(args)) },
        throwOnError: true,
      });
      return data.content ?? "";
    },
    async scoped_git_stage(args) {
      await api.gitStage({
        body: {
          ...args,
          ...(await gitArgs(args)),
          repository: args.repository,
        },
        throwOnError: true,
      });
    },
    async scoped_git_unstage(args) {
      await api.gitUnstage({
        body: {
          ...args,
          ...(await gitArgs(args)),
          repository: args.repository,
        },
        throwOnError: true,
      });
    },
    async scoped_git_commit(args) {
      await api.gitCommit({
        body: {
          ...args,
          ...(await gitArgs(args)),
          repository: args.repository,
        },
        throwOnError: true,
      });
    },
    async scoped_git_discard(args) {
      await api.gitDiscard({
        body: {
          ...args,
          ...(await gitArgs(args)),
          repository: args.repository,
        },
        throwOnError: true,
      });
      return true;
    },
    async get_workspace_git_changes({ root }): Promise<WorkspaceGitChanges> {
      const current = await draftScope(root);
      const { data: response } = await api.gitStatus({
        query: { scopeId: current.id, root },
        throwOnError: true,
      });
      const data = gitStatusSchema.parse(response);
      const files = (data.entries ?? []).map((entry) => ({
        path: entry.path,
        name: basename(entry.path),
        status: (entry.xy === "??"
          ? "untracked"
          : entry.xy.includes("D")
            ? "deleted"
            : entry.xy.includes("A")
              ? "added"
              : "modified") as "untracked" | "deleted" | "added" | "modified",
        addedLines: entry.stagedAddedLines + entry.unstagedAddedLines,
        deletedLines: entry.stagedDeletedLines + entry.unstagedDeletedLines,
        gitRoot: data.root ?? root,
      }));
      return {
        isGitRepo: data.isRepo,
        branch: data.branch,
        files,
        totalAddedLines: files.reduce((n, f) => n + f.addedLines, 0),
        totalDeletedLines: files.reduce((n, f) => n + f.deletedLines, 0),
      };
    },
    async list_workspace_git_branches({ root }) {
      const current = await draftScope(root);
      const { data } = await api.gitBranches({
        query: { scopeId: current.id, root },
        throwOnError: true,
      });
      return branchesSchema.parse(data);
    },
    async preview_workspace_git_checkout({ root, branch }) {
      const current = await draftScope(root);
      const { data } = await api.gitCheckoutPreview({
        query: { scopeId: current.id, root, branch },
        throwOnError: true,
      });
      return checkoutPreviewSchema.parse(data);
    },
    async checkout_workspace_git_branch({ root, branch, commitMessage }) {
      const current = await draftScope(root);
      const { data } = await api.gitCheckout({
        body: {
          scopeId: current.id,
          root,
          repository: "",
          branch,
          commitMessage: commitMessage ?? "",
        },
        throwOnError: true,
      });
      return checkoutSchema.parse(data);
    },
    async list_workspace_git_repositories({ roots }) {
      const repositories = [];
      const seen = new Set<string>();
      for (const root of roots) {
        const current = await draftScope(root);
        const { data: response } = await api.gitStatus({
          query: { scopeId: current.id, root },
          throwOnError: true,
        });
        const data = gitStatusSchema.parse(response);
        for (const gitRoot of data.repositories ?? []) {
          if (seen.has(gitRoot)) continue;
          seen.add(gitRoot);
          repositories.push({
            directory: gitRoot,
            gitRoot,
            label: basename(gitRoot),
            branch: gitRoot === data.root ? data.branch : undefined,
          });
        }
      }
      return { repositories };
    },
    async read_workspace_diff({ root, relativePath }) {
      const current = await draftScope(root);
      const { data } = await api.gitShow({
        query: { scopeId: current.id, root, revision: "HEAD", relativePath },
        throwOnError: true,
      });
      const file = await workspace.stat_workspace_resource({
        scopeId: current.id,
        root,
        relativePath,
      });
      return {
        name: basename(relativePath),
        relativePath,
        oldContent: data.content ?? "",
        newContent: file
          ? new TextDecoder().decode(
              (await read({ scopeId: current.id, root, relativePath })).bytes,
            )
          : "",
      };
    },
  };
  return { workspace, git };
}
