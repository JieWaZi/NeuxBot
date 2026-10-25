/** 已发送 Chat 浏览受管工作区的 projects/；草稿直接浏览当前工作目录根。 */
export function projectFilesScopePath(activeChatId?: string): string {
  return activeChatId ? "projects" : "";
}
