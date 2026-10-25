import {
  openWorkspaceResourceScope,
  readWorkspaceResource,
  statWorkspaceResource,
  type WorkspaceFileContent,
} from "@/shared/platform";

const textLimit = 10 * 1024 * 1024;
const imageLimit = 50 * 1024 * 1024;
const base64Chunk = 24 * 1024;

function encodeBase64(bytes: Uint8Array): string {
  let encoded = "";
  for (let offset = 0; offset < bytes.length; offset += base64Chunk) {
    encoded += btoa(
      String.fromCharCode(...bytes.subarray(offset, offset + base64Chunk)),
    );
  }
  return encoded;
}

/** Chat 范围内的预览适配层；Office/PDF bytes 保留给后续独立 renderer。 */
export async function readScopedFilePreview(
  chatId: string,
  rootPath: string,
  relativePath: string,
  signal?: AbortSignal,
): Promise<WorkspaceFileContent> {
  const scope = await openWorkspaceResourceScope(chatId);
  const info = await statWorkspaceResource(scope.id, relativePath, rootPath);
  if (!info) throw new Error("文件不存在或已被删除");
  const name = relativePath.split(/[\\/]/).at(-1) ?? relativePath;
  const image = info.mediaType.startsWith("image/");
  const document =
    info.mediaType === "application/pdf" ||
    info.mediaType === "application/msword" ||
    info.mediaType.startsWith("application/vnd.") ||
    info.mediaType === "text/csv" ||
    info.mediaType === "text/tab-separated-values";
  const base = {
    name,
    relativePath,
    mediaType: info.mediaType,
    size: info.size,
    version: info.version,
  };
  if (document) return { ...base, kind: "binary" };
  if (info.size > (image ? imageLimit : textLimit))
    return { ...base, kind: "too_large" };
  const file = await readWorkspaceResource(
    scope.id,
    relativePath,
    signal,
    rootPath,
  );
  if (image)
    return {
      ...base,
      version: file.version,
      kind: "image",
      dataUrl: `data:${file.mediaType};base64,${encodeBase64(file.bytes)}`,
    };
  try {
    return {
      ...base,
      version: file.version,
      kind: "text",
      mediaType: "text/plain; charset=utf-8",
      content: new TextDecoder("utf-8", { fatal: true }).decode(file.bytes),
    };
  } catch {
    return { ...base, version: file.version, kind: "binary" };
  }
}
