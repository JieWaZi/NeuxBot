import {
  knowledgeAdapter,
  type ResourceRef,
  type KnowledgeUploadContext,
} from "@/shared/api/knowledge";

/** 原件大小先校验，再读取实际字节；请求身份由调用方保留以处理未知结果。 */
export async function encodeUpload(
  file: Pick<File, "name" | "type" | "size" | "arrayBuffer">,
  requestId: string,
): Promise<{
  requestId: string;
  filename: string;
  mime: string;
  bytes: string;
}> {
  if (file.size > 32 * 1024 * 1024) throw new Error("source_too_large");
  const bytes = new Uint8Array(await file.arrayBuffer());
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 8192)
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192));
  return {
    requestId,
    filename: file.name,
    mime: file.type,
    bytes: btoa(binary),
  };
}

/** 上传前才加载编码与发送；取消、原请求身份和真实字节沿用原入口。 */
export async function uploadKnowledgeSource(
  resource: ResourceRef,
  file: Parameters<typeof encodeUpload>[0],
  requestId: string,
  signal: AbortSignal,
  confirmed?: KnowledgeUploadContext,
) {
  const body = await encodeUpload(file, requestId);
  signal.throwIfAborted();
  return (
    await knowledgeAdapter(resource).uploadSource(
      {
        path: { libraryId: resource.knowledgeBaseId },
        body,
        signal,
        throwOnError: true,
      },
      confirmed,
    )
  ).data;
}
