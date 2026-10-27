import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile, access } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { smokeCore } from "./smoke-core.mjs";

test("Core 启动失败保留有界脱敏诊断，并清理隔离数据", async (t) => {
  const directory = await mkdtemp(path.join(tmpdir(), "core-smoke-failure-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const fixture = path.join(directory, "failure.cjs");
  const record = path.join(directory, "data-dir.txt");
  const token = `ally_${"a".repeat(32)}`;
  await writeFile(
    fixture,
    `
require("node:fs").writeFileSync(${JSON.stringify(record)}, process.env.ALLY_DATA_DIR);
console.error("x".repeat(70000));
console.error("fixture panic: ${token}");
process.exitCode = 2;
`,
  );
  const diagnostic = [];
  await assert.rejects(
    smokeCore({
      binary: process.execPath,
      resources: directory,
      executable: process.execPath,
      env: {
        ...process.env,
        NODE_OPTIONS: `--require=${JSON.stringify(fixture)}`,
      },
      diagnostic: (text) => diagnostic.push(text),
    }),
    /本地服务已退出 \(2\)/,
  );
  assert.equal(diagnostic.length, 1);
  assert.match(diagnostic[0], /fixture panic: \[redacted\]/);
  assert.ok(diagnostic[0].length < 66000);
  assert.ok(!diagnostic[0].includes(token));
  await assert.rejects(access(await readFile(record, "utf8")), {
    code: "ENOENT",
  });
});
