import type {
  AttachmentAdapter,
  CompleteAttachment,
  PendingAttachment,
} from "@assistant-ui/react";
import { stageAttachment, discardAttachment } from "@/shared/platform";

export type AllyPathFile = File & {
  allySourcePath: string;
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

export function pathFile(path: string): AllyPathFile {
  const name = path.split(/[\\/]/).pop() || "attachment";
  return Object.assign(new File([], name, { type: mediaTypeOf(path) }), {
    allySourcePath: path,
  });
}

export const chatAttachmentAdapter = {
  accept: "*",
  async add({ file }): Promise<PendingAttachment> {
    const sourcePath =
      (file as AllyPathFile).allySourcePath || (await stageAttachment(file));
    const attachmentFile = Object.assign(file, { allySourcePath: sourcePath });
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
    const sourcePath = (attachment.file as AllyPathFile).allySourcePath;
    if (!sourcePath) throw new Error("附件路径不可用");
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
    const sourcePath = (attachment.file as AllyPathFile | undefined)
      ?.allySourcePath;
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
