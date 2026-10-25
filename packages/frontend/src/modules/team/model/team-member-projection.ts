import type {
  GithubComJieWaZiNeuxBotServerApisChannelsChannelMemberDetail as ChannelMemberDetail,
  GithubComJieWaZiNeuxBotServerApisChannelsChannelThreadDetail as ChannelThreadDetail,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsChannelThreadMember as ChannelThreadMember,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsWaker as Waker,
} from "@/shared/api/generated/types.gen";
import { permissionLevelOf } from "@/shared/lib/runtime-permissions";

import type { TeamChatContext } from "../team-chat-context";
import { teamMemberTaskStatusOf } from "./team-member-task-status";

type TeamMemberProjectionInput = {
  channelMembers: readonly ChannelMemberDetail[];
  fallbackName: string;
  leadWakerId: string;
  roster?: "execution" | "navigation";
  threadDetail?: ChannelThreadDetail;
};

type ProjectedTeamMember = TeamChatContext["members"][number];

type MemberProjectionContext = {
  activeTaskSubjectsByWakerId: Map<string, string>;
  fallbackName: string;
  leadWakerId: string;
  memberActivityByWakerId: Map<
    string,
    NonNullable<ChannelThreadDetail["memberActivities"]>[number]
  >;
  memberDetailsByWakerId: Map<string, Waker>;
  pendingInteractionTypesByWakerId: Map<string, Set<string>>;
  taskStatusesByWakerId: Map<
    string,
    Array<NonNullable<ChannelThreadDetail["tasks"]>[number]["status"]>
  >;
};

function buildMemberProjectionContext({
  channelMembers,
  fallbackName,
  leadWakerId,
  threadDetail,
}: Omit<TeamMemberProjectionInput, "roster">): MemberProjectionContext {
  const memberDetailsByWakerId = new Map(
    channelMembers.flatMap((member) =>
      member.waker?.id ? ([[member.waker.id, member.waker]] as const) : [],
    ),
  );
  const memberActivityByWakerId = new Map(
    (threadDetail?.memberActivities ?? []).flatMap((activity) =>
      activity.wakerId ? ([[activity.wakerId, activity]] as const) : [],
    ),
  );
  const pendingInteractionTypesByWakerId = new Map<string, Set<string>>();
  for (const interaction of threadDetail?.pendingInteractions ?? []) {
    if (!interaction.wakerId || !interaction.type) continue;
    const types = pendingInteractionTypesByWakerId.get(interaction.wakerId);
    if (types) types.add(interaction.type);
    else {
      pendingInteractionTypesByWakerId.set(
        interaction.wakerId,
        new Set([interaction.type]),
      );
    }
  }
  const taskStatusesByWakerId = new Map<
    string,
    Array<NonNullable<ChannelThreadDetail["tasks"]>[number]["status"]>
  >();
  const activeTaskSubjectsByWakerId = new Map<string, string>();
  for (const task of threadDetail?.tasks ?? []) {
    if (!task.assigneeWakerId) continue;
    const statuses = taskStatusesByWakerId.get(task.assigneeWakerId);
    if (statuses) statuses.push(task.status);
    else taskStatusesByWakerId.set(task.assigneeWakerId, [task.status]);
    if (task.status === "in_progress" && task.subject) {
      activeTaskSubjectsByWakerId.set(task.assigneeWakerId, task.subject);
    }
  }

  return {
    activeTaskSubjectsByWakerId,
    fallbackName,
    leadWakerId,
    memberActivityByWakerId,
    memberDetailsByWakerId,
    pendingInteractionTypesByWakerId,
    taskStatusesByWakerId,
  };
}

