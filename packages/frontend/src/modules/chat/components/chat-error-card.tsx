import { ApiError } from "@/shared/api/errors";
import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import type { AllyChatMessage } from "../runtime/chat-projection";
import {
  AlertTriangle,
  ChevronRight,
  ChevronUp,
  LoaderCircle,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { cn } from "@/shared/lib/cn";
import {
  dismissErrorNotice,
  dismissErrorNotices,
  runErrorNoticeAction,
  reportErrorNotice,
  apiErrorNoticePresentation,
  useErrorNotices,
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
  type ErrorNotice,
} from "@/shared/api/error-notices";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/shared/ui/collapsible";
import {
  runtimeFailurePresentation,
  runtimeSessionMissing,
} from "../model/runtime-failure-presentation";
import { chatErrorPresentation } from "../model/chat-error-presentation";
import type { ChatSendFailure } from "../runtime/chat-send-failure";
import {
  isMissingRuntimeSessionMessage,
  isSendFailureActive,
  sendFailureDisplayText,
} from "../runtime/chat-send-failure";

export const ChatErrorActions = createContext<{
  retry?: (executionId: string) => Promise<void>;
  recover?: () => void;
  disabled?: boolean;
  report?: (message: string, operation: string) => void;
  begin?: (operation: string) => () => void;
}>({});

export function ChatErrorCard({
  id,
  message,
  onRetry,
  onContinue,
  onDismiss,
  actionLabel,
  disabled,
  execution = false,
  historical = false,
  appearance = "card",
}: {
  id: string;
  message: string;
  onRetry?: () => void | Promise<void>;
  onContinue?: () => void;
  onDismiss?: () => void;
  actionLabel?: string;
  disabled?: boolean;
  execution?: boolean;
  historical?: boolean;
  appearance?: "card" | "composer";
}) {
  const { t } = useTranslation("chat");
  const [dismissed, setDismissed] = useState(false);
  const [expanded, setExpanded] = useState(!historical);
  const [pending, setPending] = useState(false);
  const [actionError, setActionError] = useState("");
  if (dismissed) return null;
  const currentMessage = actionError || message;
  const missingSession = isMissingRuntimeSessionMessage(currentMessage);
  const presentation = chatErrorPresentation(
    currentMessage,
    t(actionError ? "errorActionFailed" : "executionIncomplete"),
  );
  const details = presentation.details;
  const card = (
    <div
      role={historical ? undefined : "alert"}
      data-chat-error={id}
      data-work-result-body={execution ? "" : undefined}
      className={cn(
        appearance === "composer"
          ? "type-ui w-full min-w-0"
          : "border-destructive/35 bg-destructive/8 type-body mt-3 rounded-md border px-3 py-2",
      )}
    >
      <div className="flex items-start gap-2">
        <AlertTriangle className="text-destructive size-icon-md mt-1 shrink-0" />
        <div className="min-w-0 flex-1">
          <p
            className={cn(
              "break-words whitespace-pre-wrap",
              appearance === "composer"
                ? "text-foreground"
                : "text-destructive",
            )}
          >
            {presentation.message}
          </p>
        </div>
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={execution ? t("collapseFailure") : t("dismissError")}
          onClick={() => {
            if (execution) setExpanded(false);
            else if (onDismiss) onDismiss();
            else setDismissed(true);
          }}
        >
          {execution ? <ChevronUp /> : <X />}
        </Button>
      </div>
      {details ? (
        <Collapsible>
          <CollapsibleTrigger asChild>
            <Button size="sm" variant="ghost">
              {t("errorDetails")}
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <pre className="bg-muted type-body mt-2 max-h-80 overflow-auto rounded-md p-3 break-words whitespace-pre-wrap">
              {details}
            </pre>
          </CollapsibleContent>
        </Collapsible>
      ) : null}
      {onContinue || (onRetry && !missingSession) ? (
        <div data-chat-error-actions className="mt-2 flex justify-end">
          <Button
            size="sm"
            variant="ghost"
            disabled={disabled || pending}
            onClick={() => {
              if (onContinue) {
                onContinue();
                return;
              }
              if (pending) return;
              setPending(true);
              void Promise.resolve()
                .then(() => onRetry?.())
                .catch((error: unknown) =>
                  setActionError(
                    error instanceof ApiError
                      ? error.message
                      : t("errorActionFailed"),
                  ),
                )
                .finally(() => setPending(false));
            }}
          >
            {pending ? <LoaderCircle className="animate-spin" /> : null}
            {onContinue
              ? t("sessionTransitionAction")
              : (actionLabel ?? t("common:retry"))}
          </Button>
        </div>
      ) : null}
    </div>
  );
  if (!execution) return card;
  return (
    <Collapsible open={expanded} onOpenChange={setExpanded}>
      {!expanded ? (
        <CollapsibleTrigger asChild>
          <Button size="sm" variant="ghost" aria-label={t("expandFailure")}>
            <AlertTriangle className="text-destructive" />
            <span>{t("failed")}</span>
            <ChevronRight />
          </Button>
        </CollapsibleTrigger>
      ) : null}
      <CollapsibleContent>{card}</CollapsibleContent>
    </Collapsible>
  );
}

/** 失败是消息的独立状态块，永远不进入可复制的助手正文。 */
export function MessageFailure({
  message,
  last,
  placement = "message",
  onDismiss,
  appearance,
}: {
  message: AllyChatMessage;
  last: boolean;
  placement?: "message" | "composer";
  onDismiss?: () => void;
  appearance?: "card" | "composer";
}) {
  const actions = useContext(ChatErrorActions);
  const { t } = useTranslation("chat");
  if (message.status !== "failed" || (last && placement === "message"))
    return null;
  const executionId = message.executionId || message.id || "";
  const text =
    runtimeFailurePresentation(
      message.errorCode,
      message.errorMessage,
      message.errorParams,
      message.errorMessageSource,
    ).message || t("executionIncomplete");
  const canRetry = message.retryable === true && last && actions.retry;
  const canRecover =
    runtimeSessionMissing(message.errorCode) && last && actions.recover;
  return (
    <ChatErrorCard
      key={`${executionId}:${last}`}
      id={`execution:${executionId}`}
      execution={placement === "message"}
      historical={!last}
      message={text}
      onDismiss={onDismiss}
      appearance={appearance}
      disabled={actions.disabled}
      onRetry={canRetry ? () => actions.retry?.(executionId) : undefined}
      onContinue={canRecover ? actions.recover : undefined}
    />
  );
}

// 只保存提示的关闭身份，不复制执行状态；聊天重新打开后不恢复已覆盖的旧提示。
const dismissedFailuresStorageKey = "ally:chat-dismissed-failures";
let dismissedExecutionFailures: ReadonlySet<string> = (() => {
  try {
    const stored: unknown = JSON.parse(
      localStorage.getItem(dismissedFailuresStorageKey) ?? "[]",
    );
    return new Set(
      Array.isArray(stored)
        ? stored.filter((id): id is string => typeof id === "string")
        : [],
    );
  } catch {
    return new Set<string>();
  }
})();
const dismissedFailureListeners = new Set<() => void>();
function subscribeDismissedFailures(listener: () => void) {
  dismissedFailureListeners.add(listener);
  return () => {
    dismissedFailureListeners.delete(listener);
  };
}
function dismissExecutionFailure(id: string) {
  if (dismissedExecutionFailures.has(id)) return;
  dismissedExecutionFailures = new Set([...dismissedExecutionFailures, id]);
  try {
    localStorage.setItem(
      dismissedFailuresStorageKey,
      JSON.stringify([...dismissedExecutionFailures]),
    );
  } catch {
    /* 存储不可用时仍保留本次运行内的关闭状态。 */
  }
  dismissedFailureListeners.forEach((listener) => listener());
}

type ChatErrorNoticesProps = {
  scopes: readonly string[];
  fallback?: ChatSendFailure;
  failure?: AllyChatMessage;
  appearance?: "card" | "composer";
};

export function ChatErrorNotices(props: ChatErrorNoticesProps) {
  return useChatErrorNotices(props);
}

/** 同一份提示状态供独立区域与 Composer 顶栏复用，关闭后返回空槽位。 */
export function useChatErrorNotices({
  scopes,
  fallback,
  failure,
  appearance = "card",
}: ChatErrorNoticesProps) {
  const notices = useErrorNotices();
  const { t } = useTranslation("chat");
  const [hiddenFallbackId, setHiddenFallbackId] = useState<string>();
  const dismissedFailures = useSyncExternalStore(
    subscribeDismissedFailures,
    () => dismissedExecutionFailures,
    () => dismissedExecutionFailures,
  );
  const matchingNotices = notices.filter((notice) =>
    scopes.includes(notice.scope),
  );
  const failureId = failure?.executionId || failure?.id;
  const failureMessage = failure
    ? runtimeFailurePresentation(
        failure.errorCode,
        failure.errorMessage,
        failure.errorParams,
        failure.errorMessageSource,
      ).message
    : undefined;
  const sameFailure = (notice: ErrorNotice) => {
    if (failureId && dismissedFailures.has(failureId)) return false;
    if (!failure || (failure.createdAt && notice.createdAt < failure.createdAt))
      return false;
    return (
      notice.message === failureMessage ||
      notice.message === failure.errorMessage
    );
  };
  const duplicateNotices = matchingNotices.filter(sameFailure);
  const dismissFailure = () => {
    if (failureId) dismissExecutionFailure(failureId);
    dismissErrorNotices(duplicateNotices.map((notice) => notice.id));
  };
  type Item =
    | { kind: "notice"; notice: ErrorNotice; at: number }
    | { kind: "execution"; message: AllyChatMessage; at: number };
  const items: Item[] = matchingNotices
    .filter((notice) => !sameFailure(notice))
    .map((notice) => ({ kind: "notice", notice, at: notice.createdAt }));
  if (
    failure?.status === "failed" &&
    (!failureId || !dismissedFailures.has(failureId))
  ) {
    items.push({
      kind: "execution",
      message: failure,
      at: Math.max(
        failure.updatedAt ?? failure.createdAt ?? 0,
        ...duplicateNotices.map((notice) => notice.createdAt),
      ),
    });
  }
  items.sort((a, b) => a.at - b.at);
  const latest = items.at(-1);
  const latestNoticeId =
    latest?.kind === "notice" ? latest.notice.id : undefined;
  // 新错误替换旧提示；按版本清理，避免异步恢复删掉同 ID 的新失败。
  useEffect(() => {
    if (latestNoticeId && failure?.status === "failed" && failureId) {
      dismissExecutionFailure(failureId);
    }
    resolveErrorNoticeRecovery(
      notices.filter(
        (notice) =>
          scopes.includes(notice.scope) && notice.id !== latestNoticeId,
      ),
    );
  }, [notices, scopes, latestNoticeId, failure?.status, failureId]);
  const fallbackId =
    scopes.length === 0 && fallback && isSendFailureActive(fallback)
      ? `send:${fallback.kind}:${fallback.message ?? ""}`
      : undefined;
  if (!fallbackId && hiddenFallbackId) setHiddenFallbackId(undefined);
  const showFallback = fallbackId && fallbackId !== hiddenFallbackId;
  const renderItem = (item: Item) =>
    item.kind === "execution" ? (
      <MessageFailure
        key={item.message.executionId || item.message.id}
        message={item.message}
        last
        placement="composer"
        onDismiss={dismissFailure}
        appearance={appearance}
      />
    ) : (
      <ChatErrorCard
        key={`${item.notice.id}:${item.notice.version}`}
        id={item.notice.id}
        message={item.notice.message}
        appearance={appearance}
        onDismiss={() => dismissErrorNotice(item.notice.id)}
        onRetry={
          item.notice.action
            ? () => runErrorNoticeAction(item.notice.id)
            : undefined
        }
        actionLabel={
          item.notice.action === "check" ? t("checkErrorStatus") : undefined
        }
      />
    );
  if (!latest && !showFallback) return null;
  return (
    <>
      {latest ? renderItem(latest) : null}
      {!latest && showFallback && fallbackId && fallback ? (
        <ChatErrorCard
          key={fallbackId}
          id={fallbackId}
          message={sendFailureDisplayText(fallback, t)}
          appearance={appearance}
          onDismiss={() => setHiddenFallbackId(fallbackId)}
        />
      ) : null}
    </>
  );
}

/** 查询恢复后只清除该查询对应的错误，不清理同一聊天中的其他操作。 */
export function useChatHistoryError(
  error: { message: string; onRetry: () => void; error?: unknown } | undefined,
  scope: string | undefined,
) {
  const retry = useRef(error?.onRetry);
  const recovery = useRef<ReturnType<typeof captureErrorNoticeRecovery>>([]);
  useEffect(() => {
    retry.current = error?.onRetry ?? retry.current;
  }, [error?.onRetry]);
  const message = error?.message;
  const originalError = error?.error;
  const noticeId =
    error?.error instanceof ApiError ? error.error.noticeId : undefined;
  const isNetworkError =
    error?.error instanceof ApiError && error.error.code === "NETWORK";
  useEffect(() => {
    if (!message) {
      if (recovery.current[0]?.scope === scope)
        resolveErrorNoticeRecovery(recovery.current);
      recovery.current = [];
      return;
    }
    if (!scope || isNetworkError || noticeId) return;
    const operation = `history:${scope}`;
    reportErrorNotice(
      {
        id: operation,
        scope,
        operation,
        ...apiErrorNoticePresentation(originalError, message),
        action: "retry",
      },
      () => retry.current?.(),
    );
    recovery.current = captureErrorNoticeRecovery(operation);
  }, [message, scope, noticeId, isNetworkError, originalError]);
}
