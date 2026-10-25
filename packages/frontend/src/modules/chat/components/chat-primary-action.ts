export type ChatPrimaryAction = "send" | "stop" | "queue";

/** 把 Chat 与团队工作状态合并为输入框唯一主操作。 */
export function chatPrimaryAction({
  isComposerEmpty,
  isRunning,
  isWorkRunning,
}: {
  isComposerEmpty: boolean;
  isRunning: boolean;
  isWorkRunning: boolean;
}): ChatPrimaryAction {
  if (!isRunning && !isWorkRunning) return "send";
  return isComposerEmpty ? "stop" : "queue";
}
