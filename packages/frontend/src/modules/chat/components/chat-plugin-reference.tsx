import { useTranslation } from "react-i18next";
import {
  ChatMessageReferenceChip,
  ChatReferenceChipList,
} from "./chat-reference-chip";

/** 历史使用消息自身快照；当前安装只决定是否还能进入详情。 */
export function ChatMessagePluginReferences({
  plugins,
  wakerId,
  availablePluginIds,
}: {
  plugins: unknown;
  wakerId: string;
  availablePluginIds?: ReadonlySet<string>;
}) {
  const { t } = useTranslation("plugin");
  if (!Array.isArray(plugins)) return null;
  return (
    <ChatReferenceChipList>
      {plugins.flatMap((value) => {
        if (
          !value ||
          typeof value !== "object" ||
          typeof value.id !== "string" ||
          typeof value.name !== "string"
        )
          return [];
        const version = typeof value.version === "string" ? value.version : "";
        const unavailable = availablePluginIds
          ? !availablePluginIds.has(value.id)
          : false;
        return [
          <ChatMessageReferenceChip
            key={value.id}
            tone="plugin"
            label={[value.name, version].filter(Boolean).join(" · ")}
            unavailable={unavailable}
            unavailableTitle={t("notInstalled")}
            linkAriaLabel={t("details")}
            href={
              unavailable
                ? undefined
                : wakerId
                  ? `/wakers/${wakerId}/plugins/${value.id}`
                  : `/resources/plugins/${value.id}`
            }
          />,
        ];
      })}
    </ChatReferenceChipList>
  );
}
