import { useTranslation } from "react-i18next";
import { SettingsNavigationRow } from "@/modules/settings";
import type { IMBinding, IMConnection } from "../types";
import { ConnectionStatus } from "./connection-status";

export function ConnectionSettingsRow({
  connection,
  bindings,
  onView,
}: {
  connection: IMConnection;
  bindings: IMBinding[];
  onView: () => void;
}) {
  const { t } = useTranslation("im");
  return (
    <SettingsNavigationRow
      title={connection.name}
      ariaLabel={connection.name}
      description={connection.appId || t("notConnected")}
      onClick={onView}
      trailing={
        <div className="flex flex-wrap items-center gap-2">
          <ConnectionStatus connection={connection} appearance="inline" />
          {bindings.some((item) => item.state !== "approved") ? (
            <span className="type-caption text-warning">
              {t("pendingAuthorization")}
            </span>
          ) : null}
        </div>
      }
    />
  );
}
