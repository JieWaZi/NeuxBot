import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { Field } from "./knowledge-controls";
import { knowledgeKeys } from "./knowledge-queries";
import type { ResourceRef } from "./knowledge-transfer";
import type { WikiTheme } from "./knowledge-wiki-queries";
import type { KnowledgeWikiCommands } from "./knowledge-wiki-command";
import { isKnowledgeWriteOutcomeUnknown } from "./knowledge-write-outcome";
import { reportKnowledgeError } from "./knowledge-error-notice";
import { useWikiCopy } from "./use-wiki-copy";

/** 目录命令固定打开时的对象和版本；失败保留名称，未知结果沿原身份对账。 */
export function KnowledgeWikiFolderDialog({
  resource,
  folder,
  kind,
  structureVersion,
  onClose,
  commands,
  commandKey,
}: {
  resource: ResourceRef;
  commands: KnowledgeWikiCommands;
  commandKey: string;
  folder: WikiTheme;
  kind: "rename" | "delete";
  structureVersion: number;
  onClose: () => void;
}) {
  const t = useWikiCopy();
  const client = useQueryClient();
  const [retained] = useState(() => commands.get(commandKey));
  const [unresolved, setUnresolved] = useState(Boolean(retained));
  const snapshot = retained?.theme ?? folder;
  const [version] = useState(retained?.structureVersion ?? structureVersion);
  const [name, setName] = useState(retained?.theme?.name ?? folder.name ?? "");
  const [command, setCommand] = useState<{ fingerprint: string; id: string }>();
  const [pending, setPending] = useState(false);
  const deleting = kind === "delete";
  async function submit() {
    if (pending || !folder.id || (!deleting && !name.trim())) return;
    const theme = { ...snapshot, name: name.trim() };
    const fingerprint = JSON.stringify(theme);
    const requestId =
      unresolved && retained
        ? retained.requestId
        : command?.fingerprint === fingerprint
          ? command.id
          : crypto.randomUUID();
    setCommand({ fingerprint, id: requestId });
    setPending(true);
    try {
      await knowledgeAdapter(resource).writeTheme({
        path: { libraryId: resource.knowledgeBaseId, themeId: folder.id },
        body: { requestId, structureVersion: version, theme, delete: deleting },
        throwOnError: true,
      });
      await Promise.all([
        client.invalidateQueries({
          queryKey: knowledgeKeys.resource(resource),
        }),
        client.invalidateQueries({ queryKey: knowledgeKeys.libraryLists }),
      ]);
      commands.delete(commandKey);
      setUnresolved(false);
      onClose();
    } catch (cause) {
      const unknown = isKnowledgeWriteOutcomeUnknown(cause);
      if (unknown)
        commands.set(commandKey, {
          kind: deleting ? "delete-folder" : "rename",
          requestId,
          structureVersion: version,
          theme,
        });
      else commands.delete(commandKey);
      setUnresolved(unknown);
      if (!unknown) setCommand(undefined);
      reportKnowledgeError(
        cause,
        `write-wiki-folder:${commandKey}`,
        t("writeFailed"),
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
        title={t(deleting ? "deleteFolder" : "renameFolder")}
        description={
          deleting
            ? t("deleteFolderHelp", { name: folder.name ?? "" })
            : t("renameFolderHelp")
        }
        size="sm"
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <div className="grid gap-4 px-6 pb-5">
            {!deleting && (
              <Field label={t("folderName")}>
                <Input
                  autoFocus
                  required
                  maxLength={255}
                  aria-label={t("folderName")}
                  placeholder={t("folderPlaceholder")}
                  value={name}
                  disabled={pending || unresolved}
                  onChange={(event) => setName(event.target.value)}
                />
              </Field>
            )}
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="secondary"
              disabled={pending}
              onClick={onClose}
            >
              {t("cancel")}
            </Button>
            <Button
              type="submit"
              variant={deleting ? "danger" : "primary"}
              disabled={pending || (!deleting && !name.trim())}
            >
              {t(deleting ? "deleteConfirm" : "save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
