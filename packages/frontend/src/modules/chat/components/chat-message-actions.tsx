import { CircleDot, Copy, Ellipsis, History, Trash2 } from "lucide-react";
import { useRef, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { toast } from "@/shared/ui/sonner";

export function ChatMessageActions({
  copyText,
  onOpenWorkRecord,
  forkItem,
  onMarkUnread,
  onDelete,
}: {
  copyText?: string;
  onOpenWorkRecord?: (trigger: HTMLButtonElement) => void;
  forkItem?: ReactNode;
  onMarkUnread?: () => void;
  onDelete?: (trigger: HTMLButtonElement) => void;
}) {
  const { t } = useTranslation("chat");
  const trigger = useRef<HTMLButtonElement>(null);
  const focusMoved = useRef(false);
  const handOff = (action: (button: HTMLButtonElement) => void) => {
    if (!trigger.current) return;
    focusMoved.current = true;
    action(trigger.current);
  };
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger ref={trigger} asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          title={t("messageActions.more")}
          aria-label={t("messageActions.more")}
        >
          <Ellipsis aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        onCloseAutoFocus={(event) => {
          if (focusMoved.current) event.preventDefault();
          focusMoved.current = false;
        }}
      >
        {onOpenWorkRecord && (
          <DropdownMenuItem onSelect={() => handOff(onOpenWorkRecord)}>
            <History aria-hidden="true" />
            {t("openWorkRecord")}
          </DropdownMenuItem>
        )}
        {copyText !== undefined && (
          <DropdownMenuItem
            onSelect={async () => {
              try {
                await navigator.clipboard.writeText(copyText);
              } catch {
                toast.error(t("messageActions.copyFailed"));
              }
            }}
          >
            <Copy aria-hidden="true" />
            {t("messageActions.copy")}
          </DropdownMenuItem>
        )}
        {forkItem}
        {onMarkUnread && (
          <DropdownMenuItem onSelect={onMarkUnread}>
            <CircleDot aria-hidden="true" />
            {t("reading.markUnread")}
          </DropdownMenuItem>
        )}
        {onDelete && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant="destructive"
              onSelect={() => handOff(onDelete)}
            >
              <Trash2 aria-hidden="true" />
              {t("common:delete")}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
