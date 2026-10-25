import { ArrowRight, LogIn } from "lucide-react";
import type { ComponentProps } from "react";
import { useTranslation } from "react-i18next";
import { Card } from "@/shared/ui/card";
import { jsonText, record, type Manifest } from "../model/types";

/** 参数顺序来自冻结版本；运行视图只展示本次实际提交的字段。 */
export function workflowInputFields(manifest: Manifest, args?: unknown) {
  const schema = manifest.meta?.inputSchema;
  const properties = record(schema?.properties) ?? {};
  const values = args === undefined ? undefined : (record(args) ?? {});
  const keys = values ? Object.keys(values) : Object.keys(properties);
  const order = manifest.inputPropertyOrder?.[""] ?? [];
  return [...new Set([...order, ...keys])]
    .filter((key) => keys.includes(key))
    .map((key) => {
      const field = record(properties[key]);
      const title = typeof field?.title === "string" ? field.title.trim() : "";
      const description =
        typeof field?.description === "string" ? field.description.trim() : "";
      return {
        key,
        label: title || description || key,
        description,
        required:
          Array.isArray(schema?.required) && schema.required.includes(key),
        value: values?.[key],
      };
    });
}

export function WorkflowInputNode({
  manifest,
  args,
  open = false,
  ...buttonProps
}: {
  manifest: Manifest;
  args?: unknown;
  open?: boolean;
} & ComponentProps<"button">) {
  const { t } = useTranslation("workflow");
  const actual = args !== undefined;
  const fields = workflowInputFields(manifest, args);
  return (
    <Card asChild variant="resource" data-workflow-input-node>
      <button
        {...buttonProps}
        data-state={open ? "open" : "closed"}
        type="button"
        aria-label={t("inputNodeTitle")}
        aria-haspopup="dialog"
        aria-expanded={open}
        className="focus-visible:ring-ring data-[state=open]:bg-accent-surface grid w-full min-w-0 gap-4 p-4 text-left outline-none focus-visible:ring-2"
      >
        <span className="flex items-center gap-2">
          <LogIn
            className="text-content-secondary size-icon-md shrink-0"
            aria-hidden="true"
            strokeWidth={1.5}
          />
          <span className="type-label font-medium">{t("inputNodeTitle")}</span>
        </span>
        {fields.length ? (
          <span className="grid min-w-0 gap-3">
            {fields.slice(0, 3).map((field) => (
              <span key={field.key} className="grid min-w-0 gap-1">
                <span className="type-ui flex items-start justify-between gap-2">
                  <span className="min-w-0 font-medium break-words">
                    {field.label}
                  </span>
                  {!actual ? (
                    <span className="text-content-secondary shrink-0">
                      {t(field.required ? "fieldRequired" : "fieldOptional")}
                    </span>
                  ) : null}
                </span>
                {actual ? (
                  <span className="text-content-secondary type-body line-clamp-2 break-words whitespace-pre-wrap">
                    {(typeof field.value === "string"
                      ? field.value
                      : jsonText(field.value)
                    ).slice(0, 160)}
                  </span>
                ) : field.description && field.description !== field.label ? (
                  <span className="text-content-secondary type-ui line-clamp-2 break-words">
                    {field.description}
                  </span>
                ) : null}
              </span>
            ))}
            {fields.length > 3 ? (
              <span className="text-content-secondary type-caption">
                {t("moreInputFields", { count: fields.length - 3 })}
              </span>
            ) : null}
          </span>
        ) : (
          <span className="text-content-secondary type-body">
            {t("noInputs")}
          </span>
        )}
        <span className="text-content-secondary type-ui flex items-center justify-between gap-2">
          {t(actual ? "viewRunInputs" : "fillRunInputs")}
          <ArrowRight
            className="size-icon-sm shrink-0"
            aria-hidden="true"
            strokeWidth={1.5}
          />
        </span>
      </button>
    </Card>
  );
}
