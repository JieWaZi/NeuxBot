import { useSearchParams } from "react-router-dom";
import { IntegrationBreadcrumbs } from "@/modules/settings/components/integration-breadcrumbs";
import { IMConnectionDetailPage, IMPage, useIMConnections } from "@/modules/im";

/** 连接深链由同一 Scene 的三级页面承载。 */
export function IMIntegrationSettingsContent() {
  const [params, setParams] = useSearchParams();
  const connectionId = params.get("connection");
  return connectionId ? (
    <IMConnectionDetailPage
      connectionId={connectionId}
      initialBindingId={
        params.get("binding") || params.get("chat") || undefined
      }
      onBack={(platform) => {
        const next = new URLSearchParams(params);
        for (const key of ["connection", "binding", "chat"]) next.delete(key);
        if (platform) next.set("platform", platform);
        setParams(next);
      }}
    />
  ) : (
    <IMPage />
  );
}

export function IMIntegrationBreadcrumbs() {
  const [params] = useSearchParams();
  return params.has("connection") ? (
    <ConnectionBreadcrumbs />
  ) : (
    <IntegrationBreadcrumbs />
  );
}

function ConnectionBreadcrumbs() {
  const [params] = useSearchParams();
  const connections = useIMConnections();
  return (
    <IntegrationBreadcrumbs
      connection={connections.data?.resources?.find(
        (item) => item.id === params.get("connection"),
      )}
    />
  );
}
