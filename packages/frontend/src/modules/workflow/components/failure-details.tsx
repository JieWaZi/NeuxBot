import { applicationErrorMessage } from "@/shared/api/error-presentation";
import { useTranslation } from "react-i18next";
import { record } from "../model/types";

/** 先展示稳定原因和处理办法，不将内部协议封套直接呈现给用户。 */
export function WorkflowFailure({ value }: { value: unknown }) {
  const { t } = useTranslation("workflow");
  const failure = record(value) ?? {};
  const cause = record(failure.cause) ?? {};
  const delivery = record(failure.delivery ?? failure.data) ?? {};
  const code =
    failure.reason ?? cause.code ?? delivery.errorCode ?? failure.code;
  const owner =
    typeof code === "string" && cause.code === code
      ? cause
      : typeof code === "string" && delivery.errorCode === code
        ? delivery
        : failure;
  const detail = owner.message ?? owner.errorMessage;
  const source = owner.messageSource ?? owner.errorMessageSource;
  const message = applicationErrorMessage({
    code: typeof code === "string" ? code : undefined,
    params: record(owner.params ?? owner.errorParams) ?? undefined,
    message: typeof detail === "string" ? detail : undefined,
    messageSource: source === "public" ? "public" : "localized",
    fallback: t("failureCauses.generic"),
  });
  return (
    <div className="grid min-w-0 gap-3">
      <p role="alert" className="text-destructive type-body wrap-anywhere">
        {failure.code === "EFFECT_OUTCOME_UNKNOWN"
          ? t("uncertainFailureHelp")
          : message}
      </p>
      {delivery.state === "unknown" ? (
        <p className="type-body">{t("failureDeliveryUnknown")}</p>
      ) : delivery.state === "sent" ? (
        <p className="type-body">{t("failureDeliverySent")}</p>
      ) : delivery.deliveryId ? (
        <p className="type-body">
          {t("failureDeliveryProgress", {
            count:
              typeof delivery.partsSent === "number" ? delivery.partsSent : 0,
          })}
        </p>
      ) : null}
    </div>
  );
}
