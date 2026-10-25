import type { ReactNode } from "react";

import type {
  CreateChannelThreadResponse,
  GithubComJieWaZiNeuxBotServerApisChannelsChannelWorkInput as ChannelWorkInput,
  GithubComJieWaZiNeuxBotServerApisChannelsCreateChannelThreadRequest as CreateChannelThreadRequest,
  GithubComJieWaZiNeuxBotServerConstantsChannelTaskStatus as ChannelTaskStatus,
  GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessage as ChatMessage,
  SendChannelMessageResponse,
} from "@/shared/api/generated/types.gen";
import type { PermissionLevel } from "@/shared/lib/runtime-permissions";

/** 团队聊天页注入的 channel/thread 上下文，与 TeamThreadPage 控制器对齐。 */
export type TeamChatContext = {
  channelId: string;
  description?: string;
  leadActiveExecutionId?: string;
  leadProcessing: boolean;
  isStopping?: boolean;
  leadWakerId: string;
  members: Array<{
    activeExecutionId?: string;
    activeTaskSubject?: string;
    avatar?: string;
    chatId?: string;
    id: string;
    isLead: boolean;
    accountId?: string;
    model: string;
    name: string;
    permissionLevel: PermissionLevel;
    reasoning: string;
    runtimeId: string;
    taskStatus?: ChannelTaskStatus;
    waitingApproval?: boolean;
    waitingElicitation?: boolean;
  }>;
  name: string;
  timeline: Array<{
    chatId?: string;
    coordinator: boolean;
    directDelivered?: boolean;
    message: ChatMessage;
    wakerId?: string;
  }>;
  hasEarlierTimelineMessages: boolean;
  isLoadingEarlierTimelineMessages: boolean;
  onLoadEarlierTimelineMessages: () => Promise<void>;
  onThreadChanged: () => void;
  onThreadCreated: (threadId: string) => void;
  onCreateThread: (
    request: CreateChannelThreadRequest,
  ) => Promise<CreateChannelThreadResponse>;
  onSendMessage: (
    request: ChannelWorkInput,
  ) => Promise<SendChannelMessageResponse>;
  onStopWork: () => Promise<void>;
  onUpdateMemberConfigs: (
    members: Array<{
      accountId?: string;
      model: string;
      permissionLevel: PermissionLevel;
      reasoning: string;
      runtimeId: string;
      wakerId: string;
    }>,
  ) => Promise<void>;
  renderTaskBoard: () => ReactNode;
  loadError?: {
    error?: unknown;
    message: string;
    onRetry: () => void;
  };
  threadId?: string;
  workspaceDirectory?: string;
  additionalWorkingDirectories?: string[];
  workProgress: {
    activeMemberName?: string;
    activeTaskSubject?: string;
    phase:
      | "coordinating"
      | "executing"
      | "idle"
      | "review"
      | "stopping"
      | "summarizing";
    review: number;
    terminal: number;
    total: number;
  };
  workState: "idle" | "review" | "running";
  threadDetailLoading?: boolean;
  /** 首屏团队时间线尚未返回；后台刷新不重新置为加载态。 */
  timelineLoading?: boolean;
  channelDetailLoading?: boolean;
};

export type TeamMemberExecutionConfig = {
  accountId?: string;
  model: string;
  permissionLevel: PermissionLevel;
  reasoning: string;
  runtimeId: string;
};
