import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { ResourcePartnerAvatar } from "@/shared/ui/resource-management-card";
import { Button } from "@/shared/ui/button";
import {
  ResourceCatalogRow,
  ResourceCatalogRows,
} from "@/shared/ui/resource-catalog";
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
          <ResourceCatalogRows>
            {detail.installations.map((item) => {
              const name =
                detail.partnerNames?.[item.wakerId ?? ""] || t("unknown");
              return (
                <ResourceCatalogRow key={item.wakerId}>
                  <Link
                    className="focus-visible:ring-ring flex min-w-0 flex-1 items-center gap-4 rounded-xs outline-none focus-visible:ring-2"
                    aria-label={name}
                    to={`/wakers/${item.wakerId}/resources`}
                  >
                    <ResourcePartnerAvatar
                      name={name}
                      avatar={detail.partnerAvatars?.[item.wakerId ?? ""]}
                      size="md"
                    />
                    <div className="min-w-0 flex-1 space-y-1">
                      <h3
                        className="type-resource-body text-resource-content truncate font-medium"
                        title={name}
                      >
                        {name}
                      </h3>
                      <p className="type-resource-caption text-resource-muted">
                        {t(item.enabled ? "enabled" : "disabled")}
                      </p>
                    </div>
                  </Link>
                  {onUninstall && (
                    <Button
                      size="catalog"
                      variant="catalog-danger"
                      aria-label={t("uninstall")}
                      title={t("uninstall")}
                      disabled={uninstalling}
                      onClick={() => onUninstall(item.wakerId ?? "")}
                    >
                      <Trash2 />
                      {t("uninstallAction")}
                    </Button>
                  )}
                </ResourceCatalogRow>
              );
            })}
          </ResourceCatalogRows>
        ) : (
          <p className="type-body text-content-caption">{t("notInstalled")}</p>
        ))}
    </>
  );
}
