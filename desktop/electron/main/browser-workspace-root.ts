import { realpath } from "node:fs/promises";
import type { CoreSupervisor } from "./core-supervisor";

/** 原生浏览器只接受 Core 为 Chat 冻结的精确目录；不保存第二份文件范围。 */
export async function authorizeBrowserRoot(
  core: CoreSupervisor,
  chatId: string,
  requestedRoot: string,
): Promise<string> {
  const connection = await core.connection();
  const response = await fetch(
    `${connection.baseUrl}/api/v1/chats/${encodeURIComponent(chatId)}`,
    {
      headers: { Authorization: `Bearer ${connection.token}` },
      signal: AbortSignal.timeout(5000),
    },
  );
  if (!response.ok) {
    await response.body?.cancel();
    throw new Error("无法验证 Chat 浏览器目录");
  }
  const value: unknown = await response.json();
  const chat =
    typeof value === "object" && value !== null && "chat" in value
      ? value.chat
      : undefined;
  if (
    typeof chat !== "object" ||
    chat === null ||
    !("id" in chat) ||
    chat.id !== chatId ||
    !("workspaceDirectory" in chat) ||
    typeof chat.workspaceDirectory !== "string"
  )
    throw new Error("Core 未返回匹配的 Chat 工作区");
  const sources =
    "additionalWorkingDirectories" in chat &&
    Array.isArray(chat.additionalWorkingDirectories)
      ? chat.additionalWorkingDirectories.filter(
          (v): v is string => typeof v === "string",
        )
      : [];
  const target = await realpath(requestedRoot);
  for (const root of [chat.workspaceDirectory, ...sources]) {
    if ((await realpath(root).catch(() => undefined)) === target) return target;
  }
  throw new Error("浏览器目录不属于此 Chat 范围");
}
