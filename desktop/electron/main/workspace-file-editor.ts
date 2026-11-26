import { spawn } from "node:child_process";
import { constants } from "node:fs";
import { access, realpath, stat } from "node:fs/promises";
import { homedir } from "node:os";
import path from "node:path";
import { z } from "zod";
import type { CoreConnection } from "../shared/contracts";
import {
  workspaceFileOpenTargets as knownTargets,
  type WorkspaceFileOpenRequest,
  type WorkspaceFileOpenTarget,
} from "@neuxbot/platform-contracts/workspace-file-editors";
import { workspacePath } from "./workspace-files";

type EditorTarget = Exclude<WorkspaceFileOpenTarget, "file-manager">;
const editors: Record<
  EditorTarget,
  { command: string; app: string; windowsDirectory: string; executable: string }
> = {
  vscode: {
    command: "code",
    app: "Visual Studio Code",
    windowsDirectory: "Microsoft VS Code",
    executable: "Code.exe",
  },
  "vscode-insiders": {
    command: "code-insiders",
    app: "Visual Studio Code - Insiders",
    windowsDirectory: "Microsoft VS Code Insiders",
    executable: "Code - Insiders.exe",
  },
  vscodium: {
    command: "codium",
    app: "VSCodium",
    windowsDirectory: "VSCodium",
    executable: "VSCodium.exe",
  },
  cursor: {
    command: "cursor",
    app: "Cursor",
    windowsDirectory: "Cursor",
    executable: "Cursor.exe",
  },
  zed: {
    command: "zeditor",
    app: "Zed",
    windowsDirectory: "Zed",
    executable: "zed.exe",
  },
};

async function editorExecutable(
  target: EditorTarget,
): Promise<string | undefined> {
  const editor = editors[target];
  const roots = (process.env.PATH ?? "")
    .split(path.delimiter)
    .filter((dir) => path.isAbsolute(dir));
  const candidates = roots.map((root) =>
    path.join(
      root,
      process.platform === "win32" ? editor.executable : editor.command,
    ),
  );
  if (process.platform === "darwin") {
    const bundled = [path.join(homedir(), "Applications"), "/Applications"].map(
      (root) =>
        path.join(
          root,
          `${editor.app}.app`,
          "Contents",
          ...(target === "zed"
            ? ["MacOS", "cli"]
            : ["Resources", "app", "bin", editor.command]),
        ),
    );
    if (target === "zed" || target === "cursor") candidates.unshift(...bundled);
    else candidates.push(...bundled);
  } else if (process.platform === "win32") {
    const userPrograms = process.env.LOCALAPPDATA
      ? path.join(process.env.LOCALAPPDATA, "Programs")
      : undefined;
    for (const root of [
      userPrograms,
      process.env.ProgramFiles,
      process.env["ProgramFiles(x86)"],
    ]) {
      if (root && path.isAbsolute(root))
        candidates.push(
          path.join(root, editor.windowsDirectory, editor.executable),
          path.join(root, editor.windowsDirectory, "bin", editor.executable),
        );
    }
  } else {
    for (const root of [
      path.join(homedir(), ".local", "bin"),
      "/usr/local/bin",
      "/usr/bin",
      "/snap/bin",
    ])
      candidates.push(path.join(root, editor.command));
  }
  for (const candidate of new Set(candidates)) {
    try {
      if (!(await stat(candidate)).isFile()) continue;
      await access(candidate, constants.X_OK);
      return candidate;
    } catch {
      // A missing installation is omitted from the chooser.
    }
  }
}

export async function workspaceFileOpenTargets(): Promise<
  WorkspaceFileOpenTarget[]
> {
  const targets = await Promise.all(
    knownTargets.map(async (target) =>
      target === "file-manager" || (await editorExecutable(target))
        ? target
        : undefined,
    ),
  );
  return targets.filter((target) => target !== undefined);
}

async function authorizeFile(
  connection: CoreConnection,
  input: WorkspaceFileOpenRequest,
): Promise<string> {
  const post = async (endpoint: string, body: object): Promise<unknown> => {
    const response = await fetch(
      `${connection.baseUrl}/api/v1/workspace/${endpoint}`,
      {
        method: "POST",
        headers: {
          Authorization: `Bearer ${connection.token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify(body),
        signal: AbortSignal.timeout(5000),
      },
    );
    if (!response.ok) {
      await response.body?.cancel();
      throw new Error("Cannot authorize the file for an external application");
    }
    return response.json();
  };
  const target = await workspacePath(input.root, input.relativePath);
  const scope = z
    .object({ id: z.string().min(1) })
    .parse(
      input.chatId
        ? await post("scopes", { chatId: input.chatId })
        : await post("draft-scopes", { root: input.root, additionalRoots: [] }),
    );
  const result = z
    .object({
      status: z.string(),
      canonicalPath: z.string().optional(),
    })
    .parse(
      await post("resolve", {
        scopeId: scope.id,
        path: target,
        baseDirectory: input.root,
      }),
    );
  if (
    result.status !== "resolved" ||
    !result.canonicalPath ||
    path.relative(target, result.canonicalPath) !== "" ||
    path.relative(target, await realpath(target)) !== "" ||
    !(await stat(target)).isFile()
  )
    throw new Error("The file is not authorized for an external application");
  return target;
}

export async function openWorkspaceFileTarget(
  connection: CoreConnection,
  input: WorkspaceFileOpenRequest,
  reveal: (path: string) => void,
): Promise<boolean> {
  const target = await authorizeFile(connection, input);
  if (input.target === "file-manager") {
    reveal(target);
    return true;
  }
  const command = await editorExecutable(input.target);
  if (!command) throw new Error("The selected editor is unavailable");
  const location = input.line ? `${target}:${input.line}` : target;
  const args =
    input.line && input.target !== "zed" ? ["--goto", location] : [location];
  if (input.target === "cursor") args.unshift("--classic");
  return new Promise((resolve, reject) => {
    const editor = spawn(command, args, {
      detached: true,
      shell: false,
      stdio: "ignore",
      windowsHide: true,
    });
    editor.once("error", reject);
    editor.once("spawn", () => {
      editor.unref();
      resolve(true);
    });
  });
}
