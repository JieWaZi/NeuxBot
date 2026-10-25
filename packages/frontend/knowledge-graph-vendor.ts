import { readFile } from "node:fs/promises";
import type { Plugin } from "vite";

/** 图谱依赖使用独立模块作用域，避免 D3 原型扩展扩大启动时的 Mermaid 共享块。 */
export function graphVendorScope(): Plugin {
  const suffix = "?knowledge-graph-d3";
  const graphPackage =
    /node_modules[\\/](?:force-graph|react-force-graph-2d)[\\/]/;
  const reactPackage = /node_modules[\\/]react(?:-dom)?[\\/]/;
  return {
    name: "knowledge-graph-d3-scope",
    enforce: "pre",
    apply: "build",
    async resolveId(source, importer) {
      if (
        !importer ||
        !(
          importer.endsWith(suffix) ||
          graphPackage.test(importer) ||
          importer.endsWith("knowledge-force-engine.ts")
        )
      )
        return;
      const base = importer.endsWith(suffix)
        ? importer.slice(0, -suffix.length)
        : importer;
      const resolved = await this.resolve(source, base, { skipSelf: true });
      if (
        resolved &&
        !resolved.external &&
        !resolved.id.includes("\0") &&
        /\.[cm]?js$/.test(resolved.id) &&
        resolved.id.includes("node_modules") &&
        !reactPackage.test(resolved.id)
      )
        return { ...resolved, id: resolved.id + suffix };
    },
    async load(id) {
      if (id.endsWith(suffix))
        return readFile(id.slice(0, -suffix.length), "utf8");
    },
  };
}
