import { chatDraftHref } from "./chat-navigation";

export type ChatDraftIntent = {
  draftId: string;
  wakerId: string;
  text: string;
  pluginId?: string;
  skillId?: string;
  connectorId?: string;
};

/** 每次示例创建独立草稿，不覆盖任何已有会话或草稿输入。 */
export function chatPromptDestination(
  text: string,
  wakerId: string,
  pluginId?: string,
  skillId?: string,
  connectorId?: string,
) {
  const to = chatDraftHref(wakerId);
  const draftId = new URLSearchParams(to.split("?")[1]).get("draftId") ?? "";
  return {
    to,
    state: {
      chatDraftIntent: {
        draftId,
        wakerId,
        text,
        ...(pluginId ? { pluginId } : {}),
        ...(skillId ? { skillId } : {}),
        ...(connectorId ? { connectorId } : {}),
      } satisfies ChatDraftIntent,
    },
  };
}

/** 路由状态只贡献当前目标草稿的输入，不触发发送或建立安装事实。 */
export function readChatDraftIntent(
  state: unknown,
  draftId: string,
  wakerId: string,
): ChatDraftIntent | undefined {
  if (!plainObject(state)) return;
  const value = ownDataField(state, "chatDraftIntent");
  if (!plainObject(value)) return;
  const text = ownDataField(value, "text");
  if (
    !draftId ||
    !wakerId ||
    ownDataField(value, "draftId") !== draftId ||
    ownDataField(value, "wakerId") !== wakerId ||
    typeof text !== "string" ||
    !text.trim()
  )
    return;

  const references: Pick<
    ChatDraftIntent,
    "pluginId" | "skillId" | "connectorId"
  > = {};
  let referenceCount = 0;
  for (const key of ["pluginId", "skillId", "connectorId"] as const) {
    const descriptor = Object.getOwnPropertyDescriptor(value, key);
    if (!descriptor) continue;
    if (!("value" in descriptor) || typeof descriptor.value !== "string")
      return;
    if (!descriptor.value) continue;
    if (!descriptor.value.trim() || ++referenceCount > 1) return;
    references[key] = descriptor.value;
  }
  return { draftId, wakerId, text, ...references };
}

/** 路由状态只接受普通记录的自有数据字段，不执行访问器或读取原型链。 */
function plainObject(value: unknown): value is object {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const prototype: unknown = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
}

function ownDataField(value: object, key: string): unknown {
  const descriptor = Object.getOwnPropertyDescriptor(value, key);
  return descriptor && "value" in descriptor ? descriptor.value : undefined;
}
