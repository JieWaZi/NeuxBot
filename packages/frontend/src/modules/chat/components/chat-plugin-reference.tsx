import { PluginIcon, pluginDetailHref } from "@/modules/plugin";
import {
  ChatMessageReferenceChip,
  ChatReferenceChipList,
} from "./chat-reference-chip";
import type { ChatSkillOption } from "./chat-skill-reference";

export type ChatPluginIcon = {
  icon?: string;
  icons?: { default?: string; dark?: string };
};
export type ChatPluginOption = ChatSkillOption & ChatPluginIcon;
export type ChatPluginIcons = Readonly<Record<string, ChatPluginIcon | null>>;

export function chatPluginReferences(values: unknown) {
  if (!Array.isArray(values)) return [];
  return values.flatMap((value: unknown) =>
    value &&
    typeof value === "object" &&
    "id" in value &&
    "name" in value &&
    typeof value.id === "string" &&
    typeof value.name === "string"
      ? [{ id: value.id, name: value.name }]
      : [],
  );
}

/** 名称来自消息快照，图标来自插件展示资料。 */
export function ChatMessagePluginReferences({
  plugins,
  pluginIcons,
}: {
  plugins: unknown;
  pluginIcons?: ChatPluginIcons;
}) {
  return (
    <ChatReferenceChipList>
      {chatPluginReferences(plugins).map(({ id, name }) => (
        <ChatMessageReferenceChip
          key={id}
          tone="plugin"
          label={name}
          icon={<PluginIcon {...pluginIcons?.[id]} size="mention" />}
          href={pluginIcons?.[id] === null ? undefined : pluginDetailHref(id)}
        />
      ))}
    </ChatReferenceChipList>
  );
}
