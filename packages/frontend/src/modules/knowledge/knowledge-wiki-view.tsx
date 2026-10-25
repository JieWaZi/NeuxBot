import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import { trackHorizontalDrag } from "@/shared/lib/horizontal-drag";
import { VerticalResizeHandle } from "@/shared/ui/vertical-resize-handle";
import { useQuery } from "@tanstack/react-query";
import { History, Pencil, Trash2, Sparkles } from "lucide-react";
import { defaultRemarkPlugins } from "streamdown";
import {
  remarkWikiLinks,
  normalizeWikiIndexIntro,
  stripDuplicateWikiTitle,
} from "./knowledge-wiki-markdown";
import { DetailDockScrollArea } from "@/shared/ui/detail-dock-layout";
import { Badge } from "@/shared/ui/badge";
import { Separator } from "@/shared/ui/separator";
import { PageAction } from "@/shared/ui/resource-page";
import { IconAction } from "./knowledge-controls";
import { KnowledgeWikiStructuredIndex } from "./knowledge-wiki-index";
import { KnowledgeWikiRemoteIndex } from "./knowledge-wiki-remote-index";
import { KnowledgeWikiHistory } from "./knowledge-wiki-history";
import { KnowledgeWikiDelete } from "./knowledge-wiki-delete";
import { WikiTime } from "./knowledge-wiki-time";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import {
  KnowledgeWikiDirectoryLoading,
  KnowledgeWikiContentLoading,
} from "./knowledge-loading";
import { StaticMarkdown } from "@/shared/ui/static-markdown";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import type { ResourceRef } from "./knowledge-transfer";
import {
  wikiPagesOptions,
  wikiThemesOptions,
  wikiPageOptions,
  type WikiPage,
} from "./knowledge-wiki-queries";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";
import { KnowledgeWikiTree } from "./knowledge-wiki-tree";
import { useWikiCopy } from "./use-wiki-copy";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { reportKnowledgeError } from "./knowledge-error-notice";
import { KnowledgeWikiFolderDialog } from "./knowledge-wiki-folder-dialog";
import type { KnowledgeWikiCommands } from "./knowledge-wiki-command";
import { resourceKey } from "./knowledge-transfer";
import type { WikiTheme } from "./knowledge-wiki-queries";
import { KnowledgeWikiEditor } from "./knowledge-wiki-editor";
import { KnowledgeWikiReferences } from "./knowledge-wiki-references";

const wikiRemarkPlugins = [
  ...Object.values(defaultRemarkPlugins),
  remarkWikiLinks,
];

