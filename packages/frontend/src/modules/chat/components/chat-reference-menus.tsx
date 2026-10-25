import {
  ChatSkillReferenceMenu,
  type ChatSkillOption,
} from "./chat-skill-reference";

export type { ChatSkillOption };

type ChatReferenceMenusProps = {
  skills: ChatSkillOption[];
  selectedSkillIds: string[];
  onSkillSelectionChange: (skillIds: string[]) => void;
  skillsError?: boolean;
};

export function ChatReferenceMenus({
  skills,
  selectedSkillIds,
  onSkillSelectionChange,
  skillsError = false,
}: ChatReferenceMenusProps) {
  return (
    <>
      <ChatSkillReferenceMenu
        skills={skills}
        selectedIds={selectedSkillIds}
        onSelectionChange={onSkillSelectionChange}
        skillsError={skillsError}
      />
    </>
  );
}

export {
  ChatComposerAttachments,
  ChatMessageAttachments,
} from "./chat-attachment";
export type { ChatFileOpenRequest } from "./chat-attachment";
export {
  ChatComposerSkillReferences,
  ChatMessageSkillReferences,
  ChatSkillReferenceMenu,
} from "./chat-skill-reference";
export {
  ChatComposerReferenceChip,
  ChatMessageReferenceChip,
  chatComposerReferenceChipClass,
  chatMessageReferenceChipClass,
} from "./chat-reference-chip";
export {
  ChatReferencePopover,
  ChatReferencePopoverItem,
  ChatReferencePopoverList,
  ChatReferencePopoverMessage,
  chatReferencePopoverClass,
} from "./chat-reference-popover";
