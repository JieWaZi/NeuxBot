import { Navigate, useSearchParams } from "react-router-dom";
import { chatEntryHref } from "@/shared/lib/chat-navigation";

/** 旧伙伴和团队目录统一回到聊天工作台，由侧栏管理身份。 */
export function PartnerManagementPage() {
  const [params] = useSearchParams();
  return (
    <Navigate to={chatEntryHref(params.get("wakerId") ?? undefined)} replace />
  );
}
