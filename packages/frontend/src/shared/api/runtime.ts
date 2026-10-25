import {
  apiErrorScope,
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
  reportErrorNotice,
} from "./error-notices";
import { getCoreConnection, type CoreConnection } from "@/shared/platform";

import { client } from "./generated/client.gen";
import { toApiError } from "./errors";
import {
  ALLY_LOCAL_ERROR_SURFACE,
  hasLocalErrorSurface,
} from "./local-error-surface";

let connection: CoreConnection | undefined;
let interceptorInstalled = false;
const trustedOrigins = new Set<string>();
const recoveries = new WeakMap<
  Request,
  ReturnType<typeof captureErrorNoticeRecovery>
>();

/** 按方法、资源和查询参数区分操作，分页或其他请求成功不能清除当前失败。 */
function requestOperation(request: Request) {
  const url = new URL(request.url);
  const remoteKnowledge = url.pathname.match(
    /^\/api\/v1\/knowledge\/connections\/([^/]+)\/libraries\/([^/]+)(?:\/|$)/,
  );
  if (remoteKnowledge?.[1] && remoteKnowledge[2]) {
    return `api:knowledge:${remoteKnowledge[1]}:${remoteKnowledge[2]}`;
  }
  url.searchParams.sort();
  return `api:${request.method}:${url.pathname}${url.search}`;
}

export async function initializeApi(): Promise<CoreConnection> {
  connection = await getCoreConnection();
  trustedOrigins.add(new URL(connection.baseUrl).origin);
  client.setConfig({
    baseUrl: connection.baseUrl,
    credentials: connection.credentials ?? "omit",
    headers: connection.token
      ? { Authorization: `Bearer ${connection.token}` }
      : {},
  });
  if (!interceptorInstalled) {
    client.interceptors.request.use((request) => {
      const headers = new Headers(request.headers);
      headers.delete(ALLY_LOCAL_ERROR_SURFACE);
      const original = new URL(request.url);
      if (
        connection &&
        trustedOrigins.has(original.origin) &&
        (original.pathname === "/api/v1" ||
          original.pathname.startsWith("/api/v1/"))
      ) {
        // 生成 QueryKey 会冻结旧 baseUrl/headers；实际 Core 请求必须沿用当前受信连接。
        const target = new URL(connection.baseUrl);
        target.pathname = original.pathname;
        target.search = original.search;
        if (connection.token)
          headers.set("Authorization", `Bearer ${connection.token}`);
        else headers.delete("Authorization");
        if (
          connection.csrfToken &&
          !["GET", "HEAD", "OPTIONS"].includes(request.method)
        )
          headers.set("X-Ally-CSRF", connection.csrfToken);
        else headers.delete("X-Ally-CSRF");
        if (target.toString() !== request.url) {
          // 显式保留 Request 属性；跨运行环境不能把 Request 当普通 init 字典传递。
          const init = {
            method: request.method,
            body: request.body,
            headers,
            signal: request.signal,
            cache: request.cache,
            credentials: connection.credentials ?? "omit",
            integrity: request.integrity,
            keepalive: request.keepalive,
            mode: request.mode,
            redirect: request.redirect,
            referrer: request.referrer,
            referrerPolicy: request.referrerPolicy,
            // Node 的流式请求需要 half；浏览器支持时沿用相同的受管请求语义。
            duplex: "half",
          };
          return new Request(target, init);
        }
      }
      if (
        connection &&
        original.origin !== new URL(connection.baseUrl).origin
      ) {
        headers.delete("Authorization");
        headers.delete("X-Ally-CSRF");
      }
      return new Request(request, {
        headers,
        credentials: connection?.credentials ?? "omit",
      });
    });
    client.interceptors.request.use((request) => {
      recoveries.set(
        request,
        captureErrorNoticeRecovery(requestOperation(request)),
      );
      return request;
    });
    client.interceptors.response.use(async (response, request) => {
      if (
        response.status === 401 &&
        connection?.credentials === "same-origin" &&
        new URL(request.url).origin === new URL(connection.baseUrl).origin
      ) {
        window.dispatchEvent(new Event("ally:web-session-expired"));
      }
      const recovery = recoveries.get(request);
      recoveries.delete(request);
      if (!response.ok || request.signal.aborted || !recovery?.length)
        return response;
      // JSON 正文损坏仍属于失败，不能仅凭 HTTP 200 移除原提示。
      if (
        response.status !== 204 &&
        response.headers.get("Content-Length") !== "0" &&
        response.headers.get("Content-Type")?.includes("json")
      ) {
        try {
          const text = await response.clone().text();
          if (text) JSON.parse(text);
        } catch {
          return response;
        }
      }
      resolveErrorNoticeRecovery(recovery);
      return response;
    });
    client.interceptors.error.use((error, response, request, options) => {
      const converted = toApiError(error, response?.status);
      converted.requestOperation = request
        ? requestOperation(request)
        : undefined;
      if (
        request?.signal.aborted ||
        (error instanceof DOMException && error.name === "AbortError")
      )
        return converted;
      const path = request ? new URL(request.url).pathname : "";
      // 发送失败由提交入口保存原输入和恢复动作，避免同一失败重复展示。
      const isSend =
        request?.method === "POST" &&
        (/\/chats$/.test(path) ||
          /\/messages$/.test(path) ||
          /\/channels\/[^/]+\/threads$/.test(path) ||
          /\/executions\/[^/]+\/retry$/.test(path));
      // Core 启动或短暂重连期间的传输错误交由调用方处理，不作为持久通知。
      const localSurface = hasLocalErrorSurface(options, request);
      // 资源页面的 404 由内容区和 Scene 标签提示，附属查询也不重复生成通知。
      const pageNotFound =
        request?.method === "GET" &&
        converted.status === 404 &&
        (/^\/api\/v1\/(?:chats|automatic-tasks|wakers|channels)\/[^/]+(?:\/|$)/.test(
          path,
        ) ||
          /^\/api\/v1\/workflows\/(?!sdk(?:\/|$)|chat-results(?:\/|$))[^/]+(?:\/|$)/.test(
            path,
          ));
      if (
        !isSend &&
        !localSurface &&
        !pageNotFound &&
        converted.code !== "NETWORK"
      ) {
        const operation = request ? requestOperation(request) : undefined;
        converted.noticeId = `${operation}:${converted.code}`;
        reportErrorNotice({
          id: converted.noticeId,
          scope: apiErrorScope(path),
          message: converted.message,
          operation,
        });
      }
      return converted;
    });
    interceptorInstalled = true;
  }
  return connection;
}

export function getApiConnection(): CoreConnection {
  if (!connection) {
    throw new Error("API 尚未初始化");
  }
  return connection;
}

/** 清除当前认证，阻止退出后的请求复用旧连接。 */
export function resetApiConnection(): void {
  connection = undefined;
  trustedOrigins.clear();
  client.setConfig({ headers: {}, credentials: "omit" });
}
