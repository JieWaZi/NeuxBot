import {
  ConversationAttentionMark,
  conversationAttentionMark,
  type ConversationMark,
} from "@/shared/ui/conversation-attention";
import { MoreHorizontal, Pencil, Trash2, ShieldCheck } from "lucide-react";
import {
  createContext,
  useState,
  type MouseEvent,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { useChatProtection } from "@/shared/api/chat-protection";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { cn } from "@/shared/lib/cn";
import { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
import { Button } from "@/shared/ui/button";
import { RuntimeCliIcon } from "@/shared/ui/runtime-cli-icon";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";

export const ChatHistoryContentContext = createContext<ReactNode>(null);

export function ChatHistoryPanelSurface({ children }: { children: ReactNode }) {
  return (
    <div
      className="scrollbar-hidden flex h-full min-h-0 flex-col overflow-y-auto overscroll-contain p-2"
      data-chat-history-list
    >
      {children}
    </div>
  );
}

function ChatHistoryRowStatus({
  status,
  mark,
}: {
  status: "idle" | "running" | "waiting";
  mark?: ConversationMark;
}) {
  if (mark?.kind === "action") {
    return <ConversationAttentionMark mark={mark} />;
  }
  return (
    <span
      aria-hidden="true"
      data-history-status={status}
      className={cn(
        "block size-1.5 shrink-0 place-self-center rounded-full",
        status === "running" && "bg-success",
        status === "waiting" && "bg-warning",
        status === "idle" && "bg-border-strong",
      )}
    />
  );
}

type ProtectionChoice = { chatId: string; wakerId: string; protected: boolean };

function ChatProtectionMenuItem({
  choice,
  onDone,
}: {
  choice: ProtectionChoice;
  onDone: () => void;
}) {
  const { t } = useTranslation("storage");
  const mutation = useChatProtection();
  return (
    <>
      <DropdownMenuItem
        disabled={mutation.isPending}
        onSelect={(event) => {
          event.preventDefault();
          mutation.mutate(
            { ...choice, protected: !choice.protected },
            { onSuccess: onDone },
          );
        }}
      >
        <ShieldCheck />
        {t(choice.protected ? "unprotect" : "protect")}
      </DropdownMenuItem>
      {mutation.isError ? (
        <p role="alert" className="type-ui text-danger px-2 py-1">
          {localApiErrorMessage(mutation.error, t("actionFailed"))}
        </p>
      ) : null}
    </>
  );
}

export function ChatHistoryNavigationRow({
  active,
  title,
  status,
  runtimeId,
  modelId,
  requestKinds,
  unreadCount,
  href,
  onOpen,
  onPrefetch,
  onAction,
  allowRename = true,
  metadata,
  extraMenu,
  protection,
}: {
  active: boolean;
  title: string;
  status: "idle" | "running" | "waiting";
  runtimeId?: string;
  modelId?: string;
  requestKinds?: readonly string[];
  unreadCount?: number;
  href?: string;
  onOpen: (event: MouseEvent<HTMLElement>) => void;
  onPrefetch?: () => void;
  onAction: (mode: "rename" | "delete") => void;
  allowRename?: boolean;
  metadata?: ReactNode;
  protection?: ProtectionChoice;
  extraMenu?: (open: boolean) => ReactNode;
}) {
  const { t } = useTranslation("common");
  const [menuOpen, setMenuOpen] = useState(false);
  const model = modelId?.trim();
  const runtime = runtimeId?.trim();
  const modelLabel =
    model || (runtime ? runtimeDisplayName({ id: runtime }) : undefined);
  const mark = conversationAttentionMark({
    requestKinds,
    unread: unreadCount,
  });
  const contents = (
    <>
      <ChatHistoryRowStatus status={status} mark={mark} />
      <span
        aria-hidden="true"
        className="size-icon-sm grid place-items-center"
        title={modelLabel}
      >
        {modelLabel ? (
          <RuntimeCliIcon
            runtimeId={runtime}
            size={14}
            className="size-icon-sm"
          />
        ) : null}
      </span>
      <span className={cn("min-w-0 truncate", active && "font-medium")}>
        {title}
      </span>
      {metadata || mark?.kind === "unread" ? (
        <span className="flex shrink-0 items-center gap-1.5">
          <ConversationAttentionMark
            mark={mark?.kind === "unread" ? mark : undefined}
          />
          {metadata}
        </span>
      ) : null}
    </>
  );
  const props = {
    onClick: onOpen,
    onFocus: onPrefetch,
    onMouseEnter: onPrefetch,
    title,
    "aria-current": active ? ("page" as const) : undefined,
    "aria-description": modelLabel,
    className:
      "focus-visible:ring-ring type-label grid min-h-row-compact min-w-0 grid-cols-[var(--spacing-icon-sm)_var(--spacing-icon-sm)_minmax(0,1fr)_auto] items-center gap-1.5 rounded-md px-2 py-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset",
  };
  return (
    <div
      className={cn(
        "group/chat grid w-full min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center rounded-md pr-0.5",
        active
          ? "bg-action-quiet-pressed text-foreground"
          : "hover:bg-action-quiet-hover",
      )}
      onContextMenu={(event) => {
        event.preventDefault();
        setMenuOpen(true);
      }}
    >
      {href ? (
        <Link to={href} {...props}>
          {contents}
        </Link>
      ) : (
        <button type="button" {...props}>
          {contents}
        </button>
      )}
      <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger asChild>
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            className="opacity-0 group-focus-within/chat:opacity-100 group-hover/chat:opacity-100 hover:bg-transparent data-[state=open]:bg-transparent data-[state=open]:opacity-100"
            aria-label={t("partnerNavigation.chatMenu", { title })}
          >
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {extraMenu?.(menuOpen)}
          {protection && menuOpen ? (
            <ChatProtectionMenuItem
              choice={protection}
              onDone={() => setMenuOpen(false)}
            />
          ) : null}
          {allowRename ? (
            <DropdownMenuItem onSelect={() => onAction("rename")}>
              <Pencil />
              {t("partnerNavigation.renameChat")}
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuItem
            variant="destructive"
            onSelect={() => onAction("delete")}
          >
            <Trash2 />
            {t("partnerNavigation.deleteChat")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
