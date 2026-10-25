import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { knowledgeKeys } from "./knowledge-queries";
import type { ResourceRef } from "./knowledge-transfer";
import type { WikiPage } from "./knowledge-wiki-queries";
import type { KnowledgeWikiCommands } from "./knowledge-wiki-command";
import { isKnowledgeWriteOutcomeUnknown } from "./knowledge-write-outcome";
import { reportKnowledgeError } from "./knowledge-error-notice";
import { useWikiCopy } from "./use-wiki-copy";

export function KnowledgeWikiDelete({
  resource,
  page,
  structureVersion,
  onClose,
  onDeleted,
  commands,
  commandKey,
}: {
  resource: ResourceRef;
  commands: KnowledgeWikiCommands;
  commandKey: string;
  page: WikiPage;
  structureVersion: number;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const t = useWikiCopy();
  const client = useQueryClient();
  const [retained] = useState(() => commands.get(commandKey));
  const snapshot = retained?.page ?? page;
  const [requestId] = useState(
    () => retained?.requestId ?? crypto.randomUUID(),
  );
  const [version] = useState(retained?.structureVersion ?? structureVersion);
  const [pending, setPending] = useState(false);
  async function remove() {
    if (pending || !page.id || !page.version || !version) return;
    setPending(true);
    try {
      // 沿用现有归档协议移除页面，保留完整正文、证据和历史版本。
      await knowledgeAdapter(resource).writePage({
        path: { libraryId: resource.knowledgeBaseId, pageId: page.id },
        body: {
          requestId,
          expectedVersion: snapshot.version,
          structureVersion: version,
          page: { ...snapshot, status: "archived" },
        },
        throwOnError: true,
      });
      commands.delete(commandKey);
      onDeleted();
      await Promise.all([
        client.invalidateQueries({
          queryKey: knowledgeKeys.resource(resource),
        }),
        client.invalidateQueries({ queryKey: knowledgeKeys.libraryLists }),
      ]);
    } catch (cause) {
      if (isKnowledgeWriteOutcomeUnknown(cause))
        commands.set(commandKey, {
          kind: "delete-page",
          requestId,
          structureVersion: version,
          page: snapshot,
        });
      else commands.delete(commandKey);
      reportKnowledgeError(
        cause,
        `delete-wiki-page:${commandKey}`,
        t("deleteFailed"),
      );
    } finally {
      setPending(false);
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <DialogContent
        title={t("deletePage")}
        description={t("deleteHelp", { title: page.title ?? "" })}
        size="sm"
      >
        <DialogFooter>
          <Button variant="secondary" disabled={pending} onClick={onClose}>
            {t("cancel")}
          </Button>
          <Button
            variant="danger"
            disabled={pending}
            onClick={() => {
              void remove();
            }}
          >
            {t("deleteConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
