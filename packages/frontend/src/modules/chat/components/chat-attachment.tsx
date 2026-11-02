import {
  fileDirectory,
  isAbsoluteFilePath,
  type ResolvedFileReference,
} from "@/shared/workspace/file-reference";
import {
  AttachmentPrimitive,
  ComposerPrimitive,
  useAui,
  useAuiState,
} from "@assistant-ui/react";
import { X } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { AllyChatMessage } from "../runtime/chat-projection";
import type { AllyPathFile } from "../runtime/chat-attachment-adapter";
import {
  chatComposerReferenceChipClass,
  chatMessageReferenceChipClass,
} from "./chat-reference-chip";

export type ChatFileOpenRequest = {
  reference?: ResolvedFileReference;
  name: string;
  path: string;
  rootPath?: string;
};

function sourcePathPreviewOf(
  sourcePath: string,
  name: string,
): ChatFileOpenRequest {
  return {
    name,
    path: sourcePath,
    rootPath: isAbsoluteFilePath(sourcePath)
      ? fileDirectory(sourcePath)
      : undefined,
  };
}

function ChatComposerAttachmentChip({
  name,
  sourcePath,
  onOpenFile,
  onRemove,
}: {
  name: string;
  sourcePath: string;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
  onRemove?: (path: string) => Promise<void> | void;
}) {
  const { t } = useTranslation("chat");
  const aui = useAui();
  const preview = sourcePathPreviewOf(sourcePath, name);
  return (
    <AttachmentPrimitive.Root
      data-chat-composer-attachment={sourcePath || undefined}
      className={`${chatComposerReferenceChipClass}${onOpenFile ? "cursor-pointer" : ""}`}
      role={onOpenFile ? "button" : undefined}
      tabIndex={onOpenFile ? 0 : undefined}
      onClick={onOpenFile ? () => onOpenFile(preview) : undefined}
      onKeyDown={
        onOpenFile
          ? (event) => {
              if (event.key !== "Enter" && event.key !== " ") return;
              event.preventDefault();
              onOpenFile(preview);
            }
          : undefined
      }
    >
      <span className="max-w-40 truncate">
        <AttachmentPrimitive.Name />
      </span>
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground size-icon-sm ml-1 inline-flex shrink-0 items-center justify-center rounded-full transition-colors"
        aria-label={t("removeAttachment")}
        title={t("removeAttachment")}
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          void (onRemove ? onRemove(sourcePath) : aui.attachment.remove());
        }}
      >
        <X className="size-icon-xs" />
      </button>
    </AttachmentPrimitive.Root>
  );
}

export function ChatComposerAttachments({
  onOpenFile,
  onRemove,
}: {
  onOpenFile?: (file: ChatFileOpenRequest) => void;
  onRemove?: (path: string) => Promise<void> | void;
}) {
  const attachments = useAuiState((state) => state.composer.attachments);
  return (
    <>
      {attachments.map((attachment, index) => (
        <ComposerPrimitive.AttachmentByIndex
          key={attachment.id}
          index={index}
          components={{
            Attachment: () => (
              <ChatComposerAttachmentChip
                name={attachment.name}
                sourcePath={
                  (attachment.file as AllyPathFile | undefined)
                    ?.allySourcePath ?? ""
                }
                onOpenFile={onOpenFile}
                onRemove={onRemove}
              />
            ),
          }}
        />
      ))}
    </>
  );
}

/** 消息附件直接读取接纳时的引用；composer 仍复用 assistant-ui 附件适配器。 */
export function ChatMessageAttachments({
  attachments = [],
  onOpenFile,
}: {
  attachments?: NonNullable<AllyChatMessage["attachments"]>;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
}) {
  return (
    <>
      {attachments.map((attachment, index) => {
        const path = attachment.path ?? "";
        const name = attachment.name ?? path;
        return (
          <span
            key={`${path}:${index}`}
            className={chatMessageReferenceChipClass}
          >
            {onOpenFile && path ? (
              <button
                type="button"
                className="max-w-40 truncate text-left"
                onClick={() => onOpenFile({ name, path })}
              >
                {name}
              </button>
            ) : (
              <span className="max-w-40 truncate">{name}</span>
            )}
          </span>
        );
      })}
    </>
  );
}
