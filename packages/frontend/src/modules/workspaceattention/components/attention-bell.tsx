import { Bell } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";

export function AttentionBell({
  hasAction,
  hasUnread,
  synced,
  onOpen,
  active = false,
}: {
  hasAction: boolean;
  hasUnread: boolean;
  synced: boolean;
  onOpen: () => void;
  active?: boolean;
}) {
  const { t } = useTranslation("notification");
  const state = t(
    hasAction && hasUnread
      ? "actionAndUnread"
      : hasAction
        ? "action"
        : hasUnread
          ? "unread"
          : "none",
  );
  const label = synced ? state : t("notSynced", { state });
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          size="icon"
          shape="capsule"
          variant="ghost"
          aria-label={label}
          aria-current={active ? "page" : undefined}
          data-desktop-no-drag-region
          className="relative"
          onClick={() => onOpen()}
        >
          <Bell strokeWidth={1.5} aria-hidden="true" />
          {hasAction || hasUnread ? (
            <span
              data-attention-dot
              aria-hidden="true"
              className="bg-accent absolute end-1.5 top-1.5 size-1.5 rounded-full"
            />
          ) : null}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
