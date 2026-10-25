import { useTranslation } from "react-i18next";

import type { PaginationLabels } from "@/shared/ui/pagination";

export function useKnowledgeSourcePaginationLabels(): PaginationLabels {
  const { t } = useTranslation("knowledge");
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
