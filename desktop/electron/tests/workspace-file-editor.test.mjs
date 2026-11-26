import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { mkdtemp, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { test } from "node:test";
import { loadModule } from "./load-module.mjs";

test("外部编辑器仅打开 Core 授权的标准文件并使用无 Shell 的固定参数", async (t) => {
  const root = await realpath(
    await mkdtemp(path.join(tmpdir(), "bot-editor-")),
  );
  const executable = path.join(
    root,
    process.platform === "win32" ? "Code.exe" : "code",
  );
  const name = "-source with spaces.ts";
  const target = path.join(root, name);
  await writeFile(target, "export {}");
  await writeFile(executable, "", { mode: 0o755 });
  const originalPath = process.env.PATH;
  const originalFetch = globalThis.fetch;
  process.env.PATH = root;
  t.after(async () => {
    if (originalPath === undefined) delete process.env.PATH;
    else process.env.PATH = originalPath;
    globalThis.fetch = originalFetch;
    await rm(root, { recursive: true, force: true });
  });

  let authorized = false;
  const calls = [];
  globalThis.fetch = async (url, options) => {
    const request = JSON.parse(options.body);
    assert.equal(options.headers.Authorization, "Bearer token");
    if (String(url).endsWith("/scopes")) {
      assert.deepEqual(request, { chatId: "chat-1" });
      return Response.json({ id: "scope" });
    }
    assert.match(String(url), /\/workspace\/resolve$/);
    assert.deepEqual(request, {
      scopeId: "scope",
      path: target,
      baseDirectory: root,
    });
    return Response.json(
      authorized
        ? { status: "resolved", canonicalPath: target }
        : { status: "outside_scope" },
    );
  };
  const { workspaceFileOpenTargets, openWorkspaceFileTarget } = loadModule(
    "main/workspace-file-editor.ts",
    {
      "node:child_process": {
        spawn(command, args, options) {
          calls.push({ command, args, options });
          const child = new EventEmitter();
          child.unref = () => {};
          queueMicrotask(() => child.emit("spawn"));
          return child;
        },
      },
    },
  );
  const input = {
    chatId: "chat-1",
    root,
    relativePath: name,
    target: "vscode",
    line: 9,
  };
  const connection = { baseUrl: "http://127.0.0.1:1234", token: "token" };
  assert.ok((await workspaceFileOpenTargets()).includes("vscode"));
  await assert.rejects(
    openWorkspaceFileTarget(connection, input, () => assert.fail()),
    /authorized/,
  );
  assert.equal(calls.length, 0);

  authorized = true;
  await openWorkspaceFileTarget(connection, input, () => assert.fail());
  assert.equal(calls.length, 1);
  assert.equal(calls[0].command, executable);
  assert.deepEqual(calls[0].args, ["--goto", `${target}:9`]);
  assert.equal(calls[0].options.shell, false);
});

test("编辑器 IPC 只允许已知标识和固定字段", () => {
  const { commandSchemas } = loadModule("shared/validation.ts");
  const schema = commandSchemas.open_workspace_file_target;
  const input = { root: "/project", relativePath: "file.ts", target: "vscode" };
  assert.equal(schema.safeParse(input).success, true);
  assert.equal(
    schema.safeParse({ ...input, target: "/bin/sh" }).success,
    false,
  );
  assert.equal(schema.safeParse({ ...input, command: "other" }).success, false);
  assert.equal(schema.safeParse({ ...input, line: 0 }).success, false);
});
