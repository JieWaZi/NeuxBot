import { useRef, useState } from "react";
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

/** 确认时固定目标和并发版本；未知结果关闭重开后仍核对原请求。 */
export function KnowledgeWikiRestore({
  resource,
  page,
  revisionVersion,
  structureVersion,
  commands,
  commandKey,
  onClose,
  onRestored,
}: {
  resource: ResourceRef;
  page: WikiPage;
  revisionVersion: number;
  structureVersion: number;
  commands: KnowledgeWikiCommands;
  commandKey: string;
  onClose: () => void;
  onRestored: () => void;
}) {
  const t = useWikiCopy();
  const client = useQueryClient();
  const [command] = useState(
    () =>
      commands.get(commandKey) ?? {
        kind: "restore-page" as const,
        requestId: crypto.randomUUID(),
        page,
        revisionVersion,
        structureVersion,
      },
  );
  const busy = useRef(false);
  const [pending, setPending] = useState(false);
  async function restore() {
    if (
      busy.current ||
      !command.page?.id ||
      !command.page.version ||
      !command.structureVersion ||
      !command.revisionVersion
    )
      return;
    busy.current = true;
    setPending(true);
    // 在网络请求前登记，异步期间切换视图也不会丢失核对入口。
    commands.set(commandKey, command);
    try {
      await knowledgeAdapter(resource).restorePage({
        path: {
          libraryId: resource.knowledgeBaseId,
          pageId: command.page.id,
        },
        body: {
          requestId: command.requestId,
          expectedVersion: command.page.version,
          revisionVersion: command.revisionVersion,
          structureVersion: command.structureVersion,
        },
        throwOnError: true,
      });
      commands.delete(commandKey);
      await Promise.all([
        client.invalidateQueries({
          queryKey: knowledgeKeys.resource(resource),
        }),
        client.invalidateQueries({ queryKey: knowledgeKeys.libraryLists }),
      ]);
      onRestored();
    } catch (cause) {
      if (!isKnowledgeWriteOutcomeUnknown(cause)) commands.delete(commandKey);
      reportKnowledgeError(
        cause,
        `restore-wiki-page:${commandKey}`,
        t("restoreFailed"),
      );
    } finally {
      busy.current = false;
      setPending(false);
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy.current) onClose();
      }}
    >
      <DialogContent
        title={t("restoreTitle")}
        description={t("restoreHelp", {
          title: command.page?.title ?? "",
          version: command.revisionVersion ?? 0,
        })}
        size="sm"
      >
        <DialogFooter>
          <Button variant="secondary" disabled={pending} onClick={onClose}>
            {t("cancel")}
          </Button>
          <Button
            disabled={
              pending || !command.page?.version || !command.structureVersion
            }
            onClick={() => {
              void restore();
            }}
          >
            {t("restoreConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
