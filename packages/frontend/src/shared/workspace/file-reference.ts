import {
  resolveFileReferencePath,
  resolveWorkspaceResourceReference,
} from "@/shared/platform";

export type FileReferenceContext = {
  chatId?: string;
  baseDirectory: string;
  workspaceRoot: string;
  sourceRoots: readonly string[];
};

export type FileLocation = { line?: number; column?: number; endLine?: number };
export type FileReferenceCandidate = FileLocation & {
  kind: "file";
  path: string;
  raw: string;
};
export type WebReference = { kind: "web"; url: string };
export type Reference = FileReferenceCandidate | WebReference;
type ResolvedPathReference = {
  canonicalPath: string;
  displayPath: string;
  name: string;
};
export type ResolvedDirectoryReference = ResolvedPathReference;
export type ResolvedFileReference = ResolvedPathReference &
  FileLocation & {
    rootPath: string;
    path: string;
  };
export type FileReferenceResult =
  | { status: "resolved"; file: ResolvedFileReference }
  | { status: "directory"; directory: ResolvedDirectoryReference }
  | { status: "missing" | "outside_scope" | "ambiguous" | "unavailable" };

// Markdown 管线仅传递不透明引用，不再把工作区相对路径伪装成绝对路径。
const REFERENCE_HREF_PREFIX = "/.ally/file-reference/";
const emptyContext: FileReferenceContext = {
  baseDirectory: "",
  workspaceRoot: "",
  sourceRoots: [],
};
export { emptyContext as emptyFileReferenceContext };

export function fileName(path: string): string {
  return path.replaceAll("\\", "/").split("/").at(-1) || path;
}

export function fileDirectory(path: string): string {
  const normalized = path.replaceAll("\\", "/");
  const index = normalized.lastIndexOf("/");
  const root = normalized.slice(0, index) || "/";
  return /^[A-Za-z]:$/.test(root) ? `${root}/` : root;
}

export function isAbsoluteFilePath(path: string): boolean {
  return /^(?:\/|[A-Za-z]:[\\/])/.test(path);
}

