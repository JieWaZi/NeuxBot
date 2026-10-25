import { i18n } from "@/shared/i18n";
import {
  automaticTaskApiZh,
  automaticTaskApiEn,
} from "@/shared/i18n/automatic-task-api";
import { useTranslation } from "react-i18next";
import {
  apiDocumentationSchema,
  apiFieldPath,
  schemaRecord,
} from "../model/workflow-inputs";
import type { TaskView } from "../model/types";
import { CopyValue } from "./copy-value";

i18n.addResourceBundle(
  "zh-CN",
  "automaticTask",
  automaticTaskApiZh,
  true,
  true,
);
i18n.addResourceBundle(
  "en-US",
  "automaticTask",
  automaticTaskApiEn,
  true,
  true,
);

export function WorkflowAPIContract({
  contract,
}: {
  contract: NonNullable<TaskView["workflowInputContract"]>;
}) {
  const { t } = useTranslation("automaticTask");
  const fields =
    contract.fields?.filter((field) => field.source === "api") ?? [];
  return (
    <section className="grid min-w-0 gap-3">
      <h3 className="type-label font-medium">{t("apiCallGuide")}</h3>
      <ol className="text-content-secondary type-body grid list-inside list-decimal gap-2">
        <li>{t(fields.length ? "apiPrepareInputs" : "apiPrepareNoInputs")}</li>
        <li>{t("apiSendRequest")}</li>
        <li>{t("apiReadRunState")}</li>
        <li>{t("apiReadResult")}</li>
      </ol>
      {fields.some((field) => field.type?.includes("file")) ? (
        <p className="text-content-secondary type-body">
          {t("apiUploadFileHelp")}
        </p>
      ) : null}
      <CopyValue
        label={t("requestBodyExample")}
        value={JSON.stringify({ args: contract.example ?? {} }, null, 2)}
      />
      <h3 className="type-label font-medium">{t("apiParameters")}</h3>
      {!fields.length ? (
        <p className="text-content-secondary type-body">
          {t("noAPIParameters")}
        </p>
      ) : (
        <dl className="grid gap-3">
          {fields.map((field) => (
            <div key={field.key} className="grid gap-1">
              <dt className="type-body flex flex-wrap items-center gap-2">
                <code className="type-code break-all">
                  {apiFieldPath(field.key ?? "")}
                </code>
                <span>{field.title}</span>
                <span className="text-content-secondary type-caption">
                  {field.type} ·{" "}
                  {t(
                    field.required
                      ? "apiParameterRequired"
                      : "apiParameterOptional",
                  )}
                </span>
              </dt>
              {field.description ? (
                <dd className="text-content-secondary type-body [overflow-wrap:anywhere]">
                  {field.description}
                </dd>
              ) : null}
            </div>
          ))}
        </dl>
      )}
      {contract.exampleValid === false ? (
        <p role="status" className="text-warning type-body">
          {t("exampleNeedsReplacement")}
        </p>
      ) : null}
      <details>
        <summary className="text-content-secondary type-ui cursor-pointer">
          {t("advancedInputSchema")}
        </summary>
        <div className="mt-2">
          <CopyValue
            label={t("inputSchema")}
            value={JSON.stringify(
              apiDocumentationSchema(schemaRecord(contract.schema)),
              null,
              2,
            )}
          />
        </div>
      </details>
    </section>
  );
}
