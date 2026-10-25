import { useTranslation } from "react-i18next";
import { resolveInputSchema, schemaRecord } from "../model/workflow-inputs";
import type { TaskView } from "../model/types";

/** 详情与手动运行共用只读参数来源，避免把省略参数展示为默认值。 */
export function WorkflowInputSummary({
  task,
  configuredOnly = false,
}: {
  task: TaskView;
  configuredOnly?: boolean;
}) {
  const { t } = useTranslation("automaticTask");
  const fields = task.workflowInputContract?.fields?.filter(
    (field) => !configuredOnly || field.source !== "api",
  );
  if (!fields?.length) return null;
  return (
    <dl className="grid gap-3">
      {fields.map((field) => {
        if (field.key === undefined) return null;
        const schema = resolveInputSchema(schemaRecord(field.schema));
        return (
          <div key={field.key} className="grid gap-1">
            <dt className="type-body">
              {field.title}{" "}
              <code className="text-content-secondary type-caption">
                {field.key} · {field.type}
              </code>
            </dt>
            <dd className="text-content-secondary type-body break-words">
              {field.source === "fixed"
                ? `${t("inputFixed")}：${JSON.stringify(task.workflowInputBindings?.[field.key]?.value)}`
                : field.source === "api"
                  ? t("inputAPI")
                  : "default" in schema
                    ? t("workflowDefaultValue", {
                        value: JSON.stringify(schema.default),
                      })
                    : t("workflowOmittedValue")}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}
