import {
  openWorkspaceResourceScope,
  readWorkspaceResource,
  statWorkspaceResource,
} from "@/shared/platform";
import { maxOfficeSourceBytes, validateOfficeArchive } from "./office-archive";
import { OfficePreviewError } from "./office-preview-error";

/** Office renderer 只消费 Core 授权 Chat 下的完整 bytes。 */
export async function readOfficeBytes(
  chatId: string | undefined,
  rootPath: string,
  path: string,
  signal: AbortSignal,
): Promise<ArrayBuffer> {
  if (!chatId) throw new OfficePreviewError("scopeRequired");
  const kind = /\.docx$/i.test(path) ? "docx" : "pptx";
  const scope = await openWorkspaceResourceScope(chatId);
  signal.throwIfAborted();
  const stat = await statWorkspaceResource(scope.id, path, rootPath);
  if (!stat) throw new OfficePreviewError("fileMissing");
  if (stat.size > maxOfficeSourceBytes)
    throw new OfficePreviewError("sourceTooLarge");
  const file = await readWorkspaceResource(scope.id, path, signal, rootPath);
  signal.throwIfAborted();
  const bytes = Uint8Array.from(file.bytes);
  validateOfficeArchive(bytes, kind);
  return bytes.buffer;
}
