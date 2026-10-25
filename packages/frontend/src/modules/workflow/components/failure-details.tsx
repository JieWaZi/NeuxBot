import { useTranslation } from "react-i18next";
import { jsonText, record } from "../model/types";

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

/** 先展示稳定原因和处理办法，完整源码位置及发送证据保留在详情中。 */
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
      <details>
        <summary className="text-muted-foreground type-body cursor-pointer">
          {t("failureDetails")}
        </summary>
        <pre className="bg-muted type-body mt-3 overflow-auto rounded-lg p-4 whitespace-pre-wrap">
          {jsonText(value)}
        </pre>
      </details>
    </div>
  );
}
