import {
  ArrowLeft,
  MessageSquareText,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import { useState, type MouseEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";

/** 会话来源由入口提供；导航浮层、列表和操作样式共用。 */
export function ChatHistoryPopover({
  children,
}: {
  children: (close: () => void) => ReactNode;
}) {
  const { t } = useTranslation("common");
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          aria-label={t("partnerNavigation.viewChatHistory")}
          className="group/launcher hover:w-control-launcher focus-visible:w-control-launcher transition-[width,background-color,color,box-shadow,opacity]"
          shape="corner"
          size="icon-lg"
          type="button"
          variant="launcher"
        >
          <span className="size-control-lg grid shrink-0 place-items-center">
            <MessageSquareText className="size-icon-lg" strokeWidth={1.5} />
          </span>
          <span
            aria-hidden="true"
            className="type-ui -translate-x-1 pr-4 whitespace-nowrap opacity-0 transition-[opacity,transform] group-hover/launcher:translate-x-0 group-hover/launcher:opacity-100 group-focus-visible/launcher:translate-x-0 group-focus-visible/launcher:opacity-100"
          >
            {t("partnerNavigation.viewChatHistory")}
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        aria-label={t("partnerNavigation.conversationHistory")}
        collisionPadding={16}
        side="top"
        sideOffset={8}
        variant="navigation"
      >
        {children(() => setOpen(false))}
      </PopoverContent>
    </Popover>
  );
}

export function ChatHistoryPanelSurface({
  children,
  onBack,
  onNew,
}: {
  children: ReactNode;
  onBack?: () => void;
  onNew: () => void;
}) {
  const { t } = useTranslation("common");
  return (
    <div
      className="flex h-80 max-h-[calc(var(--radix-popover-content-available-height)-0.5rem)] min-h-0 w-80 flex-col p-1"
      data-chat-history-panel
    >
      <div className="min-h-row-default mb-1 flex items-center justify-between gap-2 px-1">
        <div className="flex min-w-0 items-center gap-1">
          {onBack ? (
            <Button
              aria-label={t("partnerNavigation.backToEntities")}
              onClick={onBack}
              size="icon-sm"
              type="button"
              variant="ghost"
            >
              <ArrowLeft />
            </Button>
          ) : null}
          <h2 className="type-label truncate font-medium">
            {t("partnerNavigation.conversationList")}
          </h2>
        </div>
        <Button
          aria-label={t("partnerNavigation.newChat")}
          onClick={onNew}
          size="icon-sm"
          title={t("partnerNavigation.newChat")}
          type="button"
          variant="ghost"
        >
          <Plus />
        </Button>
      </div>
      <div
        className="scrollbar-hidden min-h-0 flex-1 overflow-y-auto overscroll-contain"
        data-chat-history-list
      >
        {children}
      </div>
    </div>
  );
}

export function ChatHistoryNavigationRow({
  active,
  title,
  status,
  href,
  onOpen,
  onPrefetch,
  onAction,
  extraMenu,
}: {
  active: boolean;
  title: string;
  status: "idle" | "running" | "waiting";
  href?: string;
  onOpen: (event: MouseEvent<HTMLElement>) => void;
  onPrefetch?: () => void;
  onAction: (mode: "rename" | "delete") => void;
  extraMenu?: (open: boolean) => ReactNode;
}) {
  const { t } = useTranslation("common");
  const [menuOpen, setMenuOpen] = useState(false);
  const contents = (
    <>
      <span
        aria-hidden="true"
        className={cn(
          "bg-border block size-1.5 shrink-0 place-self-center rounded-full",
          status === "waiting"
            ? "bg-warning"
            : status === "running" && "bg-success animate-pulse",
        )}
      />
      <span className="min-w-0 truncate">{title}</span>
    </>
  );
  const props = {
    onClick: onOpen,
    onFocus: onPrefetch,
    onMouseEnter: onPrefetch,
    title,
    className:
      "focus-visible:ring-ring type-body grid min-w-0 grid-cols-[14px_minmax(0,1fr)] items-center gap-1 overflow-hidden rounded-sm px-1 py-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-inset",
  };
  return (
    <div
      className={cn(
        "group/chat hover:bg-muted min-h-row-compact relative grid w-full min-w-0 grid-cols-[minmax(0,1fr)_32px] items-center overflow-hidden rounded-sm",
        active && "bg-muted text-foreground",
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
        <button
          type="button"
          aria-current={active ? "page" : undefined}
          {...props}
        >
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
          <DropdownMenuItem onSelect={() => onAction("rename")}>
            <Pencil />
            {t("partnerNavigation.renameChat")}
          </DropdownMenuItem>
          <DropdownMenuItem
            className="text-destructive"
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
