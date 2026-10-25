import { useSearchParams } from "react-router-dom";

import { IMConnectionDetailDialog, IMPage } from "@/modules/im";

/** 设置与 IM 的跨领域装配；旧链接定位使用 URL，普通编辑由 IM 列表管理。 */
export function IMIntegrationSettingsContent() {
  const [params, setParams] = useSearchParams();
  const connectionId = params.get("connection");
  const initialBindingId =
    params.get("binding") || params.get("chat") || undefined;
  return (
    <>
      <IMPage />
      {connectionId ? (
        <IMConnectionDetailDialog
          key={`${connectionId}:${initialBindingId || ""}`}
          connectionId={connectionId}
          initialBindingId={initialBindingId}
          onClose={() => {
            const next = new URLSearchParams(params);
            next.delete("connection");
            next.delete("binding");
            next.delete("chat");
            setParams(next, { replace: true });
          }}
        />
      ) : null}
    </>
  );
}
