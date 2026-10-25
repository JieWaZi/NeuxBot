import { unstable_useSlashCommandAdapter, useAui } from "@assistant-ui/react";
import { Check, Sparkles } from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  ChatComposerReferenceChip,
  ChatMessageReferenceChip,
  ChatReferenceChipList,
} from "./chat-reference-chip";
import {
  ChatReferencePopover,
  ChatReferencePopoverItem,
  ChatReferencePopoverList,
  ChatReferencePopoverMessage,
  useReferencePopoverScrollRef,
} from "./chat-reference-popover";

export type ChatSkillOption = {
  id: string;
  name: string;
  description: string;
  version?: string;
};

type ChatSkillReferenceMenuProps = {
  skills: ChatSkillOption[];
  selectedIds: string[];
  onSelectionChange: (skillIds: string[]) => void;
  skillsError?: boolean;
};

function stripLeadingSlashCommand(text: string) {
  return text.replace(/^\/[^\s\n]*/, "");
}

export function ChatSkillReferenceMenu({
  skills,
  selectedIds,
  onSelectionChange,
  skillsError = false,
}: ChatSkillReferenceMenuProps) {
  const { t } = useTranslation("chat");
  const aui = useAui();
  const scrollContainerRef = useReferencePopoverScrollRef();
  const selected = useMemo(() => new Set(selectedIds), [selectedIds]);
  const slash = unstable_useSlashCommandAdapter({
    commands: skills.map((skill) => ({
      id: skill.id,
      label: skill.name,
      description: skill.description,
      execute: () => {
        if (!selected.has(skill.id)) {
          onSelectionChange([...selectedIds, skill.id]);
        }
        const text = aui.composer.getState().text;
        const cleaned = stripLeadingSlashCommand(text);
        if (cleaned !== text) {
          aui.composer.setText(cleaned);
        }
      },
    })),
    removeOnExecute: true,
  });

  return (
    <ChatReferencePopover
      char="/"
      adapter={slash.adapter}
      action={slash.action}
      title={t("skills")}
      ariaLabel={t("skills")}
      scrollContainerRef={scrollContainerRef}
    >
      {(items) =>
        skillsError ? (
          <ChatReferencePopoverMessage tone="destructive">
            {t("skillsLoadError")}
          </ChatReferencePopoverMessage>
        ) : items.length > 0 ? (
          <ChatReferencePopoverList>
            {items.map((item, index) => (
              <ChatReferencePopoverItem
                key={item.id}
                item={item}
                index={index}
                columns="three"
                icon={
                  <Sparkles
                    className="text-muted-foreground size-icon-sm"
                    strokeWidth={1.5}
                  />
                }
                trailing={
                  selected.has(item.id) ? (
                    <Check className="text-primary size-icon-sm" />
                  ) : null
                }
              />
            ))}
          </ChatReferencePopoverList>
        ) : (
          <ChatReferencePopoverMessage>
            {skills.length ? t("noMatchingSkills") : t("noEnabledSkills")}
          </ChatReferencePopoverMessage>
        )
      }
    </ChatReferencePopover>
  );
}

export function ChatComposerSkillReferences({
  skills,
  selectedIds,
}: {
  skills: ChatSkillOption[];
  selectedIds: string[];
}) {
  const selected = new Set(selectedIds);
  const selectedSkills = skills.filter((skill) => selected.has(skill.id));
  if (selectedSkills.length === 0) return null;

  return (
    <ChatReferenceChipList>
      {selectedSkills.map((skill) => (
        <ChatComposerReferenceChip
          key={skill.id}
          label={skill.name}
          marker={{
            name: "data-chat-composer-skill",
            value: skill.id,
          }}
        />
      ))}
    </ChatReferenceChipList>
  );
}

type ChatMessageSkillReference = {
  id: string;
  name: string;
};

export function ChatMessageSkillReferences({
  skills,
  wakerId,
  availableSkillIds,
}: {
  skills: ChatMessageSkillReference[];
  wakerId: string;
  availableSkillIds?: ReadonlySet<string>;
}) {
  const { t } = useTranslation("chat");
  if (skills.length === 0) return null;

  return (
    <ChatReferenceChipList>
      {skills.map((skill) => {
        const unavailable = availableSkillIds
          ? !availableSkillIds.has(skill.id)
          : false;
        return (
          <ChatMessageReferenceChip
            key={skill.id}
            label={skill.name}
            unavailable={unavailable || !wakerId}
            unavailableTitle={t("unavailableSkillSnapshot")}
            href={
              unavailable || !wakerId
                ? undefined
                : `/wakers/${wakerId}/skills/${skill.id}`
            }
            linkAriaLabel={t("viewSkill", { name: skill.name })}
          />
        );
      })}
    </ChatReferenceChipList>
  );
}
