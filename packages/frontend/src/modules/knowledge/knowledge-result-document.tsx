import { useMutation } from "@tanstack/react-query";
import { KnowledgeDocumentPreview } from "./knowledge-document-preview";
import { downloadSource } from "./knowledge-source-document";
import type { ResourceRef } from "./knowledge-transfer";

/** Wiki 引用直接预览原件版本，翻页和筛选不影响原文查看。 */
export function KnowledgeResultDocument({
  resource,
  sourceId,
  onClose,
}: {
  resource: ResourceRef;
  sourceId: string;
  onClose: () => void;
}) {
  const download = useMutation({
    mutationFn: () => downloadSource(resource, sourceId),
    retry: false,
  });
  return (
    <KnowledgeDocumentPreview
      resource={resource}
      source={{
        id: sourceId,
        name: "",
        size: "—",
        state: "unknown",
        diagnostic: "",
        date: "—",
      }}
      onClose={onClose}
      onDownload={() => download.mutate()}
      downloadError={download.error}
      downloading={download.isPending}
    />
  );
}
