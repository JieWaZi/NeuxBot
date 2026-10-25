import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { mkdtemp, mkdir, writeFile, rm, realpath } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

// 只启动此次发布产物与临时数据，不打开或控制用户的运行中应用。
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const directory = path.resolve(
  process.argv[2] ??
    path.join(root, "web/release", `${process.platform}-${process.arch}`),
);
const temporary = await realpath(
  await mkdtemp(path.join(tmpdir(), "ally-web-smoke-")),
);
const workspace = path.join(temporary, "workspace");
await mkdir(workspace);
await writeFile(path.join(workspace, "hello.txt"), "hello\n");
execFileSync("git", ["init", "-q", workspace]);
const reserve = createServer();
await new Promise((resolve) => reserve.listen(0, "127.0.0.1", resolve));
const port = reserve.address().port;
await new Promise((resolve) => reserve.close(resolve));
const origin = `http://127.0.0.1:${port}`;
const child = spawn(process.execPath, [path.join(directory, "start-web.mjs")], {
  cwd: temporary,
  env: {
    ...process.env,
    ALLY_DATA_DIR: path.join(temporary, "data"),
    ALLY_MANAGED_FILES_DIR: path.join(temporary, "managed"),
    ALLY_WEB_CORE_PORT: String(port),
  },
  stdio: ["pipe", "pipe", "pipe"],
});
let passed = false;
let ownedPIDs = [];
function descendants(pid) {
  if (process.platform === "win32") return [];
  let ids;
  try {
    ids = execFileSync("pgrep", ["-P", String(pid)], { encoding: "utf8" })
      .trim()
      .split(/\s+/)
      .filter(Boolean)
      .map(Number);
  } catch {
    return [];
  }
  return ids.flatMap((id) => [id, ...descendants(id)]);
}
function assertStopped(ids) {
  for (const pid of ids) {
    let alive = false;
    try {
      process.kill(pid, 0);
      alive = true;
    } catch {}
    assert.equal(alive, false, "Core或worker仍存活");
  }
}
let diagnostic = "";
let exited = false;
child.stdout.resume();
child.stderr.on("data", (data) => {
  diagnostic = (diagnostic + data.toString()).slice(-8192);
});
child.on("exit", () => {
  exited = true;
});
async function waitFor(predicate) {
  for (let attempt = 0; attempt < 300; attempt++) {
    const value = await predicate();
    if (value) return value;
    if (exited)
      throw new Error(
        "发布Core提前退出: " +
          diagnostic.replace(/[a-f0-9]{64}/g, "[redacted]"),
      );
    await delay(100);
  }
  throw new Error("发布Core等待超时");
}
async function connect() {
  const response = await fetch(`${origin}/api/web/session`, {
    method: "POST",
    headers: { Origin: origin },
  });
  assert.equal(response.status, 200, "建立会话失败");
  const cookie = response.headers.get("set-cookie");
  assert.match(cookie, /HttpOnly/i);
  assert.match(cookie, /SameSite=Strict/i);
  return { cookie: cookie.split(";")[0], session: await response.json() };
}
async function request(route, auth, body, method) {
  return fetch(origin + route, {
    method: method ?? (body === undefined ? "GET" : "POST"),
    headers: {
      Origin: origin,
      Cookie: auth.cookie,
      "X-Ally-CSRF": auth.session.csrfToken,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}
try {
  await waitFor(async () => {
    try {
      return (await fetch(origin)).status === 200;
    } catch {
      return false;
    }
  });
  assert.equal((await fetch(`${origin}/api/web/session`)).status, 401);
  ownedPIDs = descendants(child.pid);
  const first = await connect();
  assert.equal(first.session.capabilities.workspace, true);
  assert.equal(first.session.capabilities.git, true);
  assert.equal(first.session.capabilities.terminal, true);
  assert.equal(
    (
      await fetch(`${origin}/api/web/session`, {
        method: "POST",
        headers: { Origin: "http://evil.example" },
      })
    ).status,
    403,
    "外部来源不得建立会话",
  );
  assert.equal(
    (
      await fetch(`${origin}/api/v1/uploads?name=denied.txt`, {
        method: "POST",
        headers: { Origin: origin, Cookie: first.cookie },
        body: "denied",
      })
    ).status,
    401,
    "缺CSRF应拒绝",
  );
  const upload = await fetch(`${origin}/api/v1/uploads?name=upload.txt`, {
    method: "POST",
    headers: {
      Origin: origin,
      Cookie: first.cookie,
      "X-Ally-CSRF": first.session.csrfToken,
      "Content-Type": "application/octet-stream",
    },
    body: "upload content",
  });
  assert.equal(upload.status, 200);
  const ref = (await upload.json()).sourcePath;
  assert.match(ref, /^upload:/);
  const opened = await request("/api/v1/workspace/draft-scopes", first, {
    root: workspace,
    additionalRoots: [],
  });
  assert.equal(opened.status, 200);
  const scope = await opened.json();
  assert.ok(scope.id);
  const query = new URLSearchParams({
    scopeId: scope.id,
    relativePath: "hello.txt",
    root: workspace,
  });
  const read = await request(`/api/v1/workspace/read?${query}`, first);
  assert.equal(read.status, 200);
  const file = await read.json();
  assert.equal(Buffer.from(file.bytesBase64, "base64").toString(), "hello\n");
  const body = {
    scopeId: scope.id,
    root: workspace,
    relativePath: "hello.txt",
    content: "updated\n",
    expectedVersion: file.resource.version,
    saveRevision: 1,
  };
  assert.equal(
    (await request("/api/v1/workspace/write", first, body)).status,
    200,
  );
  assert.equal(
    (await request("/api/v1/workspace/write", first, body)).ok,
    false,
    "旧版本不得覆盖",
  );
  const git = await request(
    `/api/v1/workspace/git/status?${new URLSearchParams({ scopeId: scope.id, root: workspace })}`,
    first,
  );
  assert.equal(git.status, 200);
  assert.equal((await git.json()).isRepo, true);
  assert.equal(
    (
      await request(
        `/api/v1/workspace/read?${new URLSearchParams({ scopeId: scope.id, relativePath: "../outside.txt", root: workspace })}`,
        first,
      )
    ).ok,
    false,
  );
  const second = await connect();
  assert.equal(
    (await request(`/api/v1/workspace/read?${query}`, second)).ok,
    false,
    "范围不得跨会话使用",
  );
  const uploadRoute = `/api/v1/uploads?${new URLSearchParams({ sourcePath: ref })}`;
  assert.equal(
    (await request(uploadRoute, second, undefined, "DELETE")).ok,
    false,
    "上传不得跨会话使用",
  );
  assert.equal(
    (await request(uploadRoute, first, undefined, "DELETE")).status,
    200,
  );
  assert.equal(
    (
      await request("/api/v1/terminals", first, {
        chatId: "missing",
        ownerKey: "smoke",
        root: workspace,
        createWhenMissing: true,
      })
    ).ok,
    false,
    "终端需要真实冻结Chat",
  );
  assert.equal(
    (await request("/api/web/session", first, undefined, "DELETE")).status,
    200,
  );
  assert.equal((await request("/api/web/session", first)).status, 401);
  passed = true;
} finally {
  child.kill("SIGTERM");
  for (let attempt = 0; attempt < 100 && !exited; attempt++) await delay(100);
  if (!exited) {
    child.kill("SIGKILL");
    throw new Error("发布Core未在10秒内完成关闭");
  }
  assertStopped(ownedPIDs);
  if (passed) {
    const restarted = spawn(
      process.execPath,
      [path.join(directory, "start-web.mjs")],
      {
        cwd: temporary,
        env: {
          ...process.env,
          ALLY_DATA_DIR: path.join(temporary, "data"),
          ALLY_MANAGED_FILES_DIR: path.join(temporary, "managed"),
          ALLY_WEB_CORE_PORT: String(port),
        },
        stdio: ["pipe", "pipe", "pipe"],
      },
    );
    let ready = false,
      closed = false;
    restarted.stdout.on("data", (data) => {
      if (data.toString().includes("NeuxBot Web:")) ready = true;
    });
    restarted.stderr.on("data", () => {});
    restarted.on("exit", () => {
      closed = true;
    });
    let restartedPIDs = [];
    try {
      for (let attempt = 0; attempt < 300 && !ready && !closed; attempt++)
        await delay(100);
      assert.equal(ready, true, "原数据锁未能重开");
      for (let attempt = 0; attempt < 300; attempt++) {
        try {
          if ((await fetch(`${origin}/api/web/session`)).status === 401) break;
        } catch {}
        if (attempt === 299) throw new Error("重开Core不可用");
        await delay(100);
      }
      restartedPIDs = descendants(restarted.pid);
    } finally {
      restarted.kill("SIGTERM");
      for (let attempt = 0; attempt < 100 && !closed; attempt++)
        await delay(100);
      if (!closed) {
        restarted.kill("SIGKILL");
        throw new Error("重开Core未完成关闭");
      }
      assertStopped(restartedPIDs);
    }
    console.log(
      "Web发布包HTTP验证通过：直接连接/会话撤销、CSRF、上传隔离、文件版本/越界、Git、终端授权、Core与worker回收、同数据锁重开。",
    );
  }
  await rm(temporary, { recursive: true, force: true });
}
