import { isKnowledgeWriteOutcomeUnknown } from "./knowledge-write-outcome";

export type KnowledgeUploadEntry = {
  id: string;
  file: File;
  fingerprint?: string;
  state: "ready" | "uploading" | "done" | "failed" | "cancelled" | "duplicate";
  uncertain?: boolean;
  acceptedMode?: "separate" | "automatic";
  error?: string;
};
/** 仅保存同库的客户端上传意图，弹窗关闭不丢失不可变输入与幂等身份。 */
export type KnowledgeUploadQueue = {
  getEntries: () => KnowledgeUploadEntry[];
  update: (
    change: (entries: KnowledgeUploadEntry[]) => KnowledgeUploadEntry[],
  ) => KnowledgeUploadEntry[];
};
export function createKnowledgeUploadQueue(): KnowledgeUploadQueue {
  let entries: KnowledgeUploadEntry[] = [];
  return {
    getEntries: () => entries,
    update: (change) => {
      entries = change(entries);
      return entries;
    },
  };
}

/** 请求体中的文件名、媒体类型与实际字节共同决定身份，不使用文件时间等旁证。 */
export async function uploadFingerprint(file: File) {
  const bytes =
    typeof file.arrayBuffer === "function"
      ? await file.arrayBuffer()
      : await new Promise<ArrayBuffer>((resolve, reject) => {
          const reader = new FileReader();
          reader.onerror = () =>
            reject(reader.error ?? new Error("source_read_failed"));
          reader.onload = () =>
            reader.result instanceof ArrayBuffer
              ? resolve(reader.result)
              : reject(new Error("source_read_failed"));
          reader.readAsArrayBuffer(file);
        });
  const hash = await crypto.subtle.digest("SHA-256", bytes);
  return JSON.stringify([
    file.name,
    file.type,
    [...new Uint8Array(hash)]
      .map((byte) => byte.toString(16).padStart(2, "0"))
      .join(""),
  ]);
}

export function uploadOutcomeUnknown(cause: unknown, aborted: boolean) {
  return aborted || isKnowledgeWriteOutcomeUnknown(cause);
}
