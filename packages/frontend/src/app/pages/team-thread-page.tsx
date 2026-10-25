import { useOutletContext } from "react-router-dom";

import { TeamChatPageContent } from "@/modules/chat/pages/team-chat-page";
import { TeamThreadPage as TeamThreadController } from "@/modules/team/pages/team-thread-page";

export function TeamThreadPage() {
  const outlet = useOutletContext<{
    onTeamThreadDeleted?: (channelId: string, threadId: string) => void;
  } | null>();
  return (
    <TeamThreadController
      onThreadMissing={outlet?.onTeamThreadDeleted}
      renderChat={(props) => <TeamChatPageContent {...props} />}
    />
  );
}
