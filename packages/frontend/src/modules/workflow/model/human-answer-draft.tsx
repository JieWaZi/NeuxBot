import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  useSyncExternalStore,
  type PropsWithChildren,
} from "react";
import type { SchemaFormDraft } from "@/shared/json-schema/schema-form";
import { ApiError } from "@/shared/api/errors";
import type { GetWorkflowRunResponse } from "@/shared/api/generated/types.gen";

type AnswerDraft = {
  answer?: unknown;
  customChoice?: string;
  schema?: SchemaFormDraft;
  submission?: {
    signature: string;
    id: string;
    status: "pending" | "accepted" | "failed" | "conflict" | "resolved";
    error?: ApiError;
  };
};
type Entry = { draft: AnswerDraft; listeners: Set<() => void> };
type Drafts = Map<string, Entry>;
const emptyDraft: AnswerDraft = {};
const DraftContext = createContext<Drafts | null>(null);

export function WorkflowAnswerDraftProvider({ children }: PropsWithChildren) {
  const [drafts] = useState<Drafts>(() => new Map());
  return (
    <DraftContext.Provider value={drafts}>{children}</DraftContext.Provider>
  );
}

export function workflowDecisionDraftKey(
  kind: "human" | "operation",
  id: string,
  runId: string,
  requestId?: string,
  digest?: string,
) {
  return requestId && digest
    ? JSON.stringify([kind, id, runId, requestId, digest])
    : undefined;
}

function publish(entry: Entry, draft: AnswerDraft) {
  entry.draft = draft;
  for (const listener of entry.listeners) listener();
}

function resolveDraft(drafts: Drafts, key?: string) {
  if (!key) return;
  const entry = drafts.get(key);
  if (!entry) return;
  if (!entry.listeners.size) {
    drafts.delete(key);
    return;
  }
  const submission = entry.draft.submission;
  if (submission && submission.status !== "resolved")
    publish(entry, {
      ...entry.draft,
      submission: { ...submission, status: "resolved", error: undefined },
    });
}

/** 画布即使已关闭节点详情，也用当前请求状态释放本地提交。 */
export function useResolveWorkflowRunDecisionDrafts(
  id: string,
  runId: string,
  snapshot: GetWorkflowRunResponse | undefined,
) {
  const drafts = useContext(DraftContext);
  useEffect(() => {
    if (!drafts || !snapshot) return;
    const status = snapshot.run?.status;
    const terminal =
      status === "succeeded" || status === "failed" || status === "cancelled";
    for (const human of snapshot.humanRequests ?? [])
      if (terminal || (human.status && human.status !== "waiting"))
        resolveDraft(
          drafts,
          workflowDecisionDraftKey(
            "human",
            id,
            runId,
            human.id,
            human.snapshotDigest,
          ),
        );
    for (const operation of snapshot.operationRequests ?? [])
      if (terminal || (operation.status && operation.status !== "waiting"))
        resolveDraft(
          drafts,
          workflowDecisionDraftKey(
            "operation",
            id,
            runId,
            operation.id,
            operation.descriptorDigest,
          ),
        );
  }, [drafts, id, runId, snapshot]);
}

/** 聊天与画布共用同一快照的草稿及提交状态，已处理结果由运行提供。 */
export function useWorkflowDecisionDraft(
  key: string | undefined,
  status: string | undefined,
) {
  const shared = useContext(DraftContext);
  const [local] = useState<Drafts>(() => new Map());
  const drafts = shared ?? local;
  const waiting = status === "waiting";
  useEffect(() => {
    if (status && !waiting) resolveDraft(drafts, key);
  }, [drafts, key, status, waiting]);
  const subscribe = useCallback(
    (listener: () => void) => {
      if (!key || !waiting) return () => {};
      const entry = drafts.get(key) ?? {
        draft: emptyDraft,
        listeners: new Set<() => void>(),
      };
      drafts.set(key, entry);
      entry.listeners.add(listener);
      return () => {
        entry.listeners.delete(listener);
        const submission = entry.draft.submission;
        if (
          !entry.listeners.size &&
          (!submission || submission.status === "resolved")
        )
          drafts.delete(key);
      };
    },
    [drafts, key, waiting],
  );
  const read = () => (key && drafts.get(key)?.draft) || emptyDraft;
  const draft = useSyncExternalStore(
    subscribe,
    waiting ? read : () => emptyDraft,
  );
  const update = useCallback(
    (
      changes: Partial<AnswerDraft> | ((current: AnswerDraft) => AnswerDraft),
    ) => {
      if (!key) return;
      const entry = drafts.get(key);
      if (!entry) return;
      const draft =
        typeof changes === "function"
          ? changes(entry.draft)
          : { ...entry.draft, ...changes };
      if (draft === entry.draft) return;
      publish(entry, draft);
    },
    [drafts, key],
  );
  function start(signature: string) {
    if (!key || !waiting || !drafts.has(key)) return;
    const last = read().submission;
    if (last && last.status !== "failed") return;
    const id = last?.signature === signature ? last.id : crypto.randomUUID();
    update({ submission: { signature, id, status: "pending" } });
    return id;
  }
  function finish(
    id: string,
    status: "accepted" | "failed" | "conflict",
    error?: unknown,
  ) {
    const last = read().submission;
    if (last?.id === id && last.status === "pending")
      update({
        submission: {
          ...last,
          status,
          error: error instanceof ApiError ? error : undefined,
        },
      });
  }
  return { draft, update, start, finish };
}
