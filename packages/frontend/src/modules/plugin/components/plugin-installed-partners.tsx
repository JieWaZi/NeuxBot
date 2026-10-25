import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { ResourcePartnerAvatar } from "@/shared/ui/resource-management-card";
import { Badge } from "@/shared/ui/badge";
import { ResourceItemAction } from "@/shared/ui/resource-list-item";
import type { PluginDetail } from "../model/plugin-view";

export function PluginInstalledPartners({
  detail,
  installationStatus,
  onUninstall,
  uninstalling,
}: {
  detail?: PluginDetail;
  installationStatus?: ReactNode;
  onUninstall?: (id: string) => void;
  uninstalling?: boolean;
}) {
  const { t } = useTranslation("plugin");
  return (
    <>
      {installationStatus ??
        (detail?.installations?.length ? (
          <ul className="grid grid-cols-1 gap-3 @[36rem]/resource-page:grid-cols-2 @[56rem]/resource-page:grid-cols-3">
            {detail.installations.map((item) => (
              <li
                key={item.wakerId}
                className="bg-surface-subtle flex min-w-0 items-center gap-3 rounded-lg p-3"
              >
                <ResourcePartnerAvatar
                  name={
                    detail.partnerNames?.[item.wakerId ?? ""] || t("unknown")
                  }
                  avatar={detail.partnerAvatars?.[item.wakerId ?? ""]}
                  size="md"
                />
                <Link
                  className="type-body focus-visible:ring-ring min-w-0 flex-1 rounded-xs outline-none focus-visible:ring-2"
                  to={`/wakers/${item.wakerId}/resources`}
                >
                  <span className="block truncate">
                    {detail.partnerNames?.[item.wakerId ?? ""] || t("unknown")}
                  </span>
                  <Badge
                    className="mt-1"
                    variant={item.enabled ? "success" : "destructive"}
                  >
                    {t(item.enabled ? "enabled" : "disabled")}
                  </Badge>
                </Link>
                {onUninstall && (
                  <ResourceItemAction
                    variant="danger-quiet"
                    label={t("uninstall")}
                    disabled={uninstalling}
                    onClick={() => onUninstall(item.wakerId ?? "")}
                  >
                    <Trash2 />
                  </ResourceItemAction>
                )}
              </li>
            ))}
          </ul>
        ) : (
          <p className="type-body text-content-caption">{t("notInstalled")}</p>
        ))}
    </>
  );
}
