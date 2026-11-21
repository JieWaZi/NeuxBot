import { useTranslation } from "react-i18next";
import { TeamPartnerButton } from "./team-partner-button";
import { ChatTimelineMarker } from "./chat-timeline-marker";

type DelegationPartner = {
  id: string;
  name: string;
  avatar?: string;
  available: boolean;
  onOpen: () => void;
};

/** 同一回合的伙伴入口共用一条委派行，跳转仍由所属团队工作区管理。 */
export function TeamDelegationMarker({
  direction,
  partners,
}: {
  direction: "incoming" | "outgoing";
  partners: readonly DelegationPartner[];
}) {
  const { t } = useTranslation("chat");
  return (
    <ChatTimelineMarker>
      <span>
        {t(
          direction === "outgoing" ? "team.delegatedTo" : "team.delegatedFrom",
        )}
      </span>
      {partners.map((partner) => (
        <TeamPartnerButton
          key={partner.id}
          name={partner.name}
          avatar={partner.avatar}
          disabled={!partner.available}
          aria-label={t("team.openDelegationChat", { name: partner.name })}
          onClick={partner.onOpen}
        />
      ))}
    </ChatTimelineMarker>
  );
}
