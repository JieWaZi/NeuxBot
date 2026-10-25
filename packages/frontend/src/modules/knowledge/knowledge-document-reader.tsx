import { KnowledgePendingSourceCleanups } from "./knowledge-source-cleanup";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, RotateCcw, Trash2 } from "lucide-react";
import { PageAction } from "@/shared/ui/resource-page";
import { IconAction } from "./knowledge-controls";
import { EmptyState } from "@/shared/ui/empty-state";
import { KnowledgeDocumentPreview } from "./knowledge-document-preview";
import {
  KnowledgeSourceDelete,
  createSourceDeleter,
} from "./knowledge-source-delete";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { hasKnowledgeCapability } from "@/shared/api/knowledge/knowledge-adapter-types";
import { downloadSource } from "./knowledge-source-document";
import { knowledgeKeys } from "./knowledge-queries";
import { reportKnowledgeError } from "./knowledge-error-notice";
import {
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
} from "@/shared/api/error-notices";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useDocumentCopy } from "./use-document-copy";
import type { Library, Source } from "./knowledge-model";

type Props = {
  library: Library;
  source?: Source;
  onClose: () => void;
  onRebuild: (source: Source) => void;
  onDeleted: (source: Source) => void;
  disabled?: boolean;
  deletionDisabled?: boolean;
};
/** 文档操作与预览按需加载，读写均使用当前库的完整身份。 */
export function KnowledgeDocumentReader({
  library,
  onRebuild,
  onDeleted,
  source,
  onClose,
  disabled = false,
  deletionDisabled = false,
}: Props) {
  const t = useDocumentCopy();
  const client = useQueryClient();
  const active = useRouteSceneActive();
  const adapter = knowledgeAdapter(library.ref);
  const capabilities = useQuery({
    queryKey: knowledgeKeys.capabilities(library.ref),
    queryFn: ({ signal }) => adapter.getCapabilities(signal),
    enabled: active && adapter.supportsServiceDetection,
    refetchOnMount: false,
  });
  const supportsOperation = (operation: string) =>
    library.ref.backend !== "weknora" &&
    (!adapter.supportsServiceDetection ||
      hasKnowledgeCapability(capabilities.data, operation));

  const [deleting, setDeleting] = useState<Source>();
  const selectedSource = source;
  const [remove] = useState(createSourceDeleter);
  const downloadOperation = (source: Source) =>
    `download-source:${JSON.stringify([...knowledgeKeys.resource(library.ref), source.id])}`;
  const download = useMutation({
    mutationFn: (source: Source) => downloadSource(library.ref, source.id),
    retry: false,
    onMutate: (source) =>
      captureErrorNoticeRecovery(`knowledge:${downloadOperation(source)}`),
    onSuccess: (_result, _source, recovery) =>
      resolveErrorNoticeRecovery(recovery ?? []),
    onError: (cause, source) =>
      reportKnowledgeError(
        cause,
        downloadOperation(source),
        t("downloadFailed"),
      ),
  });
  const actionOperation = (capability: string) =>
    `document-action:${JSON.stringify([...knowledgeKeys.resource(library.ref), capability])}`;
  const action = useMutation({
    mutationFn: async ({
      capability,
      run,
    }: {
      capability: string;
      run: () => void;
    }) => {
      await knowledgeAdapter(library.ref).assertCapability(capability);
      run();
    },
    retry: false,
    onMutate: (operation) =>
      captureErrorNoticeRecovery(
        `knowledge:${actionOperation(operation.capability)}`,
      ),
    onSuccess: (_result, _operation, recovery) =>
      resolveErrorNoticeRecovery(recovery ?? []),
    onError: (cause, operation) =>
      reportKnowledgeError(
        cause,
        actionOperation(operation.capability),
        t(operation.capability === "sourcePurge" ? "delete" : "rebuild"),
      ),
  });
  return (
    <>
      {!source && (
        <EmptyState
          title={t("selectSource")}
          description={t("selectSourceHelp")}
        />
      )}
      {knowledgeAdapter(library.ref).supportsSourceCleanup && (
        <KnowledgePendingSourceCleanups library={library} />
      )}
      {selectedSource && (
        <KnowledgeDocumentPreview
          key={`preview:${selectedSource.id}`}
          presentation="pane"
          actions={
            <div
              role="group"
              aria-label={t("documentActions")}
              className="flex shrink-0 items-center gap-2"
            >
              <IconAction
                icon={Download}
                label={t("download")}
                disabled={download.isPending}
                onClick={() => download.mutate(selectedSource)}
              />
              <IconAction
                icon={RotateCcw}
                label={t("rebuild")}
                disabled={
                  !supportsOperation("rebuild") ||
                  disabled ||
                  action.isPending ||
                  selectedSource.organizeBlocked ||
                  selectedSource.state === "queued" ||
                  selectedSource.state === "processing"
                }
                onClick={() =>
                  action.mutate({
                    capability: "rebuild",
                    run: () => onRebuild(selectedSource),
                  })
                }
              />
              <PageAction
                intent="delete"
                label={t("delete")}
                disabled={
                  !supportsOperation("sourcePurge") ||
                  disabled ||
                  deletionDisabled ||
                  action.isPending
                }
                onClick={() =>
                  action.mutate({
                    capability: "sourcePurge",
                    run: () => setDeleting(selectedSource),
                  })
                }
              >
                <Trash2 />
              </PageAction>
            </div>
          }
          resource={library.ref}
          source={selectedSource}
          downloadError={
            download.variables?.id === selectedSource.id
              ? download.error
              : undefined
          }
          downloading={download.isPending}
          onClose={onClose}
          onDownload={() => download.mutate(selectedSource)}
        />
      )}
      {deleting && (
        <KnowledgeSourceDelete
          key={`delete:${deleting.id}`}
          library={library}
          source={deleting}
          remove={remove}
          onClose={() => setDeleting(undefined)}
          onDeleted={async () => {
            onDeleted(deleting);
            if (source?.id === deleting.id) onClose();
            await Promise.all([
              client.invalidateQueries({
                queryKey: knowledgeKeys.resource(library.ref),
              }),
              client.invalidateQueries({
                queryKey: knowledgeKeys.libraryLists,
              }),
            ]);
          }}
        />
      )}{" "}
    </>
  );
}
