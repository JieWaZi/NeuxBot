import { TeamCollection, TeamFormDialog } from "@/modules/team";
import { WakerListPage } from "@/modules/waker/pages/waker-list-page";
import { Navigate, useSearchParams } from "react-router-dom";
import { chatEntryHref } from "@/shared/lib/chat-navigation";

/** 旧伙伴目录回到聊天；团队目录保留显式入口。 */
export function PartnerManagementPage() {
  const [params] = useSearchParams();
  if (params.get("tab") !== "teams")
    return (
      <Navigate
        to={chatEntryHref(params.get("wakerId") ?? undefined)}
        replace
      />
    );
  return (
    <WakerListPage
      renderTeamCollection={() => <TeamCollection />}
      renderTeamDialog={(props) => <TeamFormDialog {...props} />}
    />
  );
}
