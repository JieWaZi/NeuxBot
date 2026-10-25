import { i18n } from "@/shared/i18n";

export class ApiError extends Error {
  readonly code: string;
  readonly status?: number;
  /** 已报告的提示身份，让操作入口与 API 拦截器共用一条错误。 */
  noticeId?: string;
  /** 请求的稳定操作身份，局部消费的读取失败也沿用原请求恢复。 */
  requestOperation?: string;

  constructor(message: string, code = "UNKNOWN", status?: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export const serviceConnectionMessageKey = "common:serviceConnectionFailed";

/** 识别需要按当前语言展示的本地服务连接错误。 */
export function apiErrorMessageKey(message: string) {
  const lowLevelMessage = message.trim().toLowerCase();
  if (
    lowLevelMessage === "failed to fetch" ||
    lowLevelMessage === "fetch failed" ||
    lowLevelMessage === "load failed" ||
    lowLevelMessage === "networkerror when attempting to fetch resource." ||
    message === i18n.t(serviceConnectionMessageKey) ||
    message === i18n.getFixedT("zh-CN")(serviceConnectionMessageKey)
  ) {
    return serviceConnectionMessageKey;
  }
  return undefined;
}

/** 将浏览器的底层网络异常转换为可展示的本地服务提示。 */
export function userFacingApiErrorMessage(message: string): string {
  const messageKey = apiErrorMessageKey(message);
  if (messageKey) return i18n.t(messageKey);
  return message;
}

export function toApiError(error: unknown, status?: number): ApiError {
  if (error instanceof ApiError) {
    return error;
  }
  if (error instanceof Error) {
    return new ApiError(
      userFacingApiErrorMessage(error.message),
      "NETWORK",
      status,
    );
  }
  if (isRecord(error)) {
    const message =
      typeof error.message === "string"
        ? error.message
        : i18n.t("common:serviceUnrecognizedError");
    const codeValue = error.key ?? error.code;
    const code =
      typeof codeValue === "string" || typeof codeValue === "number"
        ? String(codeValue)
        : "UNKNOWN";
    return new ApiError(userFacingApiErrorMessage(message), code, status);
  }
  return new ApiError(i18n.t("common:serviceOffline"), "NETWORK", status);
}
