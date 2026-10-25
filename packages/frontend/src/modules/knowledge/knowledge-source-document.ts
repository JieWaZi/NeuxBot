import { queryOptions } from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { saveBlob } from "@/shared/platform";
import type { ResourceRef } from "./knowledge-transfer";
import { knowledgeKeys } from "./knowledge-queries";

export function sourceDetailOptions(ref: ResourceRef, id: string) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "source", id],
    queryFn: async ({ signal }) => {
      const options = withLocalErrorSurface({
        path: {
          libraryId: ref.knowledgeBaseId,
          connectionId: ref.connectionId,
          sourceId: id,
        },
        signal,
        throwOnError: true as const,
      });
      return (await knowledgeAdapter(ref).getSource(options)).data;
    },
  });
}
export async function readSourceRaw(
  ref: ResourceRef,
  id: string,
  signal?: AbortSignal,
) {
  const options = withLocalErrorSurface({
    path: {
      libraryId: ref.knowledgeBaseId,
      connectionId: ref.connectionId,
      sourceId: id,
    },
    signal,
    throwOnError: true as const,
  });
  return (await knowledgeAdapter(ref).getRaw(options)).data;
}
export function sourceBytes(raw: { bytes?: string }) {
  return Uint8Array.from(atob(raw.bytes ?? ""), (char) => char.charCodeAt(0));
}
export async function downloadSource(ref: ResourceRef, id: string) {
  const raw = await readSourceRaw(ref, id);
  return saveBlob(
    new Blob([sourceBytes(raw)], {
      type: raw.mime || "application/octet-stream",
    }),
    raw.filename || id,
  );
}
/** 原始文本完整展示；二进制文档只读取已保存的实际分块，不临时运行解析或模型。 */
export function sourceContentOptions(
  ref: ResourceRef,
  id: string,
  name: string,
) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(ref), "source-content", id, name],
    queryFn: async ({ signal }) => {
      if (/\.(md|markdown|txt|csv|json|yaml|yml|log|xml|html?)$/i.test(name)) {
        const raw = await readSourceRaw(ref, id, signal);
        return {
          text: new TextDecoder().decode(sourceBytes(raw)),
          markdown: /\.(md|markdown)$/i.test(name),
        };
      }
      const { data: chunks } = await knowledgeAdapter(ref).getSourceChunks(
        withLocalErrorSurface({
          path: { libraryId: ref.knowledgeBaseId, sourceId: id },
          signal,
          throwOnError: true,
        }),
      );
      const parents = chunks.filter((chunk) => chunk.chunkType === "parent");
      const rows = parents.length
        ? parents
        : chunks.filter((chunk) => chunk.chunkType === "child");
      return {
        text: rows
          .toSorted((a, b) => (a.sequence ?? 0) - (b.sequence ?? 0))
          .map((chunk) => chunk.content || "")
          .join("\n\n"),
        markdown: false,
      };
    },
  });
}
