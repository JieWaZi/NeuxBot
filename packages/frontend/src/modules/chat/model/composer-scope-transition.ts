/** 判断是否为首条 create 成功后的 draft→chat 工作区切换。 */
export function isDraftToChatScopeTransition(
  previousScopeKey: string,
  nextScopeKey: string,
): boolean {
  if (
    previousScopeKey.startsWith("draft:") &&
    nextScopeKey.startsWith("chat:")
  ) {
    return true;
  }
  return (
    previousScopeKey.includes(":draft:") && !nextScopeKey.includes(":draft:")
  );
}
