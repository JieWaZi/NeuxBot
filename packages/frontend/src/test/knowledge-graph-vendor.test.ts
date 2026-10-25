import { expect, it, vi } from "vitest";
import { graphVendorScope } from "../../knowledge-graph-vendor";

async function resolveGraph(
  source: string,
  importer: string,
  resolved: { id: string; external?: boolean; moduleSideEffects?: boolean },
) {
  const plugin = graphVendorScope();
  const resolve = vi.fn(async () => resolved);
  if (typeof plugin.resolveId !== "function") throw new Error("缺少解析钩子");
  const result: unknown = await Reflect.apply(plugin.resolveId, { resolve }, [
    source,
    importer,
    {},
  ]);
  return { result, resolve };
}

it("启动模块的 D3 保持原解析结果，不被图谱扩充共享块", async () => {
  const { result, resolve } = await resolveGraph("d3-zoom", "/src/browser.ts", {
    id: "/node_modules/d3-zoom/src/index.js",
  });
  expect(result).toBeUndefined();
  expect(resolve).not.toHaveBeenCalled();
});
it("图谱依赖递归隔离且保留模块副作用信息，React 仍共用同一实例", async () => {
  const importer = "/node_modules/force-graph/dist/force-graph.mjs";
  const { result } = await resolveGraph("d3-selection", importer, {
    id: "/node_modules/d3-selection/src/index.js",
    moduleSideEffects: true,
  });
  expect(result).toEqual({
    id: "/node_modules/d3-selection/src/index.js?knowledge-graph-d3",
    moduleSideEffects: true,
  });
  const child = await resolveGraph(
    "./selection/index.js",
    "/node_modules/d3-selection/src/index.js?knowledge-graph-d3",
    { id: "/node_modules/d3-selection/src/selection/index.js" },
  );
  expect(child.resolve).toHaveBeenCalledWith(
    "./selection/index.js",
    "/node_modules/d3-selection/src/index.js",
    { skipSelf: true },
  );
  expect(child.result).toHaveProperty(
    "id",
    "/node_modules/d3-selection/src/selection/index.js?knowledge-graph-d3",
  );
  const react = await resolveGraph(
    "react",
    "/node_modules/react-force-graph-2d/dist/index.js",
    { id: "/node_modules/react/index.js" },
  );
  expect(react.result).toBeUndefined();
});

it("资源查询、虚拟模块和外部模块交还 Vite 处理", async () => {
  const importer = "/node_modules/force-graph/dist/force-graph.mjs";
  for (const resolved of [
    { id: "/node_modules/example/index.js?raw" },
    { id: "/node_modules/example/index.js#fragment" },
    { id: "/node_modules/example/style.css" },
    { id: "\0/node_modules/example/index.js" },
    { id: "/node_modules/example/index.js", external: true },
  ]) {
    const { result } = await resolveGraph("example", importer, resolved);
    expect(result).toBeUndefined();
  }
});
