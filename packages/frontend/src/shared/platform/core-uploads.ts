import type { CoreConnection } from "@neuxbot/platform-contracts/resources";
import { uploadFileBytesLimit } from "@neuxbot/platform-contracts/upload-byte-limits";

const isUploadReference = (value: unknown): value is string =>
  typeof value === "string" && value.startsWith("upload:");

/** 同一 Core 上传租约承载桌面与网页的字节附件。 */
export function createCoreUploads(
  getConnection: () => Promise<CoreConnection>,
) {
  async function send(
    method: string,
    query: Record<string, string>,
    bytes?: Uint8Array,
  ) {
    const connection = await getConnection();
    const { apiLanguageHeaders } =
      await import("@/shared/api/language-headers");
    const headers = new Headers(apiLanguageHeaders());
    if (connection.token)
      headers.set("Authorization", `Bearer ${connection.token}`);
    if (connection.csrfToken) headers.set("X-Bot-CSRF", connection.csrfToken);
    if (bytes) headers.set("Content-Type", "application/octet-stream");
    const target = new URL("/api/v1/uploads", connection.baseUrl);
    target.search = new URLSearchParams(query).toString();
    const response = await fetch(target, {
      method,
      headers,
      credentials: connection.credentials ?? "omit",
      body: bytes && new Uint8Array(bytes),
    });
    if (response.status === 401 && connection.credentials === "same-origin")
      window.dispatchEvent(new Event("bot:web-session-expired"));
    if (!response.ok) {
      const body: unknown = await response.json().catch(() => undefined);
      const message =
        body &&
        typeof body === "object" &&
        "message" in body &&
        typeof body.message === "string"
          ? body.message
          : response.statusText;
      throw new Error(message || "Upload request failed");
    }
    return response;
  }
  return {
    async stage_attachment({
      name,
      bytes,
    }: {
      name: string;
      bytes: Uint8Array;
    }): Promise<string> {
      if (bytes.byteLength > uploadFileBytesLimit)
        throw new Error("Attachment exceeds the upload limit");
      const response = await send("POST", { name }, bytes);
      const result: unknown = await response.json();
      if (
        result &&
        typeof result === "object" &&
        "sourcePath" in result &&
        isUploadReference(result.sourcePath)
      )
        return result.sourcePath;
      throw new Error("Core did not return an upload reference");
    },
    async discard_attachment({
      sourcePath,
    }: {
      sourcePath: string;
    }): Promise<void> {
      if (isUploadReference(sourcePath)) await send("DELETE", { sourcePath });
    },
  };
}
