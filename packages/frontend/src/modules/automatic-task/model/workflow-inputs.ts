import type { TaskConfiguration } from "./types";

export type InputBindings = NonNullable<
  TaskConfiguration["workflowInputBindings"]
>;
export type InputBinding = InputBindings[string];

export function schemaRecord(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? Object.fromEntries(Object.entries(value))
    : {};
}

export function resolveInputSchema(
  schema: Record<string, unknown>,
  root = schema,
): Record<string, unknown> {
  if (schema === root && !Object.keys(schema).length)
    return { type: "object", properties: {} };
  let current = schema;
  const seen = new Set<string>();
  while (
    typeof current.$ref === "string" &&
    current.$ref.startsWith("#/") &&
    !seen.has(current.$ref)
  ) {
    seen.add(current.$ref);
    let target: unknown = root;
    for (const part of current.$ref.slice(2).split("/"))
      target =
        schemaRecord(target)[part.replaceAll("~1", "/").replaceAll("~0", "~")];
    const { $ref: reference, ...own } = current;
    void reference;
    current = { ...schemaRecord(target), ...own };
  }
  return current;
}

export function inputType(
  schema: Record<string, unknown>,
  root: Record<string, unknown>,
  depth = 0,
): string {
  if (depth > 16) return "json";
  const field = resolveInputSchema(schema, root);
  if (field.format === "file") return "file";
  if (Array.isArray(field.type))
    return field.type
      .map((kind) =>
        kind === "array"
          ? `array<${inputType(schemaRecord(field.items), root, depth + 1)}>`
          : kind,
      )
      .join(" | ");
  if (field.type === "array")
    return `array<${inputType(schemaRecord(field.items), root, depth + 1)}>`;
  return typeof field.type === "string" ? field.type : "json";
}

export function completeInputBindings(
  schema: Record<string, unknown>,
  bindings: InputBindings,
): InputBindings {
  const object = resolveInputSchema(schema);
  const required = Array.isArray(object.required) ? object.required : [];
  return Object.fromEntries(
    Object.entries(schemaRecord(object.properties)).map(([key, value]) => {
      const property = resolveInputSchema(schemaRecord(value), schema);
      return [
        key,
        bindings[key] ?? {
          source:
            required.includes(key) && !("default" in property)
              ? "fixed"
              : "workflow_default",
        },
      ];
    }),
  );
}

/** 复制请求示例时保留 JSON 的实际类型，并安全处理 Shell 单引号。 */
export function shellQuote(value: string) {
  return `'${value.replaceAll("'", "'\\''")}'`;
}

/** 根级分支无法确定唯一来源集合，显式拒绝逐字段配置。 */
export function supportsInputBindings(schema: Record<string, unknown>) {
  const root = resolveInputSchema(schema);
  return root.type === "object" && !root.oneOf && !root.anyOf && !root.allOf;
}

/** 特殊字符字段显示为实际 JSON key，避免被误解为嵌套字段。 */
export function apiFieldPath(key: string) {
  return /^[A-Za-z_$][A-Za-z0-9_$]*$/.test(key)
    ? `args.${key}`
    : `args[${JSON.stringify(key)}]`;
}

/** API 文档只移除 Schema 节点上的界面注解，保留业务字段名和默认值数据。 */
export function apiDocumentationSchema(
  schema: Record<string, unknown>,
): Record<string, unknown> {
  const document: Record<string, unknown> = {};
  const childSchema = (value: unknown): unknown =>
    typeof value === "boolean"
      ? value
      : apiDocumentationSchema(schemaRecord(value));
  for (const [key, value] of Object.entries(schema)) {
    if (key.startsWith("x-ui-")) continue;
    switch (key) {
      case "properties":
      case "patternProperties":
      case "$defs":
      case "definitions":
      case "dependentSchemas":
        document[key] = Object.fromEntries(
          Object.entries(schemaRecord(value)).map(([name, child]) => [
            name,
            childSchema(child),
          ]),
        );
        break;
      case "allOf":
      case "anyOf":
      case "oneOf":
      case "prefixItems":
        document[key] = Array.isArray(value) ? value.map(childSchema) : value;
        break;
      case "items":
        document[key] = Array.isArray(value)
          ? value.map(childSchema)
          : childSchema(value);
        break;
      case "additionalProperties":
      case "unevaluatedProperties":
      case "additionalItems":
      case "unevaluatedItems":
      case "contains":
      case "propertyNames":
      case "not":
      case "if":
      case "then":
      case "else":
      case "contentSchema":
        document[key] = childSchema(value);
        break;
      default:
        document[key] = value;
    }
  }
  return document;
}