export function KnowledgeWikiView({
  resource,
  pageId,
  onPage,
  writable,
  maintenanceDisabled = false,
  structureVersion,
  commands: sharedCommands,
  onMaintenance,
  content,
  directoryTab,
  onDirectoryTab,
  sourceCount,
  sourceDirectory,
  sourceQuery,
  onSourceQuery,
  view = "wiki",
}: {
  resource: ResourceRef;
  pageId: string;
  onPage: (id: string, tab?: string) => void;
  writable: boolean;
  maintenanceDisabled?: boolean;
  structureVersion: number;
  commands?: KnowledgeWikiCommands;
  onMaintenance: (page: WikiPage) => void;
  content?: ReactNode;
  directoryTab?: string;
  onDirectoryTab?: (tab: string) => void;
  sourceCount?: number;
  sourceDirectory?: ReactNode;
  sourceQuery?: string;
  onSourceQuery?: (query: string) => void;
  view?: "raw" | "wiki" | "graph";
}) {
  const t = useWikiCopy();
  const adapter = knowledgeAdapter(resource);
  const [localCommands] = useState<KnowledgeWikiCommands>(() => new Map());
  const commands = sharedCommands ?? localCommands;
  const keyFor = (operation: string) => `${resourceKey(resource)}:${operation}`;
  const beginMaintenance = (targetPage: WikiPage) =>
    void act("maintenanceChat", () => onMaintenance(targetPage));
  const [folderAction, setFolderAction] = useState<{
    kind: "rename" | "delete";
    folder: WikiTheme;
    commandKey?: string;
  }>();
  async function act(
    capability: string,
    callback: () => void,
    commandKey?: string,
  ) {
    try {
      if (
        !adapter.localConfiguration &&
        (!commandKey || !commands.has(commandKey))
      )
        await adapter.assertCapability(capability);
      callback();
    } catch (cause) {
      reportKnowledgeError(
        cause,
        `wiki-action:${keyFor(capability)}`,
        t("writeFailed"),
      );
    }
  }
  const active = useRouteSceneActive();
  const splitRef = useRef<HTMLDivElement>(null);
  const resizeCleanup = useRef<() => void>(() => undefined);
  const [directoryWidth, setDirectoryWidth] = useState(288);
  useEffect(() => () => resizeCleanup.current(), []);
  const maxDirectoryWidth = () =>
    Math.max(224, Math.min(480, (splitRef.current?.clientWidth || 800) - 320));
  const clampWidth = (width: number) =>
    Math.max(224, Math.min(maxDirectoryWidth(), width));
  const pages = useQuery({ ...wikiPagesOptions(resource), enabled: active });
  const themes = useQuery({ ...wikiThemesOptions(resource), enabled: active });
  const index = pages.data?.find((p) => p.pageType === "index");
  const summaryMode = directoryTab === "summaries";
  const selectedId =
    pageId ||
    (summaryMode
      ? pages.data?.find((entry) => entry.pageType === "summary")?.id
      : index?.id) ||
    "";
  const page = useQuery({
    ...wikiPageOptions(
      resource,
      selectedId,
      pages.data?.find((entry) => entry.id === selectedId)?.slug,
    ),
    enabled:
      active &&
      content === undefined &&
      Boolean(selectedId) &&
      (adapter.revisionsIncludeContent || Boolean(pages.data)),
  });
  const [folder, setFolder] = useState("");
  const [editor, setEditor] = useState<{
    kind: "folder" | "page" | "edit";
    page?: WikiPage;
    commandKey?: string;
  }>();
  const [missingLink, setMissingLink] = useState(false);
  const [dialog, setDialog] = useState<{
    kind: "history" | "delete";
    page: WikiPage;
  }>();
  const indexNavigation = useRef(0);
  useEffect(
    () => () => {
      indexNavigation.current += 1;
    },
    [
      resource.backend,
      resource.connectionId,
      resource.knowledgeBaseId,
      active,
      pageId,
      view,
      directoryTab,
    ],
  );
  function navigate(id: string) {
    indexNavigation.current += 1;
    setMissingLink(false);
    onPage(
      id,
      pages.data?.find((entry) => entry.id === id)?.pageType === "summary"
        ? "summaries"
        : "knowledge",
    );
  }
  function follow(href: string, entries = pages.data) {
    let slug: string;
    try {
      slug = decodeURIComponent(href.split("#")[0] ?? "")
        .replace(/^\.\//, "")
        .replace(/^\//, "")
        .replace(/\.md$/, "");
    } catch {
      setMissingLink(true);
      return;
    }
    if (!slug) return;
    const found = entries?.find(
      (p) => p.slug === slug || p.id === slug || p.title === slug,
    );
    if (found?.id) navigate(found.id);
    else setMissingLink(true);
  }
  async function followIndex(href: string) {
    const attempt = ++indexNavigation.current;
    setMissingLink(false);
    let entries = pages.data;
    if (!entries) {
      // 轻量索引可先于目录到达；复用同一查询等待完整metadata，读取失败由原目录提供重试。
      const result = await pages.refetch({ cancelRefetch: false });
      if (result.isError || !result.data) return;
      entries = result.data;
    }
    // 切库、切页、失活或后续导航使旧等待失效，不能拿旧库结果导航。
    if (attempt !== indexNavigation.current) return;
    follow(href, entries);
  }
  const indexPage = page.data?.pageType === "index";
  const pageLabel = indexPage ? t("index") : page.data?.title;
  const source = indexPage
    ? normalizeWikiIndexIntro(page.data?.content ?? "")
    : stripDuplicateWikiTitle(page.data?.content ?? "", page.data?.title ?? "");
  const structuredIndex =
    !summaryMode &&
    (!pageId || pageId === index?.id || page.data?.pageType === "index");
  const failed = pages.error || themes.error;
  const editable = Boolean(
    writable && !page.error && page.data?.id && page.data.pageType !== "index",
  );
  const currentPage = page.data;
  const pageActions = currentPage?.id ? (
    <>
      {editable && (
        <IconAction
          icon={Sparkles}
          label={t("aiRevise")}
          disabled={maintenanceDisabled}
          onClick={() => beginMaintenance(currentPage)}
        />
      )}
      {editable && (
        <IconAction
          icon={Pencil}
          label={t("edit")}
          onClick={() =>
            void act(
              "write",
              () =>
                setEditor({
                  kind: "edit",
                  page: page.data,
                  commandKey: keyFor(`page:edit:${page.data?.id}`),
                }),
              keyFor(`page:edit:${page.data?.id}`),
            )
          }
        />
      )}
      <IconAction
        icon={History}
        label={t("history")}
        onClick={() => setDialog({ kind: "history", page: currentPage })}
      />
      {editable && (
        <PageAction
          intent="delete"
          label={t("deletePage")}
          onClick={() =>
            void act(
              "write",
              () => setDialog({ kind: "delete", page: currentPage }),
              keyFor(`page:delete:${currentPage.id}`),
            )
          }
        >
          <Trash2 />
        </PageAction>
      )}
    </>
  ) : undefined;
  const typeLabel =
    page.data?.pageType === "index"
      ? "indexType"
      : page.data?.pageType === "summary"
        ? "summaryType"
        : page.data?.pageType === "entity"
          ? "entity"
          : page.data?.pageType === "concept"
            ? "concept"
            : page.data?.pageType === "synthesis"
              ? "synthesis"
              : page.data?.pageType === "comparison"
                ? "comparison"
                : "page";
  return (
    <>
      <section
        aria-label="Wiki"
        data-motion="presence"
        data-state="open"
        className="@container/wiki flex min-h-0 flex-1 flex-col overflow-hidden"
      >
        <div
          ref={splitRef}
          style={
            { "--wiki-directory-width": `${directoryWidth}px` } as CSSProperties
          }
          className="grid min-h-0 flex-1 grid-rows-[minmax(12rem,35%)_minmax(0,1fr)] @[42rem]/wiki:grid-cols-[minmax(14rem,min(var(--wiki-directory-width),calc(100%_-_20rem)))_1px_minmax(0,1fr)] @[42rem]/wiki:grid-rows-[minmax(0,1fr)]"
        >
          <aside
            aria-label={t("directory")}
            className="border-border-subtle flex min-h-0 flex-col border-b pb-3 @[42rem]/wiki:border-b-0 @[42rem]/wiki:pr-4 @[42rem]/wiki:pb-0"
          >
            {Array.from(commands.entries())
              .filter(([key]) => key.startsWith(`${resourceKey(resource)}:`))
              .map(([key, command]) => (
                <Button
                  key={key}
                  variant="text"
                  className="justify-start"
                  onClick={() => {
                    if (command.theme)
                      setFolderAction({
                        kind: command.kind === "rename" ? "rename" : "delete",
                        folder: command.theme,
                        commandKey: key,
                      });
                    else if (command.kind === "restore-page" && command.page)
                      setDialog({ kind: "history", page: command.page });
                    else if (command.kind === "delete-page" && command.page)
                      setDialog({ kind: "delete", page: command.page });
                    else {
                      setFolder(command.draft?.parent ?? "");
                      setEditor({
                        kind:
                          command.kind === "edit"
                            ? "edit"
                            : command.kind === "folder"
                              ? "folder"
                              : "page",
                        page: command.page,
                        commandKey: key,
                      });
                    }
                  }}
                >
                  {t("reconcileCommand", {
                    name:
                      command.theme?.name ??
                      command.page?.title ??
                      command.draft?.name ??
                      "",
                  })}
                </Button>
              ))}
            {failed && pages.data && themes.data && (
              <KnowledgeReadFeedback
                hasData
                state={{
                  pending: false,
                  error: failed,
                  retry: () => {
                    void pages.refetch();
                    void themes.refetch();
                  },
                }}
              />
            )}
            <KnowledgeWikiTree
              tab={directoryTab}
              onTabChange={onDirectoryTab}
              sourceCount={sourceCount}
              sourceDirectory={sourceDirectory}
              sourceQuery={sourceQuery}
              onSourceQuery={onSourceQuery}
              feedback={
                pages.isPending || themes.isPending ? (
                  <KnowledgeWikiDirectoryLoading />
                ) : failed && (!pages.data || !themes.data) ? (
                  <KnowledgeReadFeedback
                    state={{
                      pending: false,
                      error: failed,
                      retry: () => {
                        void pages.refetch();
                        void themes.refetch();
                      },
                    }}
                  />
                ) : undefined
              }
              pages={pages.data ?? []}
              themes={themes.data ?? []}
              selectedPage={selectedId}
              onPage={navigate}
              onFolder={setFolder}
              onCreate={(kind) =>
                void act(
                  kind === "folder" ? "themeWrite" : "write",
                  () =>
                    setEditor({
                      kind,
                      commandKey: keyFor(`${kind}:create:${folder}`),
                    }),
                  keyFor(`${kind}:create:${folder}`),
                )
              }
              onFolderAction={(kind, target) =>
                void act(
                  "themeWrite",
                  () => {
                    if (kind === "create") {
                      setFolder(target.id ?? "");
                      setEditor({
                        kind: "folder",
                        commandKey: keyFor(`folder:create:${target.id}`),
                      });
                    } else
                      setFolderAction({
                        kind,
                        folder: target,
                        commandKey: keyFor(`folder:${kind}:${target.id}`),
                      });
                  },
                  keyFor(
                    kind === "create"
                      ? `folder:create:${target.id}`
                      : `folder:${kind}:${target.id}`,
                  ),
                )
              }
              writable={writable && !failed}
            />
          </aside>
          <VerticalResizeHandle
            className="hidden @[42rem]/wiki:block"
            ariaLabel={t("resizeDirectory")}
            ariaValueMin={224}
            ariaValueMax={480}
            ariaValueNow={directoryWidth}
            onKeyDown={(event) => {
              const width =
                event.key === "Home"
                  ? 224
                  : event.key === "End"
                    ? maxDirectoryWidth()
                    : event.key === "ArrowLeft"
                      ? directoryWidth - 8
                      : event.key === "ArrowRight"
                        ? directoryWidth + 8
                        : undefined;
              if (width === undefined) return;
              event.preventDefault();
              setDirectoryWidth(clampWidth(width));
            }}
            onMouseDown={(event) => {
              if (event.button !== 0) return;
              event.preventDefault();
              resizeCleanup.current();
              const startX = event.clientX;
              const startWidth = Math.min(directoryWidth, maxDirectoryWidth());
              resizeCleanup.current = trackHorizontalDrag({
                initialX: startX,
                initialValue: startWidth,
                valueAt: (x) => clampWidth(startWidth + x - startX),
                onChange: setDirectoryWidth,
                onCommit: setDirectoryWidth,
                onFinish: () => {
                  resizeCleanup.current = () => undefined;
                },
              });
            }}
          />
          <DetailDockScrollArea asChild>
            <article
              aria-label={
                content !== undefined
                  ? undefined
                  : structuredIndex
                    ? t("index")
                    : pageLabel || t("index")
              }
              className="flex min-h-0 flex-col pt-4 @[42rem]/wiki:pt-0 @[42rem]/wiki:pl-6"
            >
              {content ?? (
                <>
                  {!structuredIndex && page.error && page.data && (
                    <KnowledgeReadFeedback
                      hasData
                      state={{
                        pending: false,
                        error: page.error,
                        retry: () => {
                          void page.refetch();
                        },
                      }}
                    />
                  )}
                  {structuredIndex ? (
                    <>
                      {missingLink && (
                        <p
                          role="status"
                          className="type-body text-muted-foreground mb-4"
                        >
                          {t("missingLink")}
                        </p>
                      )}
                      {adapter.localConfiguration ? (
                        <KnowledgeWikiStructuredIndex
                          resource={resource}
                          actions={pageActions}
                          active={active}
                          onLink={(href) => {
                            void followIndex(href);
                          }}
                          remarkPlugins={wikiRemarkPlugins}
                        />
                      ) : (
                        <KnowledgeWikiRemoteIndex
                          resource={resource}
                          active={active}
                          actions={pageActions}
                          onLink={(href) => {
                            void followIndex(href);
                          }}
                          remarkPlugins={wikiRemarkPlugins}
                        />
                      )}
                    </>
                  ) : page.error && !page.data ? (
                    <>
                      <KnowledgeReadFeedback
                        state={{
                          pending: false,
                          error: page.error,
                          retry: () => void page.refetch(),
                        }}
                      />
                      <Button variant="secondary" onClick={() => navigate("")}>
                        {t("backIndex")}
                      </Button>
                    </>
                  ) : summaryMode && !selectedId && !pages.isPending ? (
                    <EmptyState
                      title={t("emptySummaries")}
                      description={t("emptySummariesHelp")}
                    />
                  ) : !page.data &&
                    (pages.isPending || (selectedId && page.isPending)) ? (
                    <KnowledgeWikiContentLoading />
                  ) : page.data ? (
                    <>
                      <header className="mb-5 space-y-3">
                        <div className="flex min-w-0 items-center gap-3">
                          <h2
                            title={pageLabel}
                            className="type-title min-w-0 flex-1 truncate font-semibold"
                          >
                            {pageLabel}
                          </h2>
                          <div className="flex shrink-0 items-center gap-2">
                            {pageActions}
                          </div>
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                          <Badge variant="secondary">{t(typeLabel)}</Badge>
                          {page.data.version ? (
                            <Badge variant="secondary">
                              {t("version", { version: page.data.version })}
                            </Badge>
                          ) : null}
                          <WikiTime
                            value={page.data.updatedAt}
                            label={(time) => t("updatedAt", { time })}
                          />
                        </div>
                        {page.data.summary && (
                          <p className="type-body text-content-secondary">
                            {page.data.summary}
                          </p>
                        )}
                        <Separator className="mt-5" />
                      </header>
                      {missingLink && (
                        <p
                          role="status"
                          className="type-body text-muted-foreground mb-4"
                        >
                          {t("missingLink")}
                        </p>
                      )}
                      {source ? (
                        <StaticMarkdown
                          key={`${selectedId}:${page.data.version ?? 0}`}
                          source={source}
                          remarkPlugins={wikiRemarkPlugins}
                          onLink={follow}
                        />
                      ) : (
                        <p className="type-body text-muted-foreground">
                          {t("noContent")}
                        </p>
                      )}
                      <KnowledgeWikiReferences
                        key={`references:${selectedId}`}
                        resource={resource}
                        page={page.data}
                        pages={pages.data ?? []}
                        onLink={follow}
                      />
                    </>
                  ) : !pages.isPending &&
                    !failed &&
                    Boolean(pages.data?.length) ? (
                    <KnowledgeWikiIndex
                      pages={pages.data ?? []}
                      onPage={navigate}
                    />
                  ) : !pages.isPending && !failed ? (
                    <EmptyState
                      title={t("empty")}
                      description={t("emptyHelp")}
                    />
                  ) : null}
                </>
              )}
            </article>
          </DetailDockScrollArea>
        </div>
        {folderAction && (
          <KnowledgeWikiFolderDialog
            commands={commands}
            commandKey={
              folderAction.commandKey ??
              keyFor(`folder:${folderAction.kind}:${folderAction.folder.id}`)
            }
            resource={resource}
            folder={folderAction.folder}
            kind={folderAction.kind}
            structureVersion={structureVersion}
            onClose={() => setFolderAction(undefined)}
          />
        )}
        {editor && (
          <KnowledgeWikiEditor
            commands={commands}
            commandKey={
              editor.commandKey ??
              keyFor(
                editor.kind === "edit"
                  ? `page:edit:${editor.page?.id}`
                  : `${editor.kind}:create:${folder}`,
              )
            }
            resource={resource}
            kind={editor.kind}
            page={editor.page}
            themes={themes.data ?? []}
            parentId={folder}
            structureVersion={structureVersion}
            onClose={() => setEditor(undefined)}
            onSaved={(id) => {
              setEditor(undefined);
              if (id) navigate(id);
            }}
          />
        )}
      </section>
      {dialog?.kind === "history" && (
        <KnowledgeWikiHistory
          resource={resource}
          page={
            page.data && page.data.id === dialog.page.id
              ? page.data
              : dialog.page
          }
          writable={writable}
          structureVersion={structureVersion}
          commands={commands}
          commandKey={keyFor(`page:restore:${dialog.page.id}`)}
          onClose={() => setDialog(undefined)}
          onRestored={() => {
            setDialog(undefined);
            navigate(dialog.page.id ?? "");
          }}
        />
      )}
      {dialog?.kind === "delete" && (
        <KnowledgeWikiDelete
          commands={commands}
          commandKey={keyFor(`page:delete:${dialog.page.id}`)}
          resource={resource}
          page={dialog.page}
          structureVersion={structureVersion}
          onClose={() => setDialog(undefined)}
          onDeleted={() => {
            setDialog(undefined);
            navigate("");
          }}
        />
      )}
    </>
  );
}

/** 尚无持久索引时仅汇总真实目录元数据，不伪造 Wiki 正文。 */
function KnowledgeWikiIndex({
  pages,
  onPage,
}: {
  pages: WikiPage[];
  onPage: (id: string) => void;
}) {
  const t = useWikiCopy();
  const groups = new Map<string, WikiPage[]>();
  for (const page of pages) {
    const kind = page.pageType ?? "concept";
    groups.set(kind, [...(groups.get(kind) ?? []), page]);
  }
  return (
    <div className="space-y-6">
      <h2 className="type-title font-semibold">{t("index")}</h2>
      {Array.from(groups, ([kind, rows]) => (
        <section key={kind} className="space-y-3">
          <h3 className="type-label font-semibold">
            {kind === "summary"
              ? t("summaryType")
              : kind === "entity"
                ? t("entity")
                : kind === "synthesis"
                  ? t("synthesis")
                  : kind === "comparison"
                    ? t("comparison")
                    : t("concept")}{" "}
            ({rows.length})
          </h3>
          <ul className="space-y-3">
            {rows.map((page) => (
              <li key={page.id} className="type-body">
                <button
                  type="button"
                  className="text-link hover:text-link-hover focus-visible:ring-ring rounded-sm text-left underline underline-offset-4 outline-none focus-visible:ring-2"
                  aria-label={t("readPage", {
                    title: page.title ?? page.slug ?? "",
                  })}
                  onClick={() => {
                    if (page.id) onPage(page.id);
                  }}
                >
                  {page.title || page.slug}
                </button>
                {page.summary && (
                  <span className="text-content-secondary">
                    {" "}
                    — {page.summary}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
