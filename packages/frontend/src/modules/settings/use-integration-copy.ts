import { useTranslation } from "react-i18next";
import { i18n } from "@/shared/i18n";
import { integrationZh, integrationEn } from "@/shared/i18n/integration";

i18n.addResourceBundle("zh-CN", "integration", integrationZh, true, true);
i18n.addResourceBundle("en-US", "integration", integrationEn, true, true);

export function useIntegrationCopy() {
  return useTranslation("integration").t;
}
