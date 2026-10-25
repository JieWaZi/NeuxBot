import { posix } from "node:path";
import type {
  FileReferenceRequest,
  FileReferenceResolution,
} from "@/shared/platform";

export function fakeFileReference(
  request: FileReferenceRequest,
  aliases: Record<string, string> = {},
): FileReferenceResolution {
  let path = request.path
    .replaceAll("\\", "/")
    .replace(/^~(?=\/)/, "/Users/example");
  if (!path.startsWith("/") && !/^[A-Za-z]:\//.test(path))
    path = posix.resolve(request.baseDirectory, path);
  for (const [alias, real] of Object.entries(aliases)) {
    if (path.startsWith(alias + "/")) {
      path = real + path.slice(alias.length);
      break;
    }
  }
  return {
    status: "resolved",
    canonicalPath: path,
    baseDirectory: request.baseDirectory,
    workspaceRoot: request.workspaceRoot,
    sourceRoots: [...request.sourceRoots],
  };
}
