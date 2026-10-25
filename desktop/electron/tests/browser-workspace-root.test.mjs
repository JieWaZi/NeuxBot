import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, realpath, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { loadModule } from "./load-module.mjs";

test("浏览器每次从Core读取冻结范围，旧授权不会跨请求缓存", async (t) => {
  const root = await realpath(
    await mkdtemp(path.join(tmpdir(), "ally-browser-root-")),
  );
  const other = await realpath(
    await mkdtemp(path.join(tmpdir(), "ally-browser-other-")),
  );
  t.after(() =>
    Promise.all([
      rm(root, { recursive: true, force: true }),
      rm(other, { recursive: true, force: true }),
    ]),
  );
  const original = globalThis.fetch;
  t.after(() => {
    globalThis.fetch = original;
  });
  let roots = [other];
  let calls = 0;
  globalThis.fetch = async (url, options) => {
    calls++;
    assert.match(url, /\/api\/v1\/chats\/chat-1$/);
    assert.equal(options.headers.Authorization, "Bearer token");
    return new Response(
      JSON.stringify({
        chat: {
          id: "chat-1",
          workspaceDirectory: root,
          additionalWorkingDirectories: roots,
        },
      }),
    );
  };
  const { authorizeBrowserRoot } = loadModule("main/browser-workspace-root.ts");
  const core = {
    connection: async () => ({
      baseUrl: "http://127.0.0.1:1234",
      token: "token",
    }),
  };
  assert.equal(await authorizeBrowserRoot(core, "chat-1", other), other);
  roots = [];
  await assert.rejects(authorizeBrowserRoot(core, "chat-1", other), /不属于/);
  assert.equal(await authorizeBrowserRoot(core, "chat-1", root), root);
  assert.equal(calls, 3);
});
