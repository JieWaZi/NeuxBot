import { ApiError } from "@/shared/api/errors";

export type ChatSendFailureKind =
  | "active-execution"
  | "create-request-in-progress"
  | "create-request-conflict"
  | "session-not-found"
  | "rejected"
  | "unknown";

export type ChatSendFailure =
  { kind: "none" } | { kind: ChatSendFailureKind; message?: string };

export const NO_SEND_FAILURE: ChatSendFailure = { kind: "none" };

export function isSendFailureActive(
  failure: ChatSendFailure,
): failure is { kind: ChatSendFailureKind; message?: string } {
  return failure.kind !== "none";
}

export function sendFailureEquals(
  left: ChatSendFailure,
  right: ChatSendFailure,
): boolean {
  if (left.kind !== right.kind) {
    return false;
  }
  if (left.kind === "none") {
    return true;
  }
  if (right.kind === "none") {
    return false;
  }
  return (left.message ?? "") === (right.message ?? "");
}

function classifyChatSendFailureKind(error: unknown): ChatSendFailureKind {
  if (!(error instanceof ApiError)) return "unknown";
  if (
    error.code === "RuntimeSessionNotFound" ||
    isMissingRuntimeSessionMessage(error.message)
  ) {
    return "session-not-found";
  }
  if (error.code === "ChatHasActiveExecution") {
    return "active-execution";
  }
  if (error.code === "CreateRequestInProgress") {
    return "create-request-in-progress";
  }
  if (error.code === "CreateRequestConflict") {
    return "create-request-conflict";
  }
  if (
    error.code === "RuntimeUnavailable" ||
    error.code === "SkillUnavailable"
  ) {
    return "rejected";
  }
  if (
    error.code !== "NETWORK" &&
    error.status !== undefined &&
    error.status >= 400 &&
    error.status < 500 &&
    error.status !== 408
  ) {
    return "rejected";
  }
  return "unknown";
}

/** 识别 Runtime 原始诊断中的原生会话缺失，供错误展示与恢复判断。 */
export function isMissingRuntimeSessionMessage(message: string): boolean {
  return message.toLowerCase().includes("no rollout found for thread id");
}

export function classifyChatSendFailure(error: unknown): ChatSendFailure {
  const kind = classifyChatSendFailureKind(error);
  if (!(error instanceof ApiError)) {
    return { kind };
  }
  if (kind === "session-not-found") {
    return { kind };
  }
  if (error.code === "NETWORK") {
    return { kind: "unknown" };
  }
  const message = error.message.trim();
  if (!message) {
    return { kind };
  }
  return { kind, message };
}

type SendFailureTranslator = (
  key:
    | "sendUnknown"
    | "sendActiveExecution"
    | "sendCreateRequestInProgress"
    | "sendCreateRequestConflict"
    | "runtimeFailure.sessionNotFound"
    | "sendFailed",
) => string;

export function sendFailureDisplayText(
  failure: ChatSendFailure,
  translate: SendFailureTranslator,
): string {
  if (!isSendFailureActive(failure)) {
    return "";
  }
  if (failure.message) {
    return failure.message;
  }
  if (failure.kind === "session-not-found") {
    return translate("runtimeFailure.sessionNotFound");
  }
  switch (failure.kind) {
    case "unknown":
      return translate("sendUnknown");
    case "active-execution":
      return translate("sendActiveExecution");
    case "create-request-in-progress":
      return translate("sendCreateRequestInProgress");
    case "create-request-conflict":
      return translate("sendCreateRequestConflict");
    default:
      return translate("sendFailed");
  }
}
