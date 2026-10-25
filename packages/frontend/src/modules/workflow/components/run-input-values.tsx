import { useTranslation } from "react-i18next";
import { jsonText, type Manifest } from "../model/types";
import { workflowInputFields } from "./workflow-input-node";

export function RunInputValues({
  manifest,
  args,
}: {
  manifest: Manifest;
  args: unknown;
}) {
  const { t } = useTranslation("workflow");
  const fields = workflowInputFields(manifest, args ?? {});
  return (
    <div className="grid min-h-0 gap-5 overflow-y-auto overscroll-contain px-6 pb-5">
      <p className="text-content-secondary type-body">{t("runInputsHelp")}</p>
      <dl className="grid gap-5">
        {fields.map((field) => (
          <div key={field.key} className="grid min-w-0 gap-2">
            <dt className="type-ui font-medium break-words">{field.label}</dt>
            {field.description && field.description !== field.label ? (
              <dd className="text-content-secondary type-ui break-words">
                {field.description}
              </dd>
            ) : null}
            <dd className="type-body min-w-0 break-words whitespace-pre-wrap">
              {typeof field.value === "string" ? (
                field.value
              ) : (
                <pre className="type-body whitespace-pre-wrap">
                  {jsonText(field.value)}
                </pre>
              )}
            </dd>
          </div>
        ))}
        {!fields.length ? (
          <div className="text-content-secondary type-body">
            {t("noInputs")}
          </div>
        ) : null}
      </dl>
    </div>
  );
}
