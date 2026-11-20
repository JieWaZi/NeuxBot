import { i18n } from "@/shared/i18n";
import type {
  AttachmentAdapter,
  CompleteAttachment,
  PendingAttachment,
} from "@assistant-ui/react";
import { stageAttachment, discardAttachment } from "@/shared/platform";

export type BotPathFile = File & {
  botSourcePath: string;
};

function mediaTypeOf(path: string): string {
  const extension = path.split(".").pop()?.toLowerCase();
  if (["png", "jpg", "jpeg", "gif", "webp"].includes(extension ?? "")) {
    return extension === "jpg" || extension === "jpeg"
      ? "image/jpeg"
      : `image/${extension}`;
  }
  return "application/octet-stream";
}

export function pathFile(
  path: string,
  metadata?: { name: string; contentType?: string },
): BotPathFile {
  const name = metadata?.name || path.split(/[\\/]/).pop() || "attachment";
  return Object.assign(
    new File([], name, { type: metadata?.contentType ?? mediaTypeOf(path) }),
    {
      botSourcePath: path,
    },
  );
}

export const chatAttachmentAdapter = {
  accept: "*",
  async add({ file }): Promise<PendingAttachment> {
    const sourcePath =
      (file as BotPathFile).botSourcePath || (await stageAttachment(file));
    const attachmentFile = Object.assign(file, { botSourcePath: sourcePath });
    return {
      id: crypto.randomUUID(),
      type: file.type.startsWith("image/") ? "image" : "file",
      name: file.name,
      contentType: file.type,
      file: attachmentFile,
      status: { type: "requires-action", reason: "composer-send" },
    };
  },
  async send(attachment: PendingAttachment): Promise<CompleteAttachment> {
    const sourcePath = (attachment.file as BotPathFile).botSourcePath;
    if (!sourcePath)
      throw new Error(i18n.t("common:errors.attachmentUnavailable"));
    return {
      ...attachment,
      status: { type: "complete" },
      content: [
        {
          type: "file",
          filename: attachment.name,
          data: sourcePath,
          mimeType: attachment.contentType || "application/octet-stream",
        },
      ],
    };
  },
  async remove(attachment): Promise<void> {
    const sourcePath = (attachment.file as BotPathFile | undefined)
      ?.botSourcePath;
    if (sourcePath) await discardAttachment(sourcePath);
  },
} satisfies AttachmentAdapter;

export function attachmentPathsOf(message: AppendMessageLike): string[] {
  return (message.attachments ?? []).flatMap((attachment) =>
    (attachment.content ?? [])
      .filter((part) => part.type === "file")
      .map((part) => part.data),
  );
}

type AppendMessageLike = {
  attachments?: readonly CompleteAttachment[];
};
