import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Choice, Field } from "./knowledge-controls";
import { knowledgeKeys } from "./knowledge-queries";
import type { ResourceRef } from "./knowledge-transfer";
import type { WikiPage, WikiTheme } from "./knowledge-wiki-queries";
import type { KnowledgeWikiCommands } from "./knowledge-wiki-command";
import { isKnowledgeWriteOutcomeUnknown } from "./knowledge-write-outcome";
import { reportKnowledgeError } from "./knowledge-error-notice";
import { useWikiCopy } from "./use-wiki-copy";

/** 打开时固定版本；未知写入结果重用请求身份，失败不丢弃人工草稿。 */
export function KnowledgeWikiEditor({
  resource,
  kind,
  page,
  themes,
  parentId,
  structureVersion,
  onClose,
  onSaved,
  commands,
  commandKey,
}: {
  resource: ResourceRef;
  commands: KnowledgeWikiCommands;
  commandKey: string;
  kind: "folder" | "page" | "edit";
  page?: WikiPage;
  themes: WikiTheme[];
  parentId: string;
  structureVersion: number;
  onClose: () => void;
  onSaved: (id?: string) => void;
}) {
  const t = useWikiCopy();
  const client = useQueryClient();
  const [retained] = useState(() => commands.get(commandKey));
  const [unresolved, setUnresolved] = useState(Boolean(retained));
  const [snapshot] = useState(retained?.page ?? page);
  const [version] = useState(retained?.structureVersion ?? structureVersion);
  const [name, setName] = useState(retained?.draft?.name ?? page?.title ?? "");
  const [slug, setSlug] = useState(retained?.draft?.slug ?? page?.slug ?? "");
  const [summary, setSummary] = useState(
    retained?.draft?.summary ?? page?.summary ?? "",
  );
  const [content, setContent] = useState(
    retained?.draft?.content ?? page?.content ?? "",
  );
  const [parent, setParent] = useState(
    retained?.draft?.parent ?? page?.themeId ?? parentId,
  );
  const [type, setType] = useState(
    retained?.draft?.type ?? page?.pageType ?? "concept",
  );
  const [saving, setSaving] = useState(false);
  const request = useRef<{ fingerprint: string; id: string }>(
    retained ? { fingerprint: "", id: retained.requestId } : undefined,
  );
  const folder = kind === "folder";
  const title = folder
    ? t("createFolder")
    : kind === "edit"
      ? t("edit")
      : t("createPage");
  async function submit() {
    if (saving || !name.trim() || (!folder && !slug.trim()) || !version) return;
    const data = folder
      ? {
          structureVersion: version,
          theme: { name: name.trim(), parentId: parent, manual: true },
        }
      : {
          structureVersion: version,
          ...(kind === "edit" ? { expectedVersion: snapshot?.version } : {}),
          page: {
            ...snapshot,
            title: name.trim(),
            summary,
            content,
            ...(kind === "edit"
              ? {}
              : {
                  slug: slug.trim(),
                  themeId: parent,
                  pageType: type,
                  status: "published",
                }),
          },
        };
    const fingerprint = JSON.stringify(data);
    if (!unresolved && request.current?.fingerprint !== fingerprint)
      request.current = { fingerprint, id: crypto.randomUUID() };
    const requestId = request.current?.id ?? crypto.randomUUID();
    const command = {
      kind,
      requestId,
      structureVersion: version,
      page: snapshot,
      draft: { name, slug, summary, content, parent, type },
    };
    setSaving(true);
    try {
      const path = { libraryId: resource.knowledgeBaseId };
      const result = folder
        ? await knowledgeAdapter(resource).createTheme({
            path,
            body: { ...data, requestId },
            throwOnError: true,
          })
        : kind === "edit"
          ? await knowledgeAdapter(resource).writePage({
              path: { ...path, pageId: page?.id ?? "" },
              body: { ...data, requestId },
              throwOnError: true,
            })
          : await knowledgeAdapter(resource).createPage({
              path,
              body: { ...data, requestId },
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
      onSaved(folder ? undefined : (result.data.resourceId ?? page?.id));
    } catch (cause) {
      const unknown = isKnowledgeWriteOutcomeUnknown(cause);
      if (unknown) commands.set(commandKey, command);
      else commands.delete(commandKey);
      setUnresolved(unknown);
      reportKnowledgeError(cause, `write-wiki:${commandKey}`, t("writeFailed"));
    } finally {
      setSaving(false);
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !saving) onClose();
      }}
    >
      <DialogContent
        title={title}
        size={folder ? "sm" : "lg"}
        layout={folder ? "content" : "form"}
      >
        <form
          className="flex min-h-0 flex-1 flex-col"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <fieldset
            disabled={saving || unresolved}
            className="scrollbar-hidden grid min-h-0 gap-4 overflow-y-auto border-0 px-6 pb-5"
          >
            <Field
              label={folder ? t("folderName") : t("title")}
              hint={folder ? t("folderHelp") : t("titleHelp")}
            >
              <Input
                autoFocus
                required
                maxLength={255}
                aria-label={folder ? t("folderName") : t("title")}
                placeholder={
                  folder ? t("folderPlaceholder") : t("titlePlaceholder")
                }
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </Field>
            {kind !== "edit" && (
              <Field label={t("parent")}>
                <Choice
                  label={t("parent")}
                  value={parent || "__root__"}
                  onChange={(value) =>
                    setParent(value === "__root__" ? "" : value)
                  }
                  options={[
                    { value: "__root__", label: t("root") },
                    ...themes
                      .filter((f) => Boolean(f.id))
                      .map((f) => ({
                        value: f.id ?? "",
                        label: f.path || f.name || "",
                      })),
                  ]}
                />
              </Field>
            )}
            {!folder && (
              <>
                {kind !== "edit" && (
                  <>
                    <Field label={t("slug")} hint={t("slugHelp")}>
                      <Input
                        required
                        aria-label={t("slug")}
                        placeholder={t("slugPlaceholder")}
                        value={slug}
                        onChange={(event) => setSlug(event.target.value)}
                      />
                    </Field>
                    <Field label={t("pageType")}>
                      <Choice
                        value={type}
                        label={t("pageType")}
                        onChange={setType}
                        options={[
                          { value: "concept", label: t("concept") },
                          { value: "entity", label: t("entity") },
                          { value: "summary", label: t("summaryType") },
                          { value: "synthesis", label: t("synthesis") },
                          { value: "comparison", label: t("comparison") },
                          ...(type === "index"
                            ? [{ value: "index", label: t("indexType") }]
                            : []),
                        ]}
                      />
                    </Field>
                  </>
                )}
                <Field label={t("summary")} hint={t("summaryHelp")}>
                  <Textarea
                    aria-label={t("summary")}
                    placeholder={t("summaryPlaceholder")}
                    value={summary}
                    onChange={(event) => setSummary(event.target.value)}
                  />
                </Field>
                <Field label={t("content")} hint={t("contentHelp")}>
                  <Textarea
                    aria-label={t("content")}
                    placeholder={t("contentPlaceholder")}
                    value={content}
                    onChange={(event) => setContent(event.target.value)}
                    rows={12}
                  />
                </Field>
              </>
            )}
          </fieldset>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              disabled={saving}
              onClick={onClose}
            >
              {t("cancel")}
            </Button>
            <Button
              type="submit"
              disabled={saving || !name.trim() || (!folder && !slug.trim())}
            >
              {kind === "edit" ? t("save") : t("create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
