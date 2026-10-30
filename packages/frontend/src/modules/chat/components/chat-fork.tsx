import { createContext, useContext } from "react";
import {
  useIsMutating,
  useMutation,
  useInfiniteQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import { useAuiState } from "@assistant-ui/react";
import { Shuffle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { toast } from "@/shared/ui/sonner";

import { forkChatMutation } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { GithubComJieWaZiNeuxBotServerInternalChatMessageviewMessage as ChatMessage } from "@/shared/api/generated/types.gen";
import { chatMessagesInfiniteOptions } from "../api/chat-queries";
import { toApiError } from "@/shared/api/errors";
import { chatHref } from "@/shared/lib/chat-navigation";
import { Button } from "@/shared/ui/button";
import { DropdownMenuItem } from "@/shared/ui/dropdown-menu";

// 同一查询客户端中的菜单和回复操作栏共用请求身份及即时互斥。
const operations = new WeakMap<
  QueryClient,
  { pending: Set<string>; requests: Map<string, string> }
>();

const requestStoragePrefix = "chat-fork-request:";

function forkUnavailableReason(
  code: string | undefined,
  t: ReturnType<typeof useTranslation<"chat">>["t"],
) {
  switch (code) {
    case "team":
      return t("forkUnavailableTeam");
    case "busy":
      return t("forkUnavailableBusy");
    case "unfinished":
      return t("forkUnavailableUnfinished");
    case "session_missing":
      return t("forkUnavailableSessionMissing");
    case "session_changed":
      return t("forkUnavailableSessionChanged");
    case "latest_only":
      return t("forkUnavailableLatestOnly");
    default:
      return t("forkUnavailablePositionMissing");
  }
}

function storedRequestId(key: string): string | null {
  try {
    return localStorage.getItem(requestStoragePrefix + key);
  } catch {
    return null;
  }
}

function saveRequestId(key: string, value: string) {
  try {
    localStorage.setItem(requestStoragePrefix + key, value);
  } catch {
    // 存储不可用时，同一窗口仍使用查询客户端中的请求身份。
  }
}

function clearRequestId(key: string) {
  try {
    localStorage.removeItem(requestStoragePrefix + key);
  } catch {
    // 已完成的请求不再依赖本地存储。
  }
}

type ForkTarget = {
  executionId: string;
  available: boolean;
  latest: boolean;
  reasonCode?: string;
};

/** head 基线始终包含全会话末尾；旧页只追加在其之前。 */
export function messageForkTargets(
  messages: readonly ChatMessage[],
  latest: string | undefined,
): ForkTarget[] {
  return messages.flatMap((message) => {
    const executionId = message.inherited ? message.id : message.executionId;
    if (!executionId || !message.fork) return [];
    const isLatest = executionId === latest;
    const outdated = Boolean(message.fork.latestOnly && !isLatest);
    return [
      {
        executionId,
        latest: isLatest,
        available: Boolean(message.fork.available && !outdated),
        reasonCode: outdated ? "latest_only" : message.fork.reasonCode,
      },
    ];
  });
}

type ForkControls = {
  targets: ForkTarget[];
  pending: boolean;
  busy: boolean;
  fork: (executionId: string) => Promise<void>;
};

export const ChatForkContext = createContext<ForkControls | undefined>(
  undefined,
);

export function useChatFork({
  chatId,
  wakerId,
  busy = false,
  enabled = true,
  messages,
  latestExecutionId,
}: {
  chatId?: string;
  wakerId?: string;
  busy?: boolean;
  enabled?: boolean;
  messages: readonly ChatMessage[];
  latestExecutionId?: string;
}): ForkControls {
  const { t } = useTranslation("chat");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const active = Boolean(chatId) && enabled;
  const mutationKey = ["chat-fork", chatId];
  const mutation = useMutation({ ...forkChatMutation(), mutationKey });
  const pending = useIsMutating({ mutationKey }) > 0;
  let operation = operations.get(queryClient);
  if (!operation) {
    operation = {
      pending: new Set<string>(),
      requests: new Map<string, string>(),
    };
    operations.set(queryClient, operation);
  }

  return {
    targets: active ? messageForkTargets(messages, latestExecutionId) : [],
    pending,
    busy,
    fork: async (executionId) => {
      if (!chatId || busy || operation.pending.has(chatId)) return;
      operation.pending.add(chatId);
      const key = `${chatId}:${executionId}`;
      const clientRequestId =
        operation.requests.get(key) ??
        storedRequestId(key) ??
        crypto.randomUUID();
      operation.requests.set(key, clientRequestId);
      saveRequestId(key, clientRequestId);
      try {
        const result = await mutation.mutateAsync({
          path: { chatId },
          body: { executionId, clientRequestId },
        });
        await queryClient.invalidateQueries({ queryKey: ["chats", "list"] });
        if (result.chatId) {
          operation.requests.delete(key);
          clearRequestId(key);
          navigate(chatHref(result.chatId, wakerId ?? ""));
        }
      } catch (error) {
        const failure = toApiError(error);
        toast.error(
          failure.code === "NETWORK" ? t("forkFailed") : failure.message,
        );
      } finally {
        operation.pending.delete(chatId);
      }
    },
  };
}

export function MessageForkAction() {
  const { t } = useTranslation("chat");
  const controls = useContext(ChatForkContext);
  const id = useAuiState((state) => state.message.id);
  const status = useAuiState((state) => state.message.status?.type);
  const target = controls?.targets.find((item) => item.executionId === id);
  if (!controls || controls.busy || !target || !status || status === "running")
    return null;
  const reason = target.available
    ? t("forkHere")
    : forkUnavailableReason(target.reasonCode, t);
  return (
    <span className="inline-flex" title={reason}>
      <Button
        type="button"
        size="icon-sm"
        variant="ghost"
        aria-label={t("forkHere")}
        disabled={!target.available || controls.pending}
        onClick={() => void controls.fork(id)}
      >
        <Shuffle />
      </Button>
    </span>
  );
}

function ForkMenuAction({
  controls,
  busy = false,
}: {
  controls?: ForkControls;
  busy?: boolean;
}) {
  const { t } = useTranslation("chat");
  const target = controls?.targets.find((item) => item.latest);
  return (
    <span
      className="block"
      title={
        busy
          ? t("forkUnavailableBusy")
          : target?.available
            ? undefined
            : forkUnavailableReason(target?.reasonCode, t)
      }
    >
      <DropdownMenuItem
        disabled={!target?.available || controls?.pending || busy}
        onSelect={() => {
          if (target?.executionId) void controls?.fork(target.executionId);
        }}
      >
        <Shuffle />
        {t("forkCurrent")}
      </DropdownMenuItem>
    </span>
  );
}

// 用户打开且会话空闲才挂载读取；每次挂载核对同一来源，不能把新鲜 running 缓存当成终态。
function IdleChatForkMenuItem({
  chatId,
  wakerId,
}: {
  chatId: string;
  wakerId?: string;
}) {
  const query = useInfiniteQuery({
    ...chatMessagesInfiniteOptions(chatId),
    refetchOnMount: "always",
  });
  const controls = useChatFork({
    chatId,
    wakerId,
    messages: [...(query.data?.pages ?? [])]
      .reverse()
      .flatMap((page) => page.resources ?? []),
    latestExecutionId: query.data?.pages[0]?.forkLatestExecutionId,
    enabled: query.isSuccess && query.isFetchedAfterMount && !query.isFetching,
  });
  return <ForkMenuAction controls={controls} />;
}

export function ChatForkMenuItem({
  chatId,
  wakerId,
  enabled,
  busy = false,
}: {
  chatId: string;
  wakerId?: string;
  enabled: boolean;
  busy?: boolean;
}) {
  return enabled && !busy ? (
    <IdleChatForkMenuItem chatId={chatId} wakerId={wakerId} />
  ) : (
    <ForkMenuAction busy={busy} />
  );
}
