import { useTranslation } from "react-i18next";

import {
  ChatMessageReferenceChip,
  ChatReferenceChipList,
} from "./chat-reference-chip";

export type ChatConnectorOption = {
  id: string;
  name: string;
  description: string;
};

type ChatMessageConnectorReference = {
  id: string;
  name: string;
};

export function ChatMessageConnectorReferences({
  connectors,
  wakerId,
  availableConnectorIds,
}: {
  connectors: ChatMessageConnectorReference[];
  wakerId: string;
  availableConnectorIds?: ReadonlySet<string>;
}) {
  const { t } = useTranslation("chat");
  if (connectors.length === 0) return null;

  return (
    <ChatReferenceChipList>
      {connectors.map((connector) => {
        const unavailable = availableConnectorIds
          ? !availableConnectorIds.has(connector.id)
          : false;
        return (
          <ChatMessageReferenceChip
            key={connector.id}
            label={connector.name}
            tone="connector"
            unavailable={unavailable || !wakerId}
            unavailableTitle={t("unavailableConnectorSnapshot")}
            href={
              unavailable || !wakerId
                ? undefined
                : `/wakers/${wakerId}/connectors/${connector.id}`
            }
            linkAriaLabel={t("viewConnector", { name: connector.name })}
          />
        );
      })}
    </ChatReferenceChipList>
  );
}
