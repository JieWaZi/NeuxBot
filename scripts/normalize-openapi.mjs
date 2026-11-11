import { readFile, writeFile } from "node:fs/promises";

const [filePath] = process.argv.slice(2);
if (!filePath) throw new Error("OpenAPI 文件路径不能为空");

const document = JSON.parse(await readFile(filePath, "utf8"));
const schemas = document.components?.schemas;
if (schemas && typeof schemas === "object") {
  for (const [name, schema] of Object.entries(schemas)) {
    if (schema === null) schemas[name] = {};
    if (name === "GithubComGinGonicGinHandlerFunc") schemas[name] = {};
  }
}

// Go 编译器生成封闭画布联合；toolx 的 map 类型不能表达 kind 与 execution 的关联。
// 此处仅重定位本地引用，不另行维护节点或字段契约。
if (schemas && typeof schemas === "object") {
  const name = Object.keys(schemas).find((key) =>
    key.endsWith("DslCanvasManifest"),
  );
  if (name) {
    const canvas = JSON.parse(
      await readFile(
        new URL(
          "../server/internal/workflow/dsl/generated/canvas-manifest.schema.json",
          import.meta.url,
        ),
        "utf8",
      ),
    );
    const prefix = name.slice(0, -"CanvasManifest".length);
    const relocate = (value) => {
      if (Array.isArray(value)) return value.map(relocate);
      if (!value || typeof value !== "object") return value;
      return Object.fromEntries(
        Object.entries(value).map(([key, child]) => [
          key,
          key === "$ref" &&
          typeof child === "string" &&
          child.startsWith("#/$defs/")
            ? `#/components/schemas/${prefix}${child.slice("#/$defs/".length)}`
            : relocate(child),
        ]),
      );
    };
    for (const [type, schema] of Object.entries(canvas.$defs)) {
      schemas[`${prefix}${type}`] = relocate(schema);
    }
  }
}

function normalizeParameterOrder(value) {
  if (Array.isArray(value)) {
    for (const item of value) normalizeParameterOrder(item);
    return;
  }
  if (!value || typeof value !== "object") return;
  if (Array.isArray(value.parameters)) {
    value.parameters.sort((left, right) =>
      `${left.in ?? ""}:${left.name ?? ""}`.localeCompare(
        `${right.in ?? ""}:${right.name ?? ""}`,
      ),
    );
  }
  for (const child of Object.values(value)) normalizeParameterOrder(child);
}

function normalizeUnspecifiedEnumValues(value) {
  if (Array.isArray(value)) {
    for (const item of value) normalizeUnspecifiedEnumValues(item);
    return;
  }
  if (!value || typeof value !== "object") return;

  const enumLabels = value["x-enum-labels"];
  // toolx 把空字符串暂记为 unknown；同时存在真实 unknown 成员时，标签映射会被覆盖。
  const unspecifiedLine =
    typeof value.description === "string"
      ? value.description
          .split("\n")
          .find(
            (line) =>
              line.startsWith("* `unknown` - ") && line.includes("Unspecified"),
          )
      : undefined;
  const unknownLabel =
    typeof enumLabels?.unknown === "string" &&
    enumLabels.unknown.includes("Unspecified")
      ? enumLabels.unknown
      : unspecifiedLine?.slice("* `unknown` - ".length);
  const unknownIndex = Array.isArray(value.enum)
    ? value.enum.indexOf("unknown")
    : -1;
  if (
    value.type === "string" &&
    unknownIndex >= 0 &&
    typeof unknownLabel === "string" &&
    unknownLabel.includes("Unspecified")
  ) {
    value.enum[unknownIndex] = "";
    value["x-enum-labels"] = Object.fromEntries([
      ["", unknownLabel],
      ...Object.entries(enumLabels ?? {}).filter(
        ([key]) => key !== "unknown" || value.enum.includes("unknown"),
      ),
    ]);
    if (typeof value.description === "string") {
      value.description = value.description.replace(
        `* \`unknown\` - ${unknownLabel}`,
        `* \`\` - ${unknownLabel}`,
      );
    }
  }

  const additionalProperties = value.additionalProperties;
  if (
    additionalProperties &&
    typeof additionalProperties === "object" &&
    !Array.isArray(additionalProperties) &&
    Object.keys(additionalProperties).length === 0
  ) {
    value.additionalProperties = true;
  }

  for (const child of Object.values(value)) {
    normalizeUnspecifiedEnumValues(child);
  }
}

function normalizeUsageNullability(schemas) {
  if (!schemas) return;
  // Toolx omits Go pointer nullability. These usage counters deliberately encode
  // missing evidence as JSON null so clients can distinguish it from zero.
  const prefix = "GithubComJieWaZiNeuxBotServer";
  const nullableFields = {
    InternalUsageTokens: [
      "input",
      "cacheRead",
      "cacheWrite",
      "output",
      "reasoning",
    ],
    InternalUsageTokenMetrics: [
      "totalWithCacheRead",
      "totalWithoutCacheRead",
      "knownTotalWithCacheRead",
      "knownTotalWithoutCacheRead",
    ],
    ServicesUsageCoverage: ["earliestRetainedAt", "indeterminateInvocations"],
  };
  for (const [name, fields] of Object.entries(nullableFields)) {
    const properties = schemas[prefix + name]?.properties;
    for (const field of fields) {
      if (properties?.[field]?.type === "integer")
        properties[field].nullable = true;
    }
  }
}

normalizeUsageNullability(schemas);
normalizeParameterOrder(document);
normalizeUnspecifiedEnumValues(document);

await writeFile(filePath, `${JSON.stringify(document, null, 2)}\n`);