/** 语法层只识别引用，不根据当前目录、扩展名白名单或操作系统目录名判定文件权限。 */
export function parseReference(
  value: string,
  source: "link" | "inline" | "path" = "link",
): Reference | null {
  const raw = value.trim();
  if (!raw || /[\0\r\n]/.test(raw)) return null;
  if (source === "link" && raw.startsWith(REFERENCE_HREF_PREFIX)) {
    try {
      const decoded: unknown = JSON.parse(
        decodeURIComponent(raw.slice(REFERENCE_HREF_PREFIX.length)),
      );
      if (
        !decoded ||
        typeof decoded !== "object" ||
        !("path" in decoded) ||
        typeof decoded.path !== "string"
      )
        return null;
      const candidate = parseReference(decoded.path, "path");
      if (candidate?.kind !== "file") return null;
      if (
        "line" in decoded &&
        typeof decoded.line === "number" &&
        Number.isSafeInteger(decoded.line) &&
        decoded.line > 0
      )
        candidate.line = decoded.line;
      if (
        "column" in decoded &&
        typeof decoded.column === "number" &&
        Number.isSafeInteger(decoded.column) &&
        decoded.column > 0
      )
        candidate.column = decoded.column;
      if (
        "endLine" in decoded &&
        typeof decoded.endLine === "number" &&
        Number.isSafeInteger(decoded.endLine) &&
        decoded.endLine > 0
      )
        candidate.endLine = decoded.endLine;
      return candidate;
    } catch {
      return null;
    }
  }
  if (/^https?:\/\//i.test(raw)) {
    try {
      new URL(raw);
      return { kind: "web", url: raw };
    } catch {
      return null;
    }
  }
  if (raw.startsWith("#")) return null;
  let path = raw;
  const location: FileLocation = {};
  if (source !== "path") {
    const suffix = /(?:#L(\d+)(?:-L(\d+))?|:(\d+)(?::(\d+))?)$/.exec(path);
    if (suffix) {
      location.line = Number(suffix[1] ?? suffix[3]);
      if (suffix[2]) location.endLine = Number(suffix[2]);
      if (suffix[4]) location.column = Number(suffix[4]);
      path = path.slice(0, suffix.index);
    }
  }
  if (/^file:/i.test(path)) {
    try {
      const url = new URL(path);
      if (url.hostname && url.hostname !== "localhost") return null;
      path = decodeURIComponent(url.pathname);
      if (/^\/[A-Za-z]:\//.test(path)) path = path.slice(1);
    } catch {
      return null;
    }
  } else {
    if (!/^[A-Za-z]:[\\/]/.test(path) && /^[a-z][a-z\d+.-]*:/i.test(path))
      return null;
    if (source === "link") {
      try {
        path = decodeURIComponent(path);
      } catch {
        return null;
      }
    }
  }
  if (!path || path.includes("\0")) return null;
  if (
    source === "inline" &&
    !isAbsoluteFilePath(path) &&
    !/^~[\\/]/.test(path)
  ) {
    // 隐式引用应有路径形态；普通变量和命令留在代码中。真实存在性由统一解析器确认。
    if (/\s/.test(path) || !/[\\/.]/.test(path)) return null;
  }
  return {
    kind: "file",
    path,
    raw,
    ...location,
  };
}

export function referenceHref(reference: Reference): string {
  if (reference.kind === "web") return reference.url;
  return `${REFERENCE_HREF_PREFIX}${encodeURIComponent(JSON.stringify(reference))}`;
}

export function localFileURL(path: string): string {
  const normalized = path.replaceAll("\\", "/");
  return `file://${normalized.startsWith("/") ? "" : "/"}${normalized
    .split("/")
    .map((part, index) =>
      index === 0 && /^[A-Za-z]:$/.test(part) ? part : encodeURIComponent(part),
    )
    .join("/")}`;
}

function relativeWithin(path: string, root: string): string | undefined {
  const prefix = root.replace(/\/+$/, "") + "/";
  const windows = /^[A-Za-z]:\//.test(path);
  return (
    windows
      ? path.toLowerCase().startsWith(prefix.toLowerCase())
      : path.startsWith(prefix)
  )
    ? path.slice(prefix.length)
    : undefined;
}

/** 所有入口共用真实文件解析；状态与文件身份分离，不以 null 隐藏失败原因。 */
export async function resolveFileReference(
  candidate: FileReferenceCandidate,
  context: FileReferenceContext,
): Promise<FileReferenceResult> {
  const result = context.chatId
    ? await resolveWorkspaceResourceReference(
        context.chatId,
        candidate.path,
        context.baseDirectory,
      )
    : await resolveFileReferencePath({
        path: candidate.path,
        baseDirectory: context.baseDirectory,
        workspaceRoot: context.workspaceRoot,
        sourceRoots: context.sourceRoots,
      });
  if (result.status !== "resolved" && result.status !== "directory")
    return { status: result.status };
  if (result.status === "directory")
    return {
      status: "directory",
      directory: {
        canonicalPath: result.canonicalPath,
        displayPath: `${result.canonicalPath.replace(/\/$/, "")}/`,
        name: fileName(result.canonicalPath),
      },
    };
  const rootPath =
    result.sourceRoots
      .toSorted((left, right) => right.length - left.length)
      .find((root) => relativeWithin(result.canonicalPath, root)) ??
    (result.workspaceRoot &&
    relativeWithin(result.canonicalPath, result.workspaceRoot)
      ? result.workspaceRoot
      : fileDirectory(result.canonicalPath));
  return {
    status: "resolved",
    file: {
      canonicalPath: result.canonicalPath,
      displayPath: result.canonicalPath,
      name: fileName(result.canonicalPath),
      rootPath,
      path:
        relativeWithin(result.canonicalPath, rootPath) ??
        fileName(result.canonicalPath),
      line: candidate.line,
      column: candidate.column,
      endLine: candidate.endLine,
    },
  };
}

export function fileReferenceLabel(
  file: ResolvedFileReference | ResolvedDirectoryReference,
  label?: string,
): string {
  return label || file.canonicalPath;
}
