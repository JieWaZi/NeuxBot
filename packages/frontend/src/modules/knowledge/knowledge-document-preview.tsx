import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Button } from "@/shared/ui/button";
import { StaticMarkdown } from "@/shared/ui/static-markdown";
import { KnowledgeWikiContentLoading } from "./knowledge-loading";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";
import {
  sourceDetailOptions,
  sourceContentOptions,
} from "./knowledge-source-document";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { toSource } from "./knowledge-queries";
import { useCopy } from "./use-copy";
import { useDocumentCopy } from "./use-document-copy";
import type { Source } from "./knowledge-model";
import type { ResourceRef } from "./knowledge-transfer";

export function KnowledgeDocumentPreview({
  resource,
  source,
  onClose,
  onDownload,
  downloading,
  presentation = "dialog",
  actions,
}: {
  resource: ResourceRef;
  source: Source;
  onClose: () => void;
  onDownload: () => void;
  downloadError?: unknown;
  downloading?: boolean;
  presentation?: "dialog" | "pane";
  actions?: ReactNode;
}) {
  const t = useDocumentCopy();
  const copy = useCopy();
  const { i18n } = useTranslation("knowledgeDocuments");
  const [unresolvedLink, setUnresolvedLink] = useState(false);
  const active = useRouteSceneActive();
  const detail = useQuery({
    ...sourceDetailOptions(resource, source.id),
    enabled: active,
  });
  const content = useQuery({
    ...sourceContentOptions(
      resource,
      source.id,
      detail.data?.filename || source.name,
    ),
    enabled: active && Boolean(detail.data?.filename || source.name),
  });
  const documentSource = detail.data
    ? toSource(detail.data, resource.backend)
    : source;
  const body = (
    <div
      className={
        presentation === "pane"
          ? "min-w-0 space-y-6"
          : "scrollbar-hidden min-h-0 flex-1 space-y-6 overflow-y-auto px-6 pb-5"
      }
    >
      <KnowledgeReadFeedback
        hasData={Boolean(detail.data)}
        state={{
          pending: detail.isPending,
          error: detail.error,
          retry: () => {
            void detail.refetch();
          },
        }}
        loading={<KnowledgeWikiContentLoading />}
      />
      {detail.data && (
        <>
          <section className="space-y-3">
            <h2 className="type-title font-semibold">{t("basic")}</h2>
            <dl className="type-body flex flex-wrap gap-x-8 gap-y-3">
              {[
                [
                  t("uploaded"),
                  detail.data.createdAt
                    ? new Intl.DateTimeFormat(i18n.language, {
                        dateStyle: "medium",
                        timeStyle: "short",
                      }).format(new Date(detail.data.createdAt))
                    : documentSource.date,
                ],
                [
                  t("fileType"),
                  detail.data.mime ||
                    documentSource.name.split(".").pop() ||
                    "—",
                ],
                [t("fileSize"), documentSource.size],
                [copy("state"), copy(documentSource.state)],
              ].map(([label, value]) => (
                <div key={label} className="space-y-1">
                  <dt className="text-content-secondary">{label}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
            </dl>
          </section>
          {detail.data.description?.trim() && (
            <section className="space-y-3">
              <h2 className="type-title font-semibold">{t("summary")}</h2>
              <p className="type-body text-content-secondary whitespace-pre-wrap">
                {detail.data.description}
              </p>
            </section>
          )}
        </>
      )}
      <section className="space-y-3">
        <h2 className="type-title font-semibold">{t("content")}</h2>
        <KnowledgeReadFeedback
          hasData={Boolean(content.data)}
          state={{
            pending: content.isPending,
            error: content.error,
            retry: () => {
              void content.refetch();
            },
          }}
          loading={<KnowledgeWikiContentLoading />}
        />
        {unresolvedLink && (
          <p role="status" className="type-body text-content-secondary">
            {t("linkUnavailable")}
          </p>
        )}
        {content.data &&
          (content.data.text ? (
            content.data.markdown ? (
              <StaticMarkdown
                source={content.data.text}
                onLink={() => setUnresolvedLink(true)}
              />
            ) : (
              <pre className="type-body font-sans break-words whitespace-pre-wrap">
                {content.data.text}
              </pre>
            )
          ) : (
            <p className="type-body text-content-secondary">{t("noPreview")}</p>
          ))}
      </section>
    </div>
  );
  if (presentation === "pane")
    return (
      <article
        aria-label={documentSource.name || source.name || copy("documents")}
        className="min-w-0"
      >
        <header className="mb-5 space-y-3">
          <div className="flex min-w-0 flex-wrap items-center gap-3">
            <h2
              title={documentSource.name || source.name}
              className="type-title min-w-0 flex-1 truncate font-semibold"
            >
              {documentSource.name || source.name || copy("documents")}
            </h2>
            {actions}
          </div>
        </header>
        {body}
      </article>
    );
  return (
    <Dialog
      open={active}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent
        title={source.name || copy("documents")}
        size="xl"
        layout="form"
      >
        {body}
        <DialogFooter>
          <Button variant="secondary" onClick={onClose}>
            {t("close")}
          </Button>
          <Button disabled={downloading} onClick={onDownload}>
            {t("download")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
