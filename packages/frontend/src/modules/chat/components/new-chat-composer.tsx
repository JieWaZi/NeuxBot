import type { ReactNode } from "react";
import type { Waker } from "@/modules/waker";
import { ChatPageContent } from "../pages/chat-page";

export function NewChatComposer({
  scopeKey,
  ...draft
}: {
  scopeKey: string;
  partner?: Waker;
  active: boolean;
  renderPartnerPicker: (sending: boolean) => ReactNode;
}) {
  return (
    <ChatPageContent
      boardDraft={draft}
      presentation="composer"
      presentationScopeKey={scopeKey}
    />
  );
}
