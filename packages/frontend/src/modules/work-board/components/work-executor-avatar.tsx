import { UsersRound } from "lucide-react";
import type { ReactNode } from "react";
import { TeamAvatarStack } from "@/modules/team";
import { useWorkExecutors } from "@/shared/hooks/use-work-executors";
import { cn } from "@/shared/lib/cn";
import { Avatar } from "@/shared/ui/avatar";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";

export type WorkExecutorKind = "waker" | "team" | "workflow";

export function normalizeWorkExecutorKind(
  kind?: string,
): WorkExecutorKind | undefined {
  if (kind === "team" || kind === "workflow" || kind === "waker") return kind;
  return undefined;
}

export function workExecutorKindFromOwner(
  ownerKind?: string,
): WorkExecutorKind | undefined {
  if (ownerKind === "team_thread") return "team";
  if (ownerKind === "workflow_run") return "workflow";
  if (ownerKind === "chat") return "waker";
  return undefined;
}

export function automaticTaskExecutorRef(task: {
  workflowId?: string;
  channelId?: string;
  executorWakerId?: string;
  wakerId?: string;
}): { kind: WorkExecutorKind; executorId: string } {
  if (task.workflowId) {
    return { kind: "workflow", executorId: task.workflowId };
  }
  if (task.channelId) {
    return { kind: "team", executorId: task.channelId };
  }
  return {
    kind: "waker",
    executorId: task.executorWakerId || task.wakerId || "",
  };
}

export function resolveWorkExecutorKind({
  kind,
  ownerKind,
  executorId,
  wakerIds,
  teamIds,
}: {
  kind?: string;
  ownerKind?: string;
  executorId?: string;
  wakerIds?: Set<string>;
  teamIds?: Set<string>;
}): WorkExecutorKind {
  const explicit = normalizeWorkExecutorKind(kind);
  if (explicit) return explicit;
  const fromOwner = workExecutorKindFromOwner(ownerKind);
  if (fromOwner) return fromOwner;
  if (executorId && teamIds?.has(executorId)) return "team";
  if (executorId && wakerIds?.has(executorId)) return "waker";
  return "workflow";
}

function ExecutorAvatarVisual({
  kind,
  name,
  size,
  wakerAvatar,
  teamMembers,
}: {
  kind: WorkExecutorKind;
  name: string;
  size: "sm" | "xs";
  wakerAvatar?: string;
  teamMembers?: Parameters<typeof TeamAvatarStack>[0]["members"];
}) {
  const frameClass = size === "xs" ? "size-avatar-xs" : "size-avatar-sm";
  if (kind === "team") {
    if (teamMembers?.length) {
      return (
        <TeamAvatarStack members={teamMembers} size={size} animated={false} />
      );
    }
    return (
      <span
        className={cn(
          "bg-muted text-muted-foreground grid place-items-center overflow-hidden rounded-sm",
          frameClass,
        )}
        aria-hidden="true"
      >
        <UsersRound className="size-icon-md" strokeWidth={1.5} />
      </span>
    );
  }
  if (kind === "workflow") {
    return (
      <Avatar
        name={name}
        size={size}
        shape="square"
        interactive={false}
        animated={false}
      />
    );
  }
  return (
    <Avatar
      name={name}
      src={wakerAvatar}
      size={size}
      shape="square"
      interactive={false}
      animated={false}
    />
  );
}

export function WorkExecutorAvatar({
  name,
  kind,
  executorId,
  ownerKind,
  size = "sm",
}: {
  name: string;
  kind?: string;
  executorId?: string;
  ownerKind?: string;
  size?: "sm" | "xs";
}) {
  const executors = useWorkExecutors();
  const wakerIds = new Set(
    executors.data?.wakers.flatMap((waker) => (waker.id ? [waker.id] : [])),
  );
  const teamIds = new Set(
    executors.data?.teams.flatMap((team) =>
      team.channel?.id ? [team.channel.id] : [],
    ),
  );
  const resolvedKind = resolveWorkExecutorKind({
    kind,
    ownerKind,
    executorId,
    wakerIds,
    teamIds,
  });
  const label = name.trim() || "—";
  const waker =
    resolvedKind === "waker"
      ? executors.data?.wakers.find((item) => item.id === executorId)
      : undefined;
  const team =
    resolvedKind === "team"
      ? executors.data?.teams.find((item) => item.channel?.id === executorId)
      : undefined;
  const visual: ReactNode = (
    <ExecutorAvatarVisual
      kind={resolvedKind}
      name={label}
      size={size}
      wakerAvatar={waker?.avatar}
      teamMembers={team?.members}
    />
  );
  return (
    <TooltipProvider delayDuration={300}>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="inline-flex align-middle" aria-label={label}>
            {visual}
          </span>
        </TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
