import { createHash } from "node:crypto";
import { readFile, realpath } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { brotliDecompress } from "node:zlib";
import { within } from "./workspace-files";

export async function rendererFile(dist: string, url: string): Promise<string> {
  const parsed = new URL(url);
  if (parsed.protocol !== "ally:" || parsed.hostname !== "app")
    throw new Error("界面资源地址无效");
  const root = await realpath(dist);
  const requested = path.join(root, decodeURIComponent(parsed.pathname));
  if (!within(root, requested)) throw new Error("界面资源超出根目录");
  const target = await realpath(requested).catch(
    async (error: NodeJS.ErrnoException) => {
      if (error.code !== "ENOENT") throw error;
      return realpath(`${requested}.br`);
    },
  );
  if (!within(root, target)) throw new Error("界面资源超出根目录");
  return target;
}
/** 首帧主题脚本按构建内容授权，页面不能执行额外注入的内联脚本。 */
export async function rendererCSP(file: string): Promise<string> {
  const hashes: string[] = [];
  if (path.extname(file) === ".html") {
    const html = await readFile(file, "utf8");
    for (const match of html.matchAll(
      /<script\b([^>]*)>([\s\S]*?)<\/script>/gi,
    )) {
      if (!/\bsrc\s*=/i.test(match[1] ?? "") && match[2]?.trim())
        hashes.push(
          `'sha256-${createHash("sha256").update(match[2]).digest("base64")}'`,
        );
    }
  }
  return `default-src 'self'; script-src 'self' ${hashes.join(" ")}; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: http://127.0.0.1:*; font-src 'self' data:; connect-src 'self' http://127.0.0.1:* ws://127.0.0.1:*; worker-src 'self' blob:; object-src 'none'; frame-src blob: http: https:`;
}

const decompress = promisify(brotliDecompress);
const contentTypes: Record<string, string> = {
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
};
/** 打包文本在读取时恢复原始字节，保持模块 URL、CSP 与 MIME 不变。 */
export async function rendererResponse(
  dist: string,
  url: string,
  fetchFile: (url: string) => Promise<Response>,
): Promise<Response> {
  const target = await rendererFile(dist, url);
  let response: Response;
  if (target.endsWith(".br")) {
    const extension = path.extname(target.slice(0, -3));
    const mediaType = contentTypes[extension];
    if (!mediaType) throw new Error("不支持的压缩界面资源");
    const bytes = await decompress(await readFile(target));
    response = new Response(new Uint8Array(bytes), {
      headers: {
        "Content-Type": mediaType,
        "Content-Length": String(bytes.length),
      },
    });
  } else {
    response = await fetchFile(pathToFileURL(target).href);
  }
  const headers = new Headers(response.headers);
  headers.set("Content-Security-Policy", await rendererCSP(target));
  return new Response(response.body, { status: response.status, headers });
}
