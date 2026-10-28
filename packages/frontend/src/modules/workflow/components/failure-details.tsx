import { useTranslation } from "react-i18next";
import { record } from "../model/types";

const reasons = [
  {
    code: "WorkflowIMTargetNotDeclared",
    key: "failureCauses.undeclaredTarget",
  },
  { code: "IMTargetUnavailable", key: "failureCauses.targetUnavailable" },
  { code: "IMResourceNotFound", key: "failureCauses.targetUnavailable" },
  {
    code: "IMWorkflowDeliveryNotAllowed",
    key: "failureCauses.deliveryNotAllowed",
  },
  { code: "IMTargetNotAuthorized", key: "failureCauses.chatNotAuthorized" },
  { code: "IMChatNotAuthorized", key: "failureCauses.chatNotAuthorized" },
  { code: "IMConnectionDisabled", key: "failureCauses.connectionDisabled" },
  { code: "IMLoginRequired", key: "failureCauses.loginRequired" },
  {
    code: "IMDeliveryContextUnavailable",
    key: "failureCauses.contextUnavailable",
  },
  { code: "IMPlatformUnavailable", key: "failureCauses.platformUnavailable" },
  { code: "IMProtocolInvalid", key: "failureCauses.protocolInvalid" },
  { code: "IMFileInvalid", key: "failureCauses.fileInvalid" },
] as const;

/** 先展示稳定原因和处理办法，不将内部协议封套直接呈现给用户。 */
export function WorkflowFailure({ value }: { value: unknown }) {
  const { t } = useTranslation("workflow");
  const failure = record(value) ?? {};
  const cause = record(failure.cause) ?? {};
  const delivery = record(failure.delivery ?? failure.data) ?? {};
  const code =
    failure.reason ?? cause.code ?? delivery.errorCode ?? failure.code;
  const reason = reasons.find((item) => item.code === code);
  return (
    <div className="grid min-w-0 gap-3">
      <p role="alert" className="text-destructive type-body wrap-anywhere">
        {t(
          failure.code === "EFFECT_OUTCOME_UNKNOWN"
            ? "uncertainFailureHelp"
            : (reason?.key ?? "failureCauses.generic"),
        )}
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
      {typeof failure.message === "string" ? (
        <p className="type-body whitespace-pre-wrap">{failure.message}</p>
      ) : typeof cause.message === "string" ? (
        <p className="type-body whitespace-pre-wrap">{cause.message}</p>
      ) : null}
    </div>
  );
}
