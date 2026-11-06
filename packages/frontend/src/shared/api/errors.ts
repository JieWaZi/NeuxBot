import {
  applicationErrorMessage,
  type ErrorParameters,
  type ErrorMessageSource,
} from "./error-presentation";
import { i18n } from "@/shared/i18n";

/** 构造器只接收调用方明确编写的可读本地提示；传输载荷必须经过 toApiError。 */
export class ApiError extends Error {
  readonly code: string;
  readonly status?: number;
  readonly params?: ErrorParameters;
  readonly messageSource?: ErrorMessageSource;
  /** 已报告的提示身份，让操作入口与 API 拦截器共用一条错误。 */
  noticeId?: string;
  /** 请求的稳定操作身份，局部消费的读取失败也沿用原请求恢复。 */
  requestOperation?: string;

  constructor(
    message: string,
    code = "UNKNOWN",
    status?: number,
    presentation?: {
      params?: ErrorParameters;
      messageSource?: ErrorMessageSource;
    },
  ) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
    this.params = presentation?.params;
    this.messageSource = presentation?.messageSource;
    if (presentation)
      Object.defineProperty(this, "message", {
        configurable: true,
        get: () => applicationErrorMessage({ code, message, ...presentation }),
      });
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
    const messageKey = apiErrorMessageKey(error.message);
    const converted = new ApiError(
      messageKey
        ? i18n.t(messageKey)
        : i18n.t("common:serviceUnrecognizedError"),
      "NETWORK",
      status,
    );
    converted.cause = error;
    return converted;
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
    return new ApiError(userFacingApiErrorMessage(message), code, status, {
      params: isRecord(error.params) ? error.params : undefined,
      messageSource: error.messageSource === "public" ? "public" : "localized",
    });
  }
  return new ApiError(i18n.t("common:serviceOffline"), "NETWORK", status);
}
