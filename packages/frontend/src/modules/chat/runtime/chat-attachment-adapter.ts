import { i18n } from "@/shared/i18n";
import type {
  AttachmentAdapter,
  CompleteAttachment,
  PendingAttachment,
} from "@assistant-ui/react";
import { stageAttachment, discardAttachment } from "@/shared/platform";

export type BotPathFile = File & {
  botSourcePath: string;
  botUploadState?: "staged" | "renew";
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

/** 新提交可以重传原字节，旧提交仍持有原暂存路径用于幂等对账。 */
export function fileForFreshSubmission(file: BotPathFile): BotPathFile {
  if (!file.botUploadState) return file;
  return Object.assign(
    new File([file], file.name, {
      type: file.type,
      lastModified: file.lastModified,
    }),
    { botSourcePath: file.botSourcePath, botUploadState: "renew" as const },
  );
}

export const chatAttachmentAdapter = {
  accept: "*",
  async add({ file }): Promise<PendingAttachment> {
    const attachmentFile = file as BotPathFile;
    if (!attachmentFile.botSourcePath) {
      attachmentFile.botSourcePath = await stageAttachment(file);
      attachmentFile.botUploadState = "staged";
    }
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
    const file = attachment.file as BotPathFile;
    if (file.botUploadState === "renew") {
      file.botSourcePath = await stageAttachment(file);
      file.botUploadState = "staged";
    }
    const sourcePath = file.botSourcePath;
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
    const file = attachment.file as BotPathFile | undefined;
    if (file?.botSourcePath && file.botUploadState !== "renew")
      await discardAttachment(file.botSourcePath);
  },
} satisfies AttachmentAdapter;

export async function renewReplyAttachments(
  attachments: readonly CompleteAttachment[],
) {
  const freshPaths: string[] = [];
  const discard = async () => {
    await Promise.allSettled(freshPaths.map((path) => discardAttachment(path)));
  };
  const results = await Promise.allSettled(
    attachments.map(async (attachment) => {
      const file = attachment.file as BotPathFile | undefined;
      if (!file?.botUploadState) return attachment;
      const fresh = fileForFreshSubmission(file);
      const renewed = await chatAttachmentAdapter.send({
        ...attachment,
        file: fresh,
        status: { type: "requires-action", reason: "composer-send" },
      });
      if (fresh.botSourcePath !== file.botSourcePath)
        freshPaths.push(fresh.botSourcePath);
      return renewed;
    }),
  );
  const renewed: CompleteAttachment[] = [];
  for (const result of results) {
    if (result.status === "rejected") {
      await discard();
      throw result.reason;
    }
    renewed.push(result.value);
  }
  return { attachments: renewed, discard };
}

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