function projectOneTeamMember({
  channelMember,
  context,
  threadMember,
  wakerId,
}: {
  channelMember?: ChannelMemberDetail;
  context: MemberProjectionContext;
  threadMember?: ChannelThreadMember;
  wakerId: string;
}): ProjectedTeamMember {
  const waker = context.memberDetailsByWakerId.get(wakerId);
  const activity = context.memberActivityByWakerId.get(wakerId);
  const pendingInteractionTypes =
    context.pendingInteractionTypesByWakerId.get(wakerId);
  const taskStatus = teamMemberTaskStatusOf(
    context.taskStatusesByWakerId.get(wakerId) ?? [],
  );

  return {
    activeExecutionId: activity?.executionId,
    activeTaskSubject: context.activeTaskSubjectsByWakerId.get(wakerId),
    accountId: threadMember?.accountId ?? channelMember?.accountId,
    avatar: threadMember?.avatar || waker?.avatar,
    chatId: threadMember?.chatId || activity?.chatId,
    id: wakerId,
    isLead: wakerId === context.leadWakerId,
    model: threadMember?.model ?? channelMember?.model ?? "",
    name: threadMember?.displayName || waker?.name || context.fallbackName,
    permissionLevel:
      permissionLevelOf(
        threadMember?.permissionLevel ?? channelMember?.permissionLevel,
      ) ?? "auto",
    reasoning: threadMember?.reasoning ?? channelMember?.reasoning ?? "",
    runtimeId: threadMember?.runtimeId ?? channelMember?.runtimeId ?? "",
    taskStatus,
    waitingApproval:
      pendingInteractionTypes?.has("approval") ||
      (activity?.state === "requires_action" &&
        !pendingInteractionTypes?.has("elicitation")),
    waitingElicitation: pendingInteractionTypes?.has("elicitation"),
  };
}

function projectNavigationTeamMembers(
  channelMembers: readonly ChannelMemberDetail[],
  context: MemberProjectionContext,
  threadMembers: readonly ChannelThreadMember[],
): ProjectedTeamMember[] {
  const threadMemberByWakerId = new Map(
    threadMembers.flatMap((member) =>
      member.wakerId ? ([[member.wakerId, member]] as const) : [],
    ),
  );
  const channelWakerIds = new Set(
    channelMembers.flatMap((member) =>
      member.waker?.id ? [member.waker.id] : [],
    ),
  );

  const currentMembers = channelMembers.flatMap((channelMember) => {
    const wakerId = channelMember.waker?.id;
    if (!wakerId) return [];
    return [
      projectOneTeamMember({
        channelMember,
        context,
        threadMember: threadMemberByWakerId.get(wakerId),
        wakerId,
      }),
    ];
  });

  const historicalMembers = [...threadMembers]
    .sort((left, right) => (left.position ?? 0) - (right.position ?? 0))
    .flatMap((threadMember) => {
      const wakerId = threadMember.wakerId;
      if (!wakerId || channelWakerIds.has(wakerId)) return [];
      return [
        projectOneTeamMember({
          context,
          threadMember,
          wakerId,
        }),
      ];
    });

  return [...currentMembers, ...historicalMembers];
}

/**
 * 把团队配置和当前会话快照投影成统一成员视图。
 * execution 在已有会话中只使用冻结成员；navigation 侧栏以当前团队配置为主并保留历史成员。
 */
export function projectTeamMembers({
  channelMembers,
  fallbackName,
  leadWakerId,
  roster = "execution",
  threadDetail,
}: TeamMemberProjectionInput): TeamChatContext["members"] {
  const context = buildMemberProjectionContext({
    channelMembers,
    fallbackName,
    leadWakerId,
    threadDetail,
  });
  const threadMembers = threadDetail?.members ?? [];

  if (roster === "navigation" && threadMembers.length > 0) {
    return projectNavigationTeamMembers(channelMembers, context, threadMembers);
  }

  if (threadMembers.length > 0) {
    return [...threadMembers]
      .sort((left, right) => (left.position ?? 0) - (right.position ?? 0))
      .flatMap((member) => {
        const wakerId = member.wakerId;
        if (!wakerId) return [];
        return [
          projectOneTeamMember({
            channelMember: channelMembers.find(
              (channelMember) => channelMember.waker?.id === wakerId,
            ),
            context,
            threadMember: member,
            wakerId,
          }),
        ];
      });
  }

  return channelMembers.flatMap((member) => {
    const waker = member.waker;
    return waker?.id
      ? [
          projectOneTeamMember({
            channelMember: member,
            context,
            wakerId: waker.id,
          }),
        ]
      : [];
  });
}
