import { KnowledgeReadFeedback } from "./knowledge-read-feedback";
import { KnowledgeDocumentsLoading } from "./knowledge-loading";
import { KnowledgeSourceList } from "./knowledge-source-list";
import { useCopy } from "./use-copy";
import type { KnowledgeReadState, Route, Source } from "./knowledge-model";

/** 原件目录与 Wiki 目录共享左侧空间，内容读取和文件操作由右侧阅读器负责。 */
export function RawView({
  rows,
  selectedId,
  onOpen,
  route,
  readState,
  hasMore,
  pending,
  failed,
  onLoadMore,
}: {
  rows: Source[];
  selectedId?: string;
  onOpen: (source: Source) => void;
  route: Route;
  readState: KnowledgeReadState;
  hasMore: boolean;
  pending: boolean;
  failed: boolean;
  onLoadMore: () => void;
}) {
  const t = useCopy();
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3">
      <KnowledgeReadFeedback
        state={readState}
        hasData={Boolean(rows.length)}
        loading={<KnowledgeDocumentsLoading />}
      />
      {!readState.pending && !readState.error && !rows.length && (
        <p className="type-body text-content-secondary">
          {route.sourceQuery ? t("noMatchingSources") : t("rawEmpty")}
        </p>
      )}
      <KnowledgeSourceList
        rows={rows}
        selectedId={selectedId}
        onOpen={onOpen}
        hasMore={hasMore}
        pending={pending}
        failed={failed}
        onLoadMore={onLoadMore}
      />
    </div>
  );
}
