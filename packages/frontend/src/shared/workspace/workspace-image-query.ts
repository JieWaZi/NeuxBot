import { queryOptions } from "@tanstack/react-query";
import { workspaceSingleFileBytesLimit } from "@neuxbot/platform-contracts/workspace-byte-limits";
import {
  openWorkspaceResourceScope,
  readWorkspaceResource,
  statWorkspaceResource,
} from "@/shared/platform";
import type { ResolvedFileReference } from "./file-reference";

const imageTypes = new Set([
  "image/png",
  "image/jpeg",
  "image/gif",
  "image/webp",
]);

export class WorkspaceImageError extends Error {
  constructor(readonly reason: "failed" | "tooLarge" | "unsupported") {
    super(reason);
  }
}

function checkImage(size: number, mediaType: string) {
  if (!Number.isSafeInteger(size) || size < 0)
    throw new WorkspaceImageError("failed");
  if (size > workspaceSingleFileBytesLimit)
    throw new WorkspaceImageError("tooLarge");
  if (!imageTypes.has(mediaType)) throw new WorkspaceImageError("unsupported");
}

/** 规范文件身份共用一次有界读取；最后一个可见消费者离开即释放缓存和传输。 */
export function workspaceImageOptions(
  chatId: string,
  file: ResolvedFileReference,
) {
  return queryOptions({
    queryKey: ["workspace-markdown-image", chatId, file.rootPath, file.path],
    queryFn: async ({ signal }) => {
      const scope = await openWorkspaceResourceScope(chatId);
      signal.throwIfAborted();
      const info = await statWorkspaceResource(
        scope.id,
        file.path,
        file.rootPath,
      );
      signal.throwIfAborted();
      if (!info) throw new WorkspaceImageError("failed");
      checkImage(info.size, info.mediaType);
      const resource = await readWorkspaceResource(
        scope.id,
        file.path,
        signal,
        file.rootPath,
      );
      signal.throwIfAborted();
      // stat 与 read 之间文件可能被替换，读取结果仍须独立满足大小和媒体类型约束。
      checkImage(resource.size, resource.mediaType);
      checkImage(resource.bytes.byteLength, resource.mediaType);
      return new Blob([Uint8Array.from(resource.bytes)], {
        type: resource.mediaType,
      });
    },
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
    throwOnError: false,
  });
}

/** data 图片只在宿主显式允许时解码，仍沿用相同媒体白名单和实际字节上限。 */
export function workspaceDataImageOptions(src: string) {
  return queryOptions({
    queryKey: ["workspace-markdown-data-image", src],
    queryFn: () => {
      const comma = src.indexOf(",");
      if (comma < 0) throw new WorkspaceImageError("unsupported");
      const [mediaType = "", ...parameters] = src
        .slice(5, comma)
        .toLowerCase()
        .split(";");
      checkImage(0, mediaType);
      const base64 = parameters.includes("base64");
      const encoded = src.slice(comma + 1);
      const encodedLimit = base64
        ? 4 * Math.ceil(workspaceSingleFileBytesLimit / 3)
        : 3 * workspaceSingleFileBytesLimit;
      if (encoded.length > encodedLimit)
        throw new WorkspaceImageError("tooLarge");
      let decoded: string;
      try {
        if (base64) decoded = atob(encoded);
        else {
          if (/%(?![\da-f]{2})/i.test(encoded))
            throw new WorkspaceImageError("failed");
          decoded = encoded.replace(/%([\da-f]{2})/gi, (_match, hex: string) =>
            String.fromCharCode(Number.parseInt(hex, 16)),
          );
        }
      } catch {
        throw new WorkspaceImageError("failed");
      }
      checkImage(decoded.length, mediaType);
      const bytes = Uint8Array.from(decoded, (character) => {
        const byte = character.charCodeAt(0);
        if (byte > 255) throw new WorkspaceImageError("failed");
        return byte;
      });
      return new Blob([bytes], { type: mediaType });
    },
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
    throwOnError: false,
  });
}
