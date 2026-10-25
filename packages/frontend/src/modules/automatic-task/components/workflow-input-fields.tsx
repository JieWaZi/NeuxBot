import { useId, useRef } from "react";
import { useTranslation } from "react-i18next";
import { SchemaForm } from "@/shared/json-schema/schema-form";
import { FieldLabel } from "@/shared/ui/field-label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import {
  apiFieldPath,
  completeInputBindings,
  inputType,
  resolveInputSchema,
  schemaRecord,
  type InputBindings,
} from "../model/workflow-inputs";

export function WorkflowInputFields({
  workflowId,
  schema,
  bindings,
  allowAPI,
  onChange,
  fieldOrder,
  onValidityChange,
}: {
  workflowId?: string;
  schema: Record<string, unknown>;
  bindings: InputBindings;
  allowAPI: boolean;
  onChange: (value: InputBindings) => void;
  fieldOrder?: Record<string, string[]>;
  onValidityChange?: (path: string, valid: boolean) => void;
}) {
  const { t } = useTranslation("automaticTask");
  const id = useId();
  const fixedDrafts = useRef<Record<string, { value?: unknown }>>({});
  const object = resolveInputSchema(schema);
  const properties = schemaRecord(object.properties);
  const required = Array.isArray(object.required) ? object.required : [];
  const values = completeInputBindings(schema, bindings);
  const keys = [
    ...new Set([
      ...(
        fieldOrder?.[
          typeof schema.$ref === "string" && schema.$ref.startsWith("#/")
            ? schema.$ref.slice(1)
            : ""
        ] ?? []
      ).filter((key) => key in properties),
      ...Object.keys(properties),
    ]),
  ];
  if (object.type !== "object" || object.oneOf || object.anyOf || object.allOf)
    return (
      <p role="alert" className="text-destructive type-body">
        {t("inputSchemaUnsupported")}
      </p>
    );
  return (
    <section className="grid min-w-0 gap-5">
      <h2 className="type-label font-medium">{t("workflowParameters")}</h2>
      {!keys.length ? (
        <p className="text-content-secondary type-body">
          {t("noWorkflowParameters")}
        </p>
      ) : null}
      {keys.map((key) => {
        const property = schemaRecord(properties[key]);
        const presentation = resolveInputSchema(property, schema);
        const binding = values[key];
        if (!binding) return null;
        const name =
          typeof presentation.title === "string" ? presentation.title : key;
        const canDefault = !required.includes(key) || "default" in presentation;
        return (
          <div key={key} className="grid min-w-0 gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <FieldLabel
                htmlFor={`${id}-${key}`}
                required={required.includes(key)}
              >
                {name}
              </FieldLabel>
              <code className="text-content-secondary type-ui break-all">
                {key} · {inputType(property, schema)}
              </code>
            </div>
            {typeof presentation.description === "string" ? (
              <p className="text-content-secondary type-caption">
                {presentation.description}
              </p>
            ) : null}
            <Select
              value={binding.source}
              onValueChange={(source) => {
                if (binding.source === "fixed")
                  fixedDrafts.current[key] =
                    "value" in binding ? { value: binding.value } : {};
                if (
                  source === "fixed" ||
                  source === "api" ||
                  source === "workflow_default"
                )
                  onChange({
                    ...values,
                    [key]:
                      source === "fixed"
                        ? {
                            source,
                            ...(fixedDrafts.current[key] ??
                              ("default" in presentation
                                ? { value: presentation.default }
                                : {})),
                          }
                        : { source },
                  });
              }}
            >
              <SelectTrigger
                id={`${id}-${key}`}
                aria-label={t("parameterSourceFor", { name })}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="fixed">{t("inputFixed")}</SelectItem>
                {allowAPI || binding.source === "api" ? (
                  <SelectItem
                    value="api"
                    disabled={
                      !allowAPI || inputType(property, schema) === "json"
                    }
                  >
                    {t("inputAPI")}
                  </SelectItem>
                ) : null}
                <SelectItem value="workflow_default" disabled={!canDefault}>
                  {t(
                    "default" in presentation ? "inputDefault" : "inputOmitted",
                  )}
                </SelectItem>
              </SelectContent>
            </Select>
            {binding.source === "fixed" ? (
              <SchemaForm
                fileWorkflowId={workflowId}
                compact
                schema={{
                  type: "object",
                  properties: {
                    [key]: {
                      ...property,
                      title: t("fixedValueFor", { name }),
                      description: "",
                    },
                  },
                  required: [key],
                  ...(schema.$defs ? { $defs: schema.$defs } : {}),
                }}
                rootSchema={schema}
                fieldOrder={fieldOrder}
                schemaPath={
                  typeof schema.$ref === "string" &&
                  schema.$ref.startsWith("#/")
                    ? schema.$ref.slice(1)
                    : ""
                }
                value={"value" in binding ? { [key]: binding.value } : {}}
                onChange={(next) => {
                  const object = schemaRecord(next);
                  onChange({
                    ...values,
                    [key]:
                      key in object
                        ? { source: "fixed", value: object[key] }
                        : { source: "fixed" },
                  });
                }}
                path={`bindings/${key}`}
                onValidityChange={onValidityChange}
              />
            ) : (
              <p
                className={
                  binding.source === "api" && !allowAPI
                    ? "text-destructive type-caption"
                    : "text-content-secondary type-caption"
                }
              >
                {binding.source === "api"
                  ? t(
                      allowAPI
                        ? required.includes(key)
                          ? "apiInputRequired"
                          : "apiInputOptional"
                        : "scheduledAPIInput",
                      { key, path: apiFieldPath(key) },
                    )
                  : "default" in presentation
                    ? t("workflowDefaultValue", {
                        value: JSON.stringify(presentation.default),
                      })
                    : t("workflowOmittedValue")}
              </p>
            )}
          </div>
        );
      })}
    </section>
  );
}
