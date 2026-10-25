import { TeamCollection, TeamFormDialog } from "@/modules/team";
import { WakerListPage } from "@/modules/waker/pages/waker-list-page";

export function PartnerManagementPage() {
  return (
    <WakerListPage
      renderTeamCollection={() => <TeamCollection />}
      renderTeamDialog={(props) => <TeamFormDialog {...props} />}
    />
  );
}
