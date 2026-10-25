import { ChatPageContent, type ChatWorkspaceEntry } from "../pages/chat-page";

type ChatComposerPanelProps = Omit<
  ChatWorkspaceEntry,
  | "mainContent"
  | "history"
  | "onNewChat"
  | "canvasInput"
  | "showSummary"
  | "emptyState"
> & { chatId?: string };

/** 业务指令复用普通聊天完整的配置、资源、输入与发送控制器，不创建额外 Chat。 */
export function ChatComposerPanel({
  chatId,
  ...entry
}: ChatComposerPanelProps) {
  return (
    <ChatPageContent chatId={chatId} entry={entry} presentation="composer" />
  );
}
