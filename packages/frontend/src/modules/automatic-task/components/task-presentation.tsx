import { useTranslation } from "react-i18next";
import { Badge } from "@/shared/ui/badge";
import type { PaginationLabels } from "@/shared/ui/pagination";

export { formatTime, scheduleSummary } from "@/shared/lib/automatic-task-schedule";

export function RunState({ state }: { state?: string }) {
  const { t } = useTranslation("automaticTask");
  if (!state) return <>—</>;
  const variant =
    state === "completed"
      ? "success"
      : state === "failed" || state === "interrupted"
        ? "destructive"
        : state.startsWith("waiting_")
          ? "warning"
          : "secondary";
  return <Badge variant={variant}>{t(`state.${state}`)}</Badge>;
}

export function useTaskPaginationLabels(): PaginationLabels {
  const { t } = useTranslation("automaticTask");
  return {
    navigation: t("pagination.navigation"),
    next: t("pagination.next"),
    previous: t("pagination.previous"),
    page: (page) => t("pagination.page", { page }),
    pageSize: (count) => t("pagination.pageSize", { count }),
    pageSizeSelection: t("pagination.pageSizeSelection"),
    total: (count) => t("pagination.total", { count }),
  };
}
