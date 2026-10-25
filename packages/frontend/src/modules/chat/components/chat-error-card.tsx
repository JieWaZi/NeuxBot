import { ApiError } from "@/shared/api/errors";
import {
  createContext,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import { useAuiState } from "@assistant-ui/react";
import {
  AlertTriangle,
  ChevronRight,
  ChevronUp,
  LoaderCircle,
  X,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import {
  dismissErrorNotice,
  dismissErrorNotices,
  runErrorNoticeAction,
  reportErrorNotice,
  useErrorNotices,
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
} from "@/shared/api/error-notices";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/shared/ui/collapsible";
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
}) {
  const { t } = useTranslation("chat");
  const [dismissed, setDismissed] = useState(false);
  const [expanded, setExpanded] = useState(!historical);
  const [pending, setPending] = useState(false);
  const [actionError, setActionError] = useState("");
  if (dismissed) return null;
  const missingSession = isMissingRuntimeSessionMessage(message);
  const presentation = chatErrorPresentation(message, t("executionIncomplete"));
  const actionPresentation = chatErrorPresentation(
    actionError,
    t("errorActionFailed"),
  );
  const details = [presentation.details, actionPresentation.details]
    .filter(Boolean)
    .join("\n\n");
  const readableMessage = presentation.message.includes(
    "does not support disabling thinking",
  )
    ? t("runtimeFailure.thinkingRequired")
    : presentation.message;
  const card = (
    <div
      role={historical ? undefined : "alert"}
      data-chat-error={id}
      data-work-result-body={execution ? "" : undefined}
      className="border-destructive/35 bg-destructive/8 type-body mt-3 rounded-md border px-3 py-2"
    >
      <div className="flex items-start gap-2">
        <AlertTriangle className="text-destructive size-icon-md mt-1 shrink-0" />
        <div className="min-w-0 flex-1">
          <p className="text-destructive break-words whitespace-pre-wrap">
            {readableMessage}
          </p>
          {actionError ? (
            <p className="text-destructive mt-1">
              {actionPresentation.message}
            </p>
          ) : null}
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
export function MessageFailure() {
  const message = useAuiState((state) => state.message);
  const actions = useContext(ChatErrorActions);
  const { t } = useTranslation("chat");
  if (
    message.status?.type !== "incomplete" ||
    message.status.reason !== "error"
  )
    return null;
  const meta = message.metadata?.custom;
  const executionId =
    typeof meta?.executionId === "string" ? meta.executionId : message.id;
  const text =
    typeof meta?.failureMessage === "string" && meta.failureMessage.trim()
      ? meta.failureMessage
      : t("executionIncomplete");
  const canRetry = meta?.retryable === true && message.isLast && actions.retry;
  const canRecover =
    meta?.failureCode === "SESSION_NOT_FOUND" &&
    message.isLast &&
    actions.recover;
  return (
    <ChatErrorCard
      key={`${executionId}:${message.isLast}`}
      id={`execution:${executionId}`}
      execution
      historical={!message.isLast}
      message={text}
      disabled={actions.disabled}
      onRetry={canRetry ? () => actions.retry?.(executionId) : undefined}
      onContinue={canRecover ? actions.recover : undefined}
    />
  );
}

export function ChatErrorNotices({
  scopes,
  fallback,
}: {
  scopes: readonly string[];
  fallback?: ChatSendFailure;
}) {
  const notices = useErrorNotices();
  const { t } = useTranslation("chat");
  const [expanded, setExpanded] = useState(false);
  const historyId = useId();
  const matchingNotices = notices.filter((notice) =>
    scopes.includes(notice.scope),
  );
  const latest = matchingNotices.at(-1);
  const previous = matchingNotices.slice(0, -1).reverse();
  return (
    <>
      {latest ? (
        <ChatErrorCard
          key={latest.id}
          id={latest.id}
          message={latest.message}
          onDismiss={() => dismissErrorNotice(latest.id)}
          onRetry={
            latest.action ? () => runErrorNoticeAction(latest.id) : undefined
          }
          actionLabel={
            latest.action === "check" ? t("checkErrorStatus") : undefined
          }
        />
      ) : null}
      {previous.length > 0 ? (
        <div className="mt-1 flex flex-wrap items-center justify-between gap-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            aria-expanded={expanded}
            aria-controls={historyId}
            onClick={() => setExpanded((value) => !value)}
          >
            {expanded
              ? t("hideEarlierErrors")
              : t("showEarlierErrors", { count: previous.length })}
          </Button>
          {expanded ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() =>
                dismissErrorNotices(matchingNotices.map((item) => item.id))
              }
            >
              {t("dismissAllErrors")}
            </Button>
          ) : null}
        </div>
      ) : null}
      {expanded && previous.length > 0 ? (
        <div id={historyId} aria-label={t("earlierErrors")}>
          {previous.map((notice) => (
            <ChatErrorCard
              key={notice.id}
              id={notice.id}
              message={notice.message}
              onDismiss={() => dismissErrorNotice(notice.id)}
              onRetry={
                notice.action
                  ? () => runErrorNoticeAction(notice.id)
                  : undefined
              }
              actionLabel={
                notice.action === "check" ? t("checkErrorStatus") : undefined
              }
            />
          ))}
        </div>
      ) : null}
      {scopes.length === 0 && fallback && isSendFailureActive(fallback) ? (
        <ChatErrorCard
          id={`send:${fallback.kind}:${fallback.message ?? ""}`}
          message={sendFailureDisplayText(fallback, t)}
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
      { id: operation, scope, operation, message, action: "retry" },
      () => retry.current?.(),
    );
    recovery.current = captureErrorNoticeRecovery(operation);
  }, [message, scope, noticeId, isNetworkError]);
}
