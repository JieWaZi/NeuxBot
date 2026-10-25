const { strict: assert } = require("node:assert");
const { mkdtemp, rm, readdir, readFile } = require("node:fs/promises");
const { brotliDecompressSync } = require("node:zlib");
const { tmpdir } = require("node:os");
const path = require("node:path");
const resources = process.argv[2];
if (!resources) throw new Error("需要传入应用 Resources 目录");
const pty = require(
  path.join(resources, "terminal-runtime/node_modules/node-pty"),
);
async function checkRendererBytes(source, packaged) {
  let compressed = 0;
  for (const entry of await readdir(source, { withFileTypes: true })) {
    if (entry.name === ".vite" || entry.name.endsWith(".map")) continue;
    const original = path.join(source, entry.name),
      target = path.join(packaged, entry.name);
    if (entry.isDirectory()) {
      compressed += await checkRendererBytes(original, target);
      continue;
    }
    let actual;
    try {
      actual = await readFile(target);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
      actual = brotliDecompressSync(await readFile(`${target}.br`));
      compressed++;
    }
    assert.deepEqual(
      actual,
      await readFile(original),
      `Renderer asset changed: ${entry.name}`,
    );
  }
  return compressed;
}
(async () => {
  if (process.argv[3]) {
    const count = await checkRendererBytes(
      process.argv[3],
      path.join(resources, "app.asar/dist"),
    );
    assert.ok(
      count > 0,
      "Packaged Renderer should include compressed text assets",
    );
    process.stdout.write(
      `Packaged Renderer bytes: passed (${count} Brotli assets)\n`,
    );
  }

  const directory = await mkdtemp(path.join(tmpdir(), "ally-smoke-"));
  try {
    await new Promise((resolve, reject) => {
      const shell =
        process.platform === "win32"
          ? process.env.ComSpec || "cmd.exe"
          : "/bin/sh";
      const args =
        process.platform === "win32"
          ? ["/d", "/s", "/c", "echo ally-pty-ready"]
          : ["-c", "printf ally-pty-ready"];
      const child = pty.spawn(shell, args, {
        cwd: directory,
        cols: 80,
        rows: 24,
        env: process.env,
      });
      let output = "";
      const timer = setTimeout(() => {
        child.kill();
        reject(new Error("终端输出超时"));
      }, 5000);
      child.onData((chunk) => {
        output += chunk;
      });
      child.onExit(({ exitCode }) => {
        clearTimeout(timer);
        try {
          assert.equal(exitCode, 0);
          assert.match(output, /ally-pty-ready/);
          resolve();
        } catch (error) {
          reject(error);
        }
      });
    });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
})().then(
  () => {
    // node-pty 的 Windows ConPTY worker 在 Shell 正常退出后仍可能保持事件循环。
    // 此进程只验证 PTY；断言和临时目录清理完成后，刷完结果再结束独立测试进程。
    process.stdout.write("Packaged Electron PTY: passed\n", () =>
      process.exit(0),
    );
  },
  (error) => {
    // 原生加载、命令执行、断言和清理失败都必须返回非零，不能被残留句柄拖成超时。
    process.stderr.write(String(error) + "\n", () => process.exit(1));
  },
);
