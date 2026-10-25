import { i18n } from "@/shared/i18n";

export type RuntimeFailurePresentation = { message: string };

function runtimeFailureMessage(errorCode?: string): string {
  switch (errorCode) {
    case "NETWORK_ERROR":
      return i18n.t("runtimeFailure.networkError", { ns: "chat" });
    case "REQUEST_TIMEOUT":
      return i18n.t("runtimeFailure.requestTimeout", { ns: "chat" });
    case "CONTEXT_TOO_LARGE":
      return i18n.t("runtimeFailure.contextTooLarge", { ns: "chat" });
    case "AUTH_REQUIRED":
      return i18n.t("runtimeFailure.authRequired", { ns: "chat" });
    case "MODEL_UNAVAILABLE":
      return i18n.t("runtimeFailure.modelUnavailable", { ns: "chat" });
    case "PLAN_UPGRADE_REQUIRED":
      return i18n.t("runtimeFailure.planUpgradeRequired", { ns: "chat" });
    case "QUOTA_EXHAUSTED":
      return i18n.t("runtimeFailure.quotaExhausted", { ns: "chat" });
    case "RATE_LIMITED":
      return i18n.t("runtimeFailure.rateLimited", { ns: "chat" });
    case "SESSION_NOT_FOUND":
      return i18n.t("runtimeFailure.sessionNotFound", { ns: "chat" });
    case "UPSTREAM_UNAVAILABLE":
      return i18n.t("runtimeFailure.upstreamUnavailable", { ns: "chat" });
    case "EVENT_DELIVERY_FAILED":
      return i18n.t("runtimeFailure.eventDeliveryFailed", { ns: "chat" });
    case "FRAME_TOO_LARGE":
      return i18n.t("runtimeFailure.frameTooLarge", { ns: "chat" });
    case "INACTIVITY_TIMEOUT":
      return i18n.t("runtimeFailure.inactivityTimeout", { ns: "chat" });
    case "POLICY_UNSUPPORTED":
      return i18n.t("runtimeFailure.policyUnsupported", { ns: "chat" });
    case "PROCESS_EXITED":
      return i18n.t("runtimeFailure.processExited", { ns: "chat" });
    case "PROTOCOL_INCOMPATIBLE":
      return i18n.t("runtimeFailure.protocolIncompatible", { ns: "chat" });
    case "RUNTIME_INCOMPATIBLE":
      return i18n.t("runtimeFailure.runtimeIncompatible", { ns: "chat" });
    case "RUNTIME_NOT_FOUND":
      return i18n.t("runtimeFailure.runtimeNotFound", { ns: "chat" });
    case "RUNTIME_UNAVAILABLE":
    case "SPAWN_FAILED":
      return i18n.t("runtimeFailure.runtimeUnavailable", { ns: "chat" });
    case "UNSUPPORTED_CAPABILITY":
      return i18n.t("runtimeFailure.unsupportedCapability", { ns: "chat" });
    default:
      return i18n.t("runtimeFailure.unknown", { ns: "chat" });
  }
}

export function runtimeFailurePresentation(
  errorCode?: string,
): RuntimeFailurePresentation {
  return {
    message: runtimeFailureMessage(errorCode),
  };
}
