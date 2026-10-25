import { useId, useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/shared/ui/button";
import { Separator } from "@/shared/ui/separator";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { KnowledgeResultDocument } from "./knowledge-result-document";
import { sourceDetailOptions } from "./knowledge-source-document";
import type { ResourceRef } from "./knowledge-transfer";
import type { WikiPage } from "./knowledge-wiki-queries";
import { useWikiCopy } from "./use-wiki-copy";

/** 与 Lite 阅读页一致，展示权威入链和来源引用；原件复用统一预览。 */
export function KnowledgeWikiReferences({
  resource,
  page,
  pages,
  onLink,
}: {
  resource: ResourceRef;
  page: WikiPage;
  pages: WikiPage[];
  onLink: (slug: string) => void;
}) {
  const t = useWikiCopy();
  const [sourceId, setSourceId] = useState("");
  const [incomingExpanded, setIncomingExpanded] = useState(false);
  const [sourcesExpanded, setSourcesExpanded] = useState(false);
  const incomingId = useId();
  const sourcesId = useId();
  const incoming = [...new Set(page.inLinks ?? [])];
  const sources = [...new Set(page.sourceRefs ?? [])];
  if (!incoming.length && !sources.length) return null;
  return (
    <>
      <footer className="mt-8 space-y-4" aria-label={t("references")}>
        <Separator />
        {incoming.length > 0 && (
          <section className="space-y-2" aria-labelledby={incomingId}>
            <h3 id={incomingId} className="type-label text-content-secondary">
              {t("linkedFrom")}
            </h3>
            <ul className="type-body flex flex-wrap gap-x-4 gap-y-1">
              {(incomingExpanded ? incoming : incoming.slice(0, 30)).map(
                (slug) => (
                  <li key={slug} className="max-w-full min-w-0">
                    <ReferenceButton
                      label={
                        pages.find((item) => item.slug === slug)?.title || slug
                      }
                      onClick={() => onLink(slug)}
                    />
                  </li>
                ),
              )}
            </ul>
            {incoming.length > 30 && (
              <Button
                variant="text"
                size="sm"
                aria-expanded={incomingExpanded}
                onClick={() => setIncomingExpanded(!incomingExpanded)}
              >
                {incomingExpanded ? <ChevronUp /> : <ChevronDown />}
                {t(incomingExpanded ? "fewerReferences" : "moreReferences")}
              </Button>
            )}
          </section>
        )}
        {sources.length > 0 && (
          <section className="space-y-2" aria-labelledby={sourcesId}>
            <h3 id={sourcesId} className="type-label text-content-secondary">
              {t("sourceDocuments")}
            </h3>
            <ul className="type-body flex flex-wrap gap-x-4 gap-y-1">
              {(sourcesExpanded ? sources : sources.slice(0, 30)).map(
                (reference, index) => (
                  <li key={reference} className="max-w-full min-w-0">
                    <SourceReference
                      resource={resource}
                      reference={reference}
                      label={t("sourceDocument", { number: index + 1 })}
                      onOpen={setSourceId}
                    />
                  </li>
                ),
              )}
            </ul>
            {sources.length > 30 && (
              <Button
                variant="text"
                size="sm"
                aria-expanded={sourcesExpanded}
                onClick={() => setSourcesExpanded(!sourcesExpanded)}
              >
                {sourcesExpanded ? <ChevronUp /> : <ChevronDown />}
                {t(sourcesExpanded ? "fewerReferences" : "moreReferences")}
              </Button>
            )}
          </section>
        )}
      </footer>
      {sourceId && (
        <KnowledgeResultDocument
          key={sourceId}
          resource={resource}
          sourceId={sourceId}
          onClose={() => setSourceId("")}
        />
      )}
    </>
  );
}

/** 引用按字符上限紧凑展示，完整名称仍供悬停和辅助技术读取。 */
function ReferenceButton({
  label,
  onClick,
}: {
  label: string;
  onClick: () => void;
}) {
  const characters = Array.from(label);
  const display =
    characters.length > 24 ? `${characters.slice(0, 24).join("")}…` : label;
  return (
    <Button
      variant="link"
      size="sm"
      className="max-w-full"
      title={label}
      aria-label={label}
      onClick={onClick}
    >
      <span aria-hidden="true">•</span>
      <span className="min-w-0 truncate">{display}</span>
    </Button>
  );
}

function SourceReference({
  resource,
  reference,
  label,
  onOpen,
}: {
  resource: ResourceRef;
  reference: string;
  label: string;
  onOpen: (id: string) => void;
}) {
  // 官方远端 SourceRefs 协议允许 ID|标题；本地管线保存不可变来源版本 ID。
  const separator = reference.indexOf("|");
  const id = separator > 0 ? reference.slice(0, separator) : reference;
  const title = separator > 0 ? reference.slice(separator + 1) : "";
  const active = useRouteSceneActive();
  const source = useQuery({
    ...sourceDetailOptions(resource, id),
    enabled: active && Boolean(id) && !title,
  });
  return (
    <ReferenceButton
      label={title || source.data?.filename || label}
      onClick={() => onOpen(id)}
    />
  );
}
