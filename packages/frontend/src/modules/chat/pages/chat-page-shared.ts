import type { ChatRightPanel } from "../components/chat-right-panel-host";
import type { ReactNode } from "react";

export type ChatOutletContext = {
  chatRightPanel: ChatRightPanel | undefined;
  setChatRightPanel: (panel: ChatRightPanel | undefined) => void;
  renderTeamMembers?: (onMemberOpened: () => void) => ReactNode;
};

export { runtimeDisplayName } from "@/shared/lib/runtime-display-name";
