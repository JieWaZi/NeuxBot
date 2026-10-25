import { lazy } from "react";

export {
  chatListInfiniteOptions,
  chatMessagesInfiniteOptions,
  chatMessagesKey,
  evictDeletedChatsFromCache,
  getChatOptions,
  listWakerChatsOptions,
} from "./api/chat-queries";
export type { ChatRightPanel } from "./components/chat-right-panel-host";
export { ChatTaskDialog } from "./components/chat-task-dialog";
export { ChatPageContent, ChatPage } from "./pages/chat-page";
export { TeamChatPageContent } from "./pages/team-chat-page";
export type { TeamChatContext } from "@/modules/team";

export { ChatForkMenuItem } from "./components/chat-fork";
export { ChatApprovalCard } from "./components/chat-approval-card";
export { ChatElicitationCard } from "./components/chat-elicitation-card";
export { ReadOnlyMarkdown } from "./components/read-only-markdown";
// 文件产出入口按需加载现有查看器，避免将编辑器与 Diff 依赖带入启动工作台。
export const WorkspaceFileViewer = lazy(() =>
  import("./components/workspace-file-viewer").then((module) => ({
    default: module.WorkspaceFileViewer,
  })),
);
export { WorkspaceDetailSplit } from "./components/workspace-detail-split";

export { FloatingChat } from "./components/floating-chat";
export { ChatPane } from "./components/chat-pane";
export { ChatQueueProvider } from "./runtime/chat-queue-store";
export { WorkbenchProvider } from "./workbench/workbench-store";

export type { ChatWorkspaceEntry } from "./pages/chat-page";

export {
  ChatHistoryPopover,
  ChatHistoryPanelSurface,
  ChatHistoryNavigationRow,
} from "./components/chat-history-navigation";
export { ChatGroupHeader } from "./components/chat-group-header";

export { RetainedChatPageContent } from "./pages/retained-chat-page";

export {
  ChatContributionsProvider,
  ChatMessageContributionProvider,
  useOpenWorkbenchContent,
} from "./components/chat-contributions";
export type {
  ChatContributionScopeProps,
  WorkbenchContributionProps,
} from "./components/chat-contributions";
export type { WorkbenchContentReference } from "./workbench/workbench-model";

export { ChatComposerPanel } from "./components/chat-composer-panel";
