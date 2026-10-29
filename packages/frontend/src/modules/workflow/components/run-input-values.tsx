import { useTranslation } from "react-i18next";
import { record } from "../model/types";
import { RunJsonValue } from "./run-json-value";

export function RunInputValues({ args }: { args: unknown }) {
  const { t } = useTranslation("workflow");
  const values = record(args) ?? {};
  return (
    <div className="grid min-h-0 gap-5 overflow-y-auto overscroll-contain px-6 pb-5">
      <p className="text-content-secondary type-body">{t("runInputsHelp")}</p>
      {Object.keys(values).length ? (
        <RunJsonValue value={values} />
      ) : (
        <p className="text-content-secondary type-body">{t("noInputs")}</p>
      )}
    </div>
  );
}
