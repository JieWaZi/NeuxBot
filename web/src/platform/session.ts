export type WebSession = {
  protocolVersion: number;
  csrfToken: string;
  expiresAt: number;
  capabilities: { workspace: boolean; git: boolean; terminal: boolean };
};
export class SessionError extends Error {
  constructor(readonly status: number) {
    super(`Web session: ${status}`);
  }
}

/** 同源会话协议在页面内存保存 CSRF；Cookie 始终由浏览器管理。 */
export async function requestSession(): Promise<WebSession> {
  const options = { credentials: "same-origin", cache: "no-store" } as const;
  let response = await fetch("/api/web/session", options);
  if (response.status === 401) {
    response = await fetch("/api/web/session", { ...options, method: "POST" });
  }
  if (!response.ok) throw new SessionError(response.status);
  const data: unknown = await response.json();
  if (
    !data ||
    typeof data !== "object" ||
    !("protocolVersion" in data) ||
    data.protocolVersion !== 1 ||
    !("csrfToken" in data) ||
    typeof data.csrfToken !== "string" ||
    !("expiresAt" in data) ||
    typeof data.expiresAt !== "number" ||
    !("capabilities" in data) ||
    !data.capabilities ||
    typeof data.capabilities !== "object" ||
    !["workspace", "git", "terminal"].every(
      (key) =>
        typeof (data.capabilities as Record<string, unknown>)[key] ===
        "boolean",
    ) ||
    !Number.isFinite(data.expiresAt) ||
    data.csrfToken.length === 0
  )
    throw new SessionError(409);
  return data as WebSession;
}
