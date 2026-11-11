import { test } from "node:test";
import assert from "node:assert/strict";
import { loadModule } from "./load-module.mjs";

const { rendererCSP } = loadModule("main/renderer-protocol.ts");

test("网站图标只增加 favicon 路径授权，脚本和网络连接仍限制在原边界", async () => {
  const policy = await rendererCSP("bundle.js");
  const directives = Object.fromEntries(
    policy.split(";").map((part) => {
      const [name, ...sources] = part.trim().split(/\s+/);
      return [name, sources];
    }),
  );
  assert.ok(directives["img-src"].includes("https://*/favicon.ico"));
  assert.ok(directives["img-src"].includes("http://*/favicon.ico"));
  for (const name of ["img-src", "script-src", "connect-src"]) {
    assert.ok(!directives[name].includes("https:"));
    assert.ok(!directives[name].includes("http:"));
    assert.ok(!directives[name].includes("*"));
  }
});
