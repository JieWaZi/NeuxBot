import assert from "node:assert/strict";
import test from "node:test";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { ESLint } from "eslint";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const eslint = new ESLint({
  cwd: root,
  overrideConfig: {
    languageOptions: {
      parserOptions: { projectService: false, project: false },
    },
  },
});
async function restricted(source) {
  const [result] = await eslint.lintText(`import ${JSON.stringify(source)};`, {
    filePath: path.join(root, "src/shared/platform/import-boundary-probe.ts"),
  });
  return result.messages.filter(
    (message) => message.ruleId === "no-restricted-imports",
  );
}
test("PDF.js 浏览器资源可用，宿主与Node边界仍受限制", async () => {
  for (const source of [
    "pdfjs-dist/web/pdf_viewer.css",
    "pdfjs-dist/web/pdf_viewer.mjs",
  ]) {
    assert.deepEqual(await restricted(source), [], source);
  }
  for (const source of [
    "node:fs",
    "electron",
    "@neuxbot/web",
    "@neuxbot/desktop/platform",
    "../../../../web/src/platform/host",
    "../../../desktop/src/platform",
    "@/../../../../web/src/main",
  ]) {
    assert.ok((await restricted(source)).length > 0, source);
  }
});
