import { schemaDefaults } from "@/shared/json-schema/schema-form";
import { jsonText, record } from "./types";

const storageKey = (id: string) => `ally.workflow.last-run-inputs.${id}`;

export function lastRunInputs(id: string, schema: Record<string, unknown>) {
  try {
    const saved = record(
      JSON.parse(localStorage.getItem(storageKey(id)) ?? "null"),
    );
    if (saved?.schema === jsonText(schema) && "args" in saved)
      return saved.args;
  } catch {
    // 存储不可用或内容损坏时，表单仍可使用当前契约的默认值。
  }
  return schemaDefaults(schema) ?? {};
}

export function rememberRunInputs(
  id: string,
  schema: Record<string, unknown>,
  args: unknown,
) {
  try {
    localStorage.setItem(
      storageKey(id),
      JSON.stringify({ schema: jsonText(schema), args }),
    );
  } catch {
    // 本地偏好写入失败不影响已经接纳的运行。
  }
}
