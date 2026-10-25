import type { UseMutationOptions } from "@tanstack/react-query";

import { ApiError } from "./errors";

/** 仅客户端识别；请求发出前会从 HTTP 头中移除。 */
export const ALLY_LOCAL_ERROR_SURFACE = "X-NeuxAlly-Error-Surface";

type Requestish = {
  headers?: RequestInit["headers"] | Record<string, unknown>;
  [key: string]: unknown;
};

function readHeader(
  headers: Requestish["headers"],
  name: string,
): string | undefined {
  if (!headers) return undefined;
  if (headers instanceof Headers) return headers.get(name) ?? undefined;
  if (Array.isArray(headers)) {
    const entry = headers.find(
      ([key]) => key.toLowerCase() === name.toLowerCase(),
    );
    return entry?.[1];
  }
  if (typeof headers === "object") {
    for (const [key, value] of Object.entries(headers)) {
      if (key.toLowerCase() !== name.toLowerCase()) continue;
      if (typeof value === "string") return value;
    }
  }
  return undefined;
}

function mergeLocalSurfaceHeader(
  headers: Requestish["headers"],
): Record<string, string> {
  const next: Record<string, string> = {};
  if (headers instanceof Headers) {
    headers.forEach((value, key) => {
      next[key] = value;
    });
  } else if (Array.isArray(headers)) {
    for (const [key, value] of headers) {
      next[key] = value;
    }
  } else if (headers && typeof headers === "object") {
    for (const [key, value] of Object.entries(headers)) {
      if (typeof value === "string") next[key] = value;
    }
  }
  next[ALLY_LOCAL_ERROR_SURFACE] = "local";
  return next;
}

/** 标记错误由调用方就地展示，API 拦截器不再生成全局通知。 */
export function withLocalErrorSurface<T extends object>(options: T): T {
  const base = options as T & Requestish;
  return {
    ...base,
    headers: mergeLocalSurfaceHeader(base.headers),
  };
}

export function hasLocalErrorSurface(
  options?: unknown,
  request?: Request,
): boolean {
  if (request?.headers.get(ALLY_LOCAL_ERROR_SURFACE) === "local") return true;
  if (!options || typeof options !== "object") return false;
  return (
    readHeader((options as Requestish).headers, ALLY_LOCAL_ERROR_SURFACE) ===
    "local"
  );
}

/** 就地展示时优先使用 ApiError 文案，否则回退到领域 fallback。 */
export function localApiErrorMessage(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback;
}

/** 包装生成 Mutation，使每次 mutate 自动带上就地错误标记。 */
export function withLocalErrorMutation<TData, TError, TVariables>(
  options: UseMutationOptions<TData, TError, TVariables>,
): UseMutationOptions<TData, TError, TVariables> {
  const { mutationFn } = options;
  if (!mutationFn) return options;
  return {
    ...options,
    mutationFn: (variables, context) =>
      mutationFn(
        withLocalErrorSurface(variables as object) as TVariables,
        context,
      ),
  };
}
