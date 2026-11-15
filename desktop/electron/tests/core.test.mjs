import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, writeFile, rm, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { loadModule } from "./load-module.mjs";
const { CoreSupervisor, validateCoreURL } = loadModule(
  "main/core-supervisor.ts",
);
const fixture = async (t, body) => {
  const root = await mkdtemp(path.join(tmpdir(), "bot-core-test-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const binary = path.join(root, "core");
  await writeFile(binary, `#!${process.execPath}\n${body}`, { mode: 0o755 });
  return { root, binary };
};
test("拒绝远程、含凭据或附带路径的 Core 地址", () => {
  for (const url of [
    "https://127.0.0.1:8432",
    "http://example.com",
    "http://u:p@127.0.0.1",
    "http://127.0.0.1/path",
  ])
    assert.throws(() => validateCoreURL(url));
  assert.equal(
    validateCoreURL("http://127.0.0.1:8432").origin,
    "http://127.0.0.1:8432",
  );
});
test("外部 Core 不创建或终止子进程", async () => {
  const core = new CoreSupervisor({
    binary: "/does/not/exist",
    dataDir: "/not/used",
    env: {
      NEUXBOT_CORE_URL: "http://127.0.0.1:9000",
      NEUXBOT_LOCAL_TOKEN: "test",
    },
  });
  assert.deepEqual(await core.connection(), {
    baseUrl: "http://127.0.0.1:9000",
    token: "test",
  });
  await core.stop();
  await core.stop();
  await assert.rejects(core.connection(), /停止/);
});
test("并发连接只启动一个 Core 并等待认证健康响应", async (t) => {
  const { root, binary } = await fixture(
    t,
    `const fs=require('node:fs');fs.appendFileSync(process.env.NEUXBOT_DATA_DIR+'/starts','1');const server=require('node:http').createServer((req,res)=>{res.setHeader('content-type','application/json');if(req.headers.authorization!=='Bearer '+process.env.NEUXBOT_LOCAL_TOKEN){res.writeHead(401);res.end('{}');return;}res.end(JSON.stringify({status:'ok'}));});server.listen(Number(process.env.SERVER_PORT),'127.0.0.1');process.on('SIGTERM',()=>server.close(()=>process.exit(0)));`,
  );
  const core = new CoreSupervisor({
    binary,
    dataDir: root,
    startupTimeout: 5000,
    stopTimeout: 100,
  });
  t.after(() => core.stop());
  const [a, b] = await Promise.all([core.connection(), core.connection()]);
  assert.deepEqual(a, b);
  assert.equal(await readFile(path.join(root, "starts"), "utf8"), "1");
  await core.stop();
  await assert.rejects(fetch(a.baseUrl));
});
test("启动失败可重试，超时会回收忽略 SIGTERM 的子进程", async (t) => {
  const { root, binary } = await fixture(
    t,
    "process.on('SIGTERM',()=>{});setInterval(()=>{},1000);",
  );
  const core = new CoreSupervisor({
    binary,
    dataDir: root,
    startupTimeout: 150,
    stopTimeout: 50,
  });
  t.after(() => core.stop());
  await assert.rejects(core.connection(), /超时/);
  await assert.rejects(core.connection(), /超时/);
  await core.stop();
});
test("启动中退出不会留下 Core", async (t) => {
  const { root, binary } = await fixture(t, "setInterval(()=>{},1000);");
  const core = new CoreSupervisor({
    binary,
    dataDir: root,
    startupTimeout: 500,
    stopTimeout: 50,
  });
  const pending = core.connection();
  const rejected = assert.rejects(pending, /取消/);
  await core.stop();
  await rejected;
});
