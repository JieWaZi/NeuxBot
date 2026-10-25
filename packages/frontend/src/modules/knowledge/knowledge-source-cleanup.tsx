import { useState } from "react";
import {
  useQuery,
  useMutation,
  useQueryClient,
  queryOptions,
} from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { hasKnowledgeCapability } from "@/shared/api/knowledge/knowledge-adapter-types";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { ApiError } from "@/shared/api/errors";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Button } from "@/shared/ui/button";
import { Skeleton } from "@/shared/ui/skeleton";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";
import { knowledgeKeys } from "./knowledge-queries";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { useDocumentCopy } from "./use-document-copy";
import type { Library } from "./knowledge-model";

/** 读取原清单后才继续；身份由持久清单版本及当前授权确定，重开页面也不重复恢复同一版本。 */
export async function continueSourceCleanup(library: Library, purgeId: string) {
  const path = { libraryId: library.id, purgeId };
  const read = async () =>
    (
      await knowledgeAdapter(library.ref).getSourcePurge({
        path,
        throwOnError: true,
      })
    ).data;
  const status = await read();
  if (status.state === "completed") return true;
  const currentPolicy =
    (
      await knowledgeAdapter(library.ref).getLibrary({
        path: { libraryId: library.id },
        throwOnError: true,
      })
    ).data.policyVersion || library.policyVersion;
  const requestId = `r:${purgeId}:${(status.version || 0).toString(36)}:${currentPolicy.toString(36)}`;
  await knowledgeAdapter(library.ref).recoverSourcePurge({
    path,
    body: { requestId, expectedPolicyVersion: currentPolicy },
    throwOnError: true,
  });
  return (await read()).state === "completed";
}
function pendingCleanupOptions(library: Library) {
  return queryOptions({
    queryKey: [...knowledgeKeys.resource(library.ref), "source-purges"],
    staleTime: 0,
    queryFn: async ({ signal }) => {
      const rows: NonNullable<
        Awaited<
          ReturnType<ReturnType<typeof knowledgeAdapter>["listSourcePurges"]>
        >["data"]
      >["items"] = [];
      let offset = 0;
      while (true) {
        const { data } = await knowledgeAdapter(library.ref).listSourcePurges(
          withLocalErrorSurface({
            path: { libraryId: library.id },
            query: { offset, limit: 100 },
            signal,
            throwOnError: true,
          }),
        );
        const items = data.items ?? [];
        rows.push(...items);
        offset += items.length;
        if (offset >= (data.total ?? offset))
          return rows.filter((row) => row.state !== "completed");
        if (!items.length)
          throw new ApiError(
            "Incomplete cleanup list",
            "KNOWLEDGE_CLEANUP_LIST",
          );
      }
    },
  });
}

/** 来源行已删除后仍有独立的持久清单入口，可跨视图及重启后继续清理。 */
export function KnowledgePendingSourceCleanups({
  library,
}: {
  library: Library;
}) {
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
  // 统一连接可能指向官方服务；仅在清理能力、授权与当前条件均可用时读取附属清单。
  const supported =
    adapter.supportsSourceCleanup &&
    (!adapter.supportsServiceDetection ||
      hasKnowledgeCapability(capabilities.data, "sourcePurge"));
  const pending = useQuery({
    ...pendingCleanupOptions(library),
    enabled: active && supported,
  });
  const [selected, setSelected] = useState<string>();
  const recovery = useMutation({
    mutationFn: async () => {
      if (!selected) return;
      if (!(await continueSourceCleanup(library, selected)))
        throw new ApiError(t("deletePending"), "KNOWLEDGE_CLEANUP_PENDING");
      await client.invalidateQueries({
        queryKey: knowledgeKeys.resource(library.ref),
      });
      setSelected(undefined);
    },
    retry: false,
  });
  if (!supported) return null;
  return (
    <>
      <KnowledgeReadFeedback
        hasData={Boolean(pending.data)}
        state={{
          pending: pending.isPending,
          error: pending.error,
          retry: () => {
            void pending.refetch();
          },
        }}
        loading={
          <Skeleton className="h-control-sm w-full motion-reduce:animate-none" />
        }
      />
      {pending.data?.map((row) => (
        <div
          key={row.id}
          className="bg-surface-subtle flex min-w-0 flex-wrap items-center justify-between gap-3 rounded-lg p-3"
        >
          <p className="type-body">
            {t("pendingCleanup", { count: row.versions?.length || 0 })}
          </p>
          <Button
            size="sm"
            variant="secondary"
            disabled={!row.id}
            onClick={() => {
              recovery.reset();
              setSelected(row.id);
            }}
          >
            {t("resumeCleanup")}
          </Button>
        </div>
      ))}
      {selected && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open && !recovery.isPending) setSelected(undefined);
          }}
        >
          <DialogContent title={t("resumeTitle")} description={t("resumeHelp")}>
            <DialogFooter>
              <Button
                variant="secondary"
                disabled={recovery.isPending}
                onClick={() => setSelected(undefined)}
              >
                {t("close")}
              </Button>
              <Button
                disabled={recovery.isPending}
                onClick={() => recovery.mutate()}
              >
                {t(recovery.isPending ? "deleting" : "continueCleanup")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  );
}
