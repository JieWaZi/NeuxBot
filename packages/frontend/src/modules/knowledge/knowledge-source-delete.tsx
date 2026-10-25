import { continueSourceCleanup } from "./knowledge-source-cleanup";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import type { GithubComJieWaZiNeuxKnowledgeInternalDomainSourcePurgePreview as PurgePreview } from "@/shared/api/generated/types.gen";
import { ApiError } from "@/shared/api/errors";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Button } from "@/shared/ui/button";
import { KnowledgeWikiContentLoading } from "./knowledge-loading";
import { knowledgeKeys } from "./knowledge-queries";
import { sourceDetailOptions } from "./knowledge-source-document";
import { reportKnowledgeError } from "./knowledge-error-notice";
import {
  captureErrorNoticeRecovery,
  resolveErrorNoticeRecovery,
} from "@/shared/api/error-notices";
import { isKnowledgeWriteOutcomeUnknown } from "./knowledge-write-outcome";
import type { Library, Source } from "./knowledge-model";
import { useDocumentCopy } from "./use-document-copy";

/** 关闭确认框后仍保留未知清理的原始身份；重试只恢复原清单。 */
export function createSourceDeleter() {
  const attempts = new Map<
    string,
    {
      preview: PurgePreview;
      requestId: string;
      purgeId?: string;
    }
  >();
  const getKey = (library: Library, versionId: string) =>
    JSON.stringify([...knowledgeKeys.resource(library.ref), versionId]);
  return {
    retained: (library: Library, versionId: string) =>
      attempts.get(getKey(library, versionId))?.preview,
    perform: async (
      library: Library,
      versionId: string,
      preview: PurgePreview,
    ) => {
      const key = getKey(library, versionId);
      let attempt = attempts.get(key);
      if (!attempt) {
        attempt = { preview, requestId: crypto.randomUUID() };
        attempts.set(key, attempt);
      }
      const path = { libraryId: library.id };
      try {
        if (!attempt.purgeId) {
          const { data: receipt } = await knowledgeAdapter(
            library.ref,
          ).confirmSourcePurge({
            path,
            body: {
              requestId: attempt.requestId,
              sourceId: attempt.preview.sourceId || "",
              previewDigest: attempt.preview.digest || "",
              confirmed: true,
            },
            throwOnError: true,
          });
          if (!receipt.resourceId)
            throw new ApiError(
              "Missing cleanup receipt",
              "KNOWLEDGE_CLEANUP_RECEIPT",
            );
          attempt.purgeId = receipt.resourceId;
        }
        if (!(await continueSourceCleanup(library, attempt.purgeId)))
          return false;
        attempts.delete(key);
        return true;
      } catch (cause) {
        if (!attempt.purgeId && !isKnowledgeWriteOutcomeUnknown(cause))
          attempts.delete(key);
        throw cause;
      }
    },
  };
}

export function KnowledgeSourceDelete({
  library,
  source,
  remove,
  onClose,
  onDeleted,
}: {
  library: Library;
  source: Source;
  remove: ReturnType<typeof createSourceDeleter>;
  onClose: () => void;
  onDeleted: () => Promise<void>;
}) {
  const t = useDocumentCopy();
  const client = useQueryClient();
  const retained = remove.retained(library, source.id);
  const preview = useQuery({
    queryKey: [
      ...knowledgeKeys.resource(library.ref),
      "source-purge-preview",
      source.id,
    ],
    staleTime: 0,
    queryFn: async ({ signal }) => {
      if (retained) return retained;
      const adapter = knowledgeAdapter(library.ref);
      if (adapter.supportsServiceDetection)
        await adapter.assertCapability("sourcePurge", signal);
      const operation = `source-delete-preview:${JSON.stringify([...knowledgeKeys.resource(library.ref), source.id])}`;
      const recovery = captureErrorNoticeRecovery(`knowledge:${operation}`);
      const stableId =
        source.sourceId ||
        (
          await client
            .fetchQuery(sourceDetailOptions(library.ref, source.id))
            .then((detail) => {
              resolveErrorNoticeRecovery(recovery);
              return detail;
            })
            .catch((cause: unknown) => {
              reportKnowledgeError(cause, operation, t("deleteFailed"));
              throw cause;
            })
        ).sourceId;
      return (
        await knowledgeAdapter(library.ref).previewSourcePurge({
          path: { libraryId: library.id },
          query: { sourceId: stableId || "" },
          signal,
          throwOnError: true,
        })
      ).data;
    },
  });
  const deletion = useMutation({
    mutationFn: async () => {
      if (!preview.data) return;
      if (!(await remove.perform(library, source.id, preview.data)))
        throw new ApiError(t("deletePending"), "KNOWLEDGE_CLEANUP_PENDING");
      await onDeleted();
      onClose();
    },
    retry: false,
    onError: (cause) => {
      void client.invalidateQueries({
        queryKey: [...knowledgeKeys.resource(library.ref), "source-purges"],
      });
      if (!isKnowledgeWriteOutcomeUnknown(cause)) void preview.refetch();
    },
  });
  const blocked = Boolean(preview.data?.blockingJobIds?.length);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !deletion.isPending) onClose();
      }}
    >
      <DialogContent
        title={t("deleteTitle")}
        description={t("deleteHelp", { name: source.name })}
      >
        <div className="space-y-4 px-6 pb-5">
          {preview.isPending ? (
            <KnowledgeWikiContentLoading />
          ) : preview.error ? null : (
            preview.data && (
              <p className="type-body text-content-secondary">
                {t("deleteImpact", {
                  versions: preview.data.versions?.length || 0,
                  pages: preview.data.pages?.length || 0,
                  archived:
                    preview.data.pages?.filter((page) => !page.preserveBody)
                      .length || 0,
                })}
              </p>
            )
          )}
          {blocked && (
            <p className="type-body text-content-secondary">
              {t("deleteBlocked")}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button
            variant="secondary"
            disabled={deletion.isPending}
            onClick={onClose}
          >
            {t("cancel")}
          </Button>
          <Button
            variant="danger"
            disabled={
              !preview.data ||
              Boolean(preview.error) ||
              blocked ||
              deletion.isPending
            }
            onClick={() => deletion.mutate()}
          >
            {t(deletion.isPending ? "deleting" : "delete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
