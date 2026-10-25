import { readFile, writeFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

// 合同来源是实际 Handler 的公开登记；不按路径前缀猜测允许项。
export function projectPublicOpenAPI(source, routes) {
  const out = {
    openapi: source.openapi,
    info: { title: "NeuxBot 集成 API", version: "1" },
    paths: {},
    components: {
      securitySchemes: {
        APIKey: { type: "apiKey", in: "header", name: "X-API-Key" },
      },
    },
  };
  const seen = new Set();
  for (const route of routes) {
    const path = route.path.replace(/\/[:*]([^/]+)/g, "/{$1}");
    const method = route.method.toLowerCase();
    const key = `${method} ${path}`;
    if (seen.has(key)) throw new Error(`duplicate public route: ${key}`);
    seen.add(key);
    const operation = source.paths?.[path]?.[method];
    if (!operation) throw new Error(`missing public route: ${key}`);
    if (operation.operationId !== route.operationId)
      throw new Error(`operationId mismatch: ${key}`);
    const projected = structuredClone(operation);
    projected.security = [{ APIKey: [] }];
    (out.paths[path] ??= {})[method] = projected;
  }
  const references = new Set();
  function visit(node) {
    if (!node || typeof node !== "object") return;
    if (node.$ref) {
      const ref = node.$ref;
      if (typeof ref !== "string" || !ref.startsWith("#/components/"))
        throw new Error(`unsupported reference: ${ref}`);
      if (!references.has(ref)) {
        references.add(ref);
        const parts = ref
          .slice(2)
          .split("/")
          .map((part) => part.replace(/~1/g, "/").replace(/~0/g, "~"));
        let value = source;
        for (const part of parts) value = value?.[part];
        if (value === undefined)
          throw new Error(`unresolved reference: ${ref}`);
        if (parts.length !== 3)
          throw new Error(`unsupported reference: ${ref}`);
        const [, kind, name] = parts;
        (out.components[kind] ??= {})[name] = structuredClone(value);
        visit(value);
      }
    }
    for (const value of Object.values(node)) visit(value);
  }
  visit(out.paths);
  return out;
}
if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const [input, output] = process.argv.slice(2);
  if (!input || !output) throw new Error("需要生成源与输出文件路径");
  const { document, routes } = JSON.parse(await readFile(input, "utf8"));
  await writeFile(
    output,
    `${JSON.stringify(projectPublicOpenAPI(document, routes), null, 2)}\n`,
  );
}
