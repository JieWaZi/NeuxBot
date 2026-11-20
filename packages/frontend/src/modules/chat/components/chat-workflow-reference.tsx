import { useQueryClient } from "@tanstack/react-query";
import type { AppendMessage, ThreadComposerRuntime } from "@assistant-ui/react";
import type { JSONContent } from "@tiptap/core";
import { useEffect, useRef, useState } from "react";
import { listWorkflowsOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { pathFile, type BotPathFile } from "../runtime/chat-attachment-adapter";

export type ChatWorkflowReference = { id: string; name: string };
type ChatComposerDraft = {
  document?: JSONContent;
  attachments?: readonly BotPathFile[];
  uploads?: Set<Promise<void>>;
  pendingSubmission?: symbol;
  restoredSubmission?: symbol;
};
export type ChatComposerDrafts = Map<string, ChatComposerDraft>;
export type ChatWorkflowPicker = {
  search: (text: string) => Promise<ChatWorkflowReference[]>;
  onSelectionChange: (
    selection: ChatWorkflowReference | undefined,
    referenceOnly: boolean,
  ) => void;
  draft?: JSONContent;
  restoredSubmission?: symbol;
  saveDraft?: (document?: JSONContent) => void;
  addPastedFiles?: (
    files: readonly File[],
    onAdded: () => void,
    onError: () => void,
  ) => Promise<void>;
  preserveTextDraft?: boolean;
};

const candidateLimit = 24;

function updateDraft(
  drafts: ChatComposerDrafts,
  key: string,
  changes: Partial<ChatComposerDraft>,
) {
  const draft = { ...drafts.get(key), ...changes };
  if (draft.document || draft.attachments?.length || draft.uploads?.size) {
    draft.pendingSubmission = undefined;
    drafts.set(key, draft);
  } else if (draft.pendingSubmission) drafts.set(key, draft);
  else drafts.delete(key);
}

export function hasChatComposerDraft(drafts: ChatComposerDrafts, key: string) {
  const draft = drafts.get(key);
  return Boolean(
    draft?.document || draft?.attachments?.length || draft?.uploads?.size,
  );
}

/** 页签只清空可编辑草稿，原提交有自己的文档与附件快照。 */
export function captureTeamComposerDraft(
  drafts: ChatComposerDrafts,
  key: string,
  message: AppendMessage,
  onRestored: () => void,
) {
  const document = drafts.get(key)?.document;
  const attachments = (message.attachments ?? []).flatMap((attachment) =>
    (attachment.content ?? []).flatMap((part) =>
      part.type === "file"
        ? [
            pathFile(part.data, {
              name: attachment.name,
              contentType: part.mimeType,
            }),
          ]
        : [],
    ),
  );
  if (!document && !attachments.length) return;
  const pendingSubmission = Symbol();
  drafts.set(key, { pendingSubmission });
  return (rejected: boolean) => {
    if (drafts.get(key)?.pendingSubmission !== pendingSubmission) return;
    if (rejected) {
      drafts.set(key, {
        document,
        attachments,
        restoredSubmission: pendingSubmission,
      });
      onRestored();
    } else {
      drafts.delete(key);
    }
  };
}

function composerFiles(composer: ThreadComposerRuntime): BotPathFile[] {
  return composer
    .getState()
    .attachments.flatMap(({ file, name, contentType }) => {
      const sourcePath = (file as BotPathFile | undefined)?.botSourcePath;
      return sourcePath ? [pathFile(sourcePath, { name, contentType })] : [];
    });
}

/** 主聊和成员各用原 Composer，暂存中的粘贴随草稿在页签间交接。 */
export function useTeamComposerDraftAttachments(
  composer: ThreadComposerRuntime,
  drafts: ChatComposerDrafts,
  scopeKey: string,
) {
  const restoredSubmission = drafts.get(scopeKey)?.restoredSubmission;
  const [completedRestoration, setCompletedRestoration] = useState<symbol>();
  const [attachmentsPending, setAttachmentsPending] = useState(() => {
    const draft = drafts.get(scopeKey);
    return Boolean(draft?.attachments?.length || draft?.uploads?.size);
  });
  const restoration = useRef<{
    composer: ThreadComposerRuntime;
    scopeKey: string;
    submission?: symbol;
    pending: Promise<unknown>;
    finished: boolean;
  }>(undefined);
  useEffect(() => {
    let active = true;
    let restoring = false;
    let previous:
      ReturnType<ThreadComposerRuntime["getState"]>["attachments"] | undefined;
    const save = () => {
      if (!active || restoring) return;
      const { attachments } = composer.getState();
      if (previous === attachments) return;
      previous = attachments;
      updateDraft(drafts, scopeKey, { attachments: composerFiles(composer) });
    };
    if (
      restoration.current?.composer !== composer ||
      restoration.current.scopeKey !== scopeKey ||
      restoration.current.submission !== restoredSubmission
    ) {
      const draft = drafts.get(scopeKey);
      if (draft?.attachments?.length || draft?.uploads?.size) {
        restoration.current = {
          composer,
          scopeKey,
          submission: restoredSubmission,
          finished: false,
          pending: (async () => {
            await Promise.allSettled(draft.uploads ?? []);
            const currentFiles = new Set(
              composerFiles(composer).map((file) => file.botSourcePath),
            );
            await Promise.allSettled(
              (drafts.get(scopeKey)?.attachments ?? [])
                .filter((file) => !currentFiles.has(file.botSourcePath))
                .map((file) => composer.addAttachment(file)),
            );
          })(),
        };
      }
    }
    const restore = restoration.current;
    if (
      restore?.composer === composer &&
      restore.scopeKey === scopeKey &&
      restore.submission === restoredSubmission
    ) {
      restoring = true;
      void restore.pending.then(() => {
        restore.finished = true;
        restoring = false;
        save();
        if (active) {
          setCompletedRestoration(restoredSubmission);
          if (!drafts.get(scopeKey)?.uploads?.size)
            setAttachmentsPending(false);
        }
      });
    } else {
      save();
    }
    const unsubscribe = composer.subscribe(save);
    return () => {
      active = false;
      unsubscribe();
    };
  }, [composer, drafts, restoredSubmission, scopeKey]);
  const addPastedFiles = async (
    files: readonly File[],
    onAdded: () => void,
    onError: () => void,
  ) => {
    setAttachmentsPending(true);
    const uploads = drafts.get(scopeKey)?.uploads ?? new Set<Promise<void>>();
    // 移交使用同一组未完成的上传。完成时以草稿当前归属为准。
    const owner = () =>
      [...drafts].find(([, draft]) => draft.uploads === uploads);
    const pending = (async () => {
      for (const file of files) {
        try {
          await composer.addAttachment(file);
          const sourcePath = (file as BotPathFile).botSourcePath;
          const draft = owner();
          if (
            draft &&
            sourcePath &&
            !draft[1].attachments?.some(
              (item) => item.botSourcePath === sourcePath,
            )
          ) {
            updateDraft(drafts, draft[0], {
              attachments: [
                ...(draft[1].attachments ?? []),
                pathFile(sourcePath, {
                  name: file.name,
                  contentType: file.type,
                }),
              ],
            });
          }
          onAdded();
        } catch {
          onError();
        }
      }
    })();
    uploads.add(pending);
    updateDraft(drafts, scopeKey, { uploads });
    try {
      await pending;
    } finally {
      uploads.delete(pending);
      const draft = owner();
      if (draft && uploads.size === 0)
        updateDraft(drafts, draft[0], { uploads: undefined });
      if (uploads.size === 0 && restoration.current?.finished !== false)
        setAttachmentsPending(false);
    }
  };
  return {
    addPastedFiles,
    attachmentsPending:
      attachmentsPending ||
      Boolean(
        restoredSubmission &&
        restoredSubmission !== completedRestoration &&
        drafts.get(scopeKey)?.attachments?.length,
      ),
  };
}

export function useChatWorkflowPicker(
  drafts: ChatComposerDrafts,
  scopeKey: string,
  enabled: boolean,
  { preserveTextDraft = false }: { preserveTextDraft?: boolean } = {},
) {
  const client = useQueryClient();
  const [{ workflow, workflowOnly }, setSelection] = useState<{
    workflow?: ChatWorkflowReference;
    workflowOnly: boolean;
  }>({ workflowOnly: false });
  const picker: ChatWorkflowPicker | undefined = enabled
    ? {
        draft: drafts.get(scopeKey)?.document,
        restoredSubmission: drafts.get(scopeKey)?.restoredSubmission,
        preserveTextDraft,
        saveDraft(document) {
          updateDraft(drafts, scopeKey, { document });
        },
        onSelectionChange(workflow, workflowOnly) {
          setSelection((current) =>
            current.workflow?.id === workflow?.id &&
            current.workflow?.name === workflow?.name &&
            current.workflowOnly === workflowOnly
              ? current
              : { workflow, workflowOnly },
          );
        },
        async search(text) {
          const search = text.trim().slice(0, 200);
          const matches: ChatWorkflowReference[] = [];
          let before: number | undefined;
          let beforeId: string | undefined;
          do {
            const page = await client.fetchQuery({
              ...listWorkflowsOptions(
                withLocalErrorSurface({
                  query: { search, limit: candidateLimit, before, beforeId },
                }),
              ),
              staleTime: 1_000,
            });
            for (const item of page.items ?? []) {
              if (item.id && item.currentRevisionId && item.title)
                matches.push({ id: item.id, name: item.title });
            }
            before = page.nextBefore;
            beforeId = page.nextBeforeId;
          } while (matches.length < candidateLimit && before && beforeId);
          return matches.slice(0, candidateLimit);
        },
      }
    : undefined;
  return { workflow, workflowOnly, picker };
}
