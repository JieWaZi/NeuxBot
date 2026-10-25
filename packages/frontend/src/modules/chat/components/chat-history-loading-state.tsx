import { useTranslation } from "react-i18next";
import { ContentLoadingState } from "@/shared/ui/content-loading-state";

/** 历史只阻塞消息区；输入框与工作台保持挂载。 */
export function ChatHistoryLoadingState() {
  const { t } = useTranslation("chat");
  return (
    <ContentLoadingState
      label={t("loadingMessages")}
      className="h-full w-full"
    />
  );
}
