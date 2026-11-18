import { useTranslation } from "react-i18next";
import { Skeleton } from "@/shared/ui/skeleton";
import { ChatMessageColumn } from "./chat-thread";

/** 历史只阻塞消息区；输入框与工作台保持挂载。 */
export function ChatHistoryLoadingState() {
  const { t } = useTranslation("chat");
  return (
    <div role="status" aria-busy="true" aria-label={t("loadingMessages")}>
      <ChatMessageColumn>
        <div className="space-y-4 px-6">
          <Skeleton className="h-control-md ml-auto w-1/3 rounded-xl" />
          <div className="flex items-start gap-3">
            <Skeleton className="size-avatar-sm shrink-0 rounded-full" />
            <div className="bg-message-card-background w-full max-w-md min-w-0 space-y-2 rounded-xl px-4 py-3">
              <Skeleton className="h-3 w-full" />
              <Skeleton className="h-3 w-3/4" />
              <Skeleton className="h-3 w-1/2" />
            </div>
          </div>
        </div>
      </ChatMessageColumn>
    </div>
  );
}
