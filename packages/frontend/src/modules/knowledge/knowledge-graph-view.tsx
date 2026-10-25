import { useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ListFilter, Network } from "lucide-react";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { Button } from "@/shared/ui/button";
import { RetainedPresence } from "@/shared/ui/retained-presence";
import { cn } from "@/shared/lib/cn";
import { Card, CardContent } from "@/shared/ui/card";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { ResourcePageSearchField } from "@/shared/ui/resource-page";
import { Empty } from "./knowledge-controls";
import {
  KnowledgeGraphCanvas,
  type KnowledgeGraphCanvasControls,
} from "./knowledge-graph-canvas";
import { KnowledgeGraphLegend } from "./knowledge-graph-legend";
import {
  buildLocalGraph,
  graphNeighborhood,
  graphAdjacency,
  graphFrontier,
  mergeGraphData,
} from "./knowledge-graph-model";
import {
  localGraphPagesOptions,
  remoteGraphOptions,
  remoteGraphExpansionOptions,
  remoteGraphSearchOptions,
} from "./knowledge-graph-queries";
import { KnowledgeGraphReader } from "./knowledge-graph-reader";
import { KnowledgeGraphLoading } from "./knowledge-loading";
import { KnowledgeReadFeedback } from "./knowledge-read-feedback";
import type { ResourceRef } from "./knowledge-transfer";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import { useGraphCopy } from "./use-graph-copy";

const emptyAnchors: string[] = [];

/** 参考本地 WeKnora WikiBrowser 图谱视图：画布铺满，控件与详情按需悬浮。 */
export function KnowledgeGraphView({
  resource,
  center,
  onCenter,
}: {
  resource: ResourceRef;
  center: string;
  onCenter: (slug: string) => void;
}) {
  const t = useGraphCopy();
  const active = useRouteSceneActive();
  const remoteGraph = knowledgeAdapter(resource).graphMode === "remote";
  const client = useQueryClient();
  const controls = useRef<KnowledgeGraphCanvasControls>(null);
  const [search, setSearch] = useState("");
  const [searchFocused, setSearchFocused] = useState(false);
  const [hiddenTypes, setHiddenTypes] = useState<Set<string>>(() => new Set());
  const [showArrows, setShowArrows] = useState(true);
  const [selectedSlug, setSelected] = useState(center);
  const [readerVisible, setReaderVisible] = useState(false);
  const [expanded, setExpanded] = useState<{
    center: string;
    anchors: string[];
    selection: string;
  }>({ center, anchors: [], selection: "" });
  const anchors = expanded.center === center ? expanded.anchors : emptyAnchors;
  const expansionSelection =
    expanded.center === center ? expanded.selection : "";
  const deferredSearch = useDeferredValue(search.trim());
  const local = useQuery({
    ...localGraphPagesOptions(resource),
    select: buildLocalGraph,
    enabled: active && !remoteGraph,
  });
  const remote = useQuery({
    ...remoteGraphOptions(resource, center),
    enabled: active && remoteGraph,
  });
  const query = !remoteGraph ? local : remote;
  const expansion = useQuery({
    ...remoteGraphExpansionOptions(resource, anchors, client),
    enabled: active && remoteGraph && anchors.length > 0,
  });
  const searchQuery = useQuery({
    ...remoteGraphSearchOptions(resource, deferredSearch),
    enabled:
      active && remoteGraph && deferredSearch.length >= 2 && searchFocused,
  });
  const merged = useMemo(() => {
    const data = query.data;
    if (!data) return undefined;
    if (remoteGraph)
      return mergeGraphData(
        data,
        anchors.length ? (expansion.data ?? []) : [],
        new Set([center, expansionSelection, anchors.at(-1) ?? ""]),
      );
    const nodes = data.nodes.filter((node) => !hiddenTypes.has(node.page_type));
    const slugs = new Set(nodes.map((node) => node.slug));
    const filtered = {
      ...data,
      nodes,
      edges: data.edges.filter(
        (edge) => slugs.has(edge.source) && slugs.has(edge.target),
      ),
    };
    const base = graphNeighborhood(filtered, center, 1);
    return mergeGraphData(
      base,
      anchors.map((slug) => graphNeighborhood(filtered, slug, 1)),
      new Set([center, expansionSelection, anchors.at(-1) ?? ""]),
    );
  }, [
    query.data,
    expansion.data,
    remoteGraph,
    hiddenTypes,
    center,
    expansionSelection,
    anchors,
  ]);
  const graph = useMemo(() => {
    if (!merged) return undefined;
    const nodes = merged.nodes.filter(
      (node) => !hiddenTypes.has(node.page_type),
    );
    const slugs = new Set(nodes.map((node) => node.slug));
    return {
      ...merged,
      nodes,
      edges: merged.edges.filter(
        (edge) => slugs.has(edge.source) && slugs.has(edge.target),
      ),
      meta: { ...merged.meta, returned: nodes.length },
    };
  }, [merged, hiddenTypes]);
  const selected = selectedSlug
    ? query.data?.nodes.find((node) => node.slug === selectedSlug) ||
      merged?.nodes.find((node) => node.slug === selectedSlug) || {
        slug: selectedSlug,
        title: selectedSlug,
        page_type: "concept",
        link_count: -1,
      }
    : undefined;
  const searchNodes =
    remoteGraph && deferredSearch.length >= 2
      ? (searchQuery.data ?? []).flatMap((page) =>
          page.slug
            ? [
                {
                  slug: page.slug,
                  title: page.title || page.slug,
                  pageId: page.id,
                  page_type: page.pageType || "concept",
                  link_count: 0,
                },
              ]
            : [],
        )
      : (query.data?.nodes ?? []);
  const remoteSearch = remoteGraph && deferredSearch.length >= 2;
  const matches = searchNodes.filter(
    (node) =>
      !hiddenTypes.has(node.page_type) &&
      ((remoteGraph && deferredSearch.length >= 2) ||
        `${node.title} ${node.slug}`
          .toLowerCase()
          .includes(search.trim().toLowerCase())),
  );
  const adjacency = graph
    ? graphAdjacency(graph)
    : new Map<string, Set<string>>();
  const frontier = graph && center ? graphFrontier(graph, center) : [];
  function select(slug: string) {
    if (slug) setSelected(slug);
    setReaderVisible(Boolean(slug));
  }
  function focus(slug: string) {
    setSelected(slug);
    setExpanded({ center: slug, anchors: [], selection: slug });
    onCenter(slug);
  }
  function jump(slug: string) {
    if (!graph?.nodes.some((node) => node.slug === slug)) focus(slug);
    select(slug);
  }
  function bloom(slugs: string[]) {
    if (!center) {
      if (slugs[0]) focus(slugs[0]);
      return;
    }
    setExpanded({
      center,
      anchors: [...new Set([...anchors, ...slugs])],
      selection: selectedSlug,
    });
  }
  useEffect(() => {
    if (readerVisible && selectedSlug) controls.current?.focus(selectedSlug);
  }, [readerVisible, selectedSlug, graph]);
  const legend = graph ? (
    <KnowledgeGraphLegend
      graph={graph}
      types={[
        ...new Set(query.data?.nodes.map((node) => node.page_type)),
      ].toSorted()}
      hiddenTypes={hiddenTypes}
      onToggleType={(type) => {
        setHiddenTypes((current) => {
          const next = new Set(current);
          if (next.has(type)) next.delete(type);
          else next.add(type);
          return next;
        });
        select("");
      }}
      showArrows={showArrows}
      onToggleArrows={() => setShowArrows((value) => !value)}
      refresh={() => void query.refetch()}
      fetching={query.isFetching}
      overview={() => {
        select("");
        focus("");
      }}
      remote={remoteGraph}
      frontier={frontier.length}
      grow={() => bloom(frontier)}
      busy={expansion.isFetching}
    />
  ) : null;
  return (
    <section
      className="@container/graph relative flex min-h-0 flex-1 flex-col overflow-hidden"
      aria-label={t("graph")}
    >
      {graph?.nodes.length ? (
        <KnowledgeGraphCanvas
          key={`${center}:${[...hiddenTypes].sort().join(",")}`}
          graph={graph}
          selected={selectedSlug}
          onSelect={select}
          onFocus={focus}
          center={center}
          onBloom={(slug) => bloom([slug])}
          showArrows={showArrows}
          controlsRef={controls}
        />
      ) : query.data ? (
        <div className="grid min-h-0 flex-1 place-items-center">
          <Empty
            icon={Network}
            title={t(query.data.nodes.length ? "noMatch" : "empty")}
            description={t(
              query.data.nodes.length ? "noMatchHelp" : "emptyHelp",
            )}
          />
        </div>
      ) : null}
      <div className="pointer-events-none absolute top-4 right-4 left-4 z-10 flex items-start justify-between gap-4">
        <div
          className="pointer-events-auto relative max-w-[calc(100%-6rem)] min-w-0 @[42rem]/graph:max-w-[calc(100%-14rem)]"
          onFocus={() => setSearchFocused(true)}
          onBlur={(event) => {
            if (!event.currentTarget.contains(event.relatedTarget))
              setSearchFocused(false);
          }}
        >
          <ResourcePageSearchField
            aria-label={t("search")}
            placeholder={t("search")}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && matches[0]) {
                jump(matches[0].slug);
                setSearch("");
                setSearchFocused(false);
              }
            }}
          />
          {searchFocused && query.data && (
            <Card
              variant="floating"
              className="absolute top-full left-0 z-20 mt-2 w-full"
            >
              <CardContent
                className="scrollbar-hidden max-h-64 overflow-y-auto"
                aria-label={t("searchHelp")}
              >
                {remoteSearch && (
                  <KnowledgeReadFeedback
                    hasData={Boolean(searchQuery.data)}
                    state={{
                      pending: searchQuery.isPending,
                      error: searchQuery.error,
                      refreshing:
                        searchQuery.isFetching && !searchQuery.isPending,
                      retry: () => void searchQuery.refetch(),
                    }}
                    loading={
                      <p
                        role="status"
                        aria-busy="true"
                        className="type-body text-content-muted"
                      >
                        {t("searching")}
                      </p>
                    }
                  />
                )}
                {matches.length ? (
                  matches.slice(0, 20).map((node) => (
                    <Button
                      key={node.slug}
                      variant="ghost"
                      className="w-full justify-start"
                      onClick={() => {
                        jump(node.slug);
                        setSearch("");
                        setSearchFocused(false);
                      }}
                    >
                      <span className="truncate">{node.title}</span>
                    </Button>
                  ))
                ) : !remoteSearch || searchQuery.isSuccess ? (
                  <p className="type-body text-content-muted">{t("noMatch")}</p>
                ) : null}
              </CardContent>
            </Card>
          )}
        </div>
        {legend && (
          <>
            <Card
              variant="floating"
              className="pointer-events-auto hidden shrink-0 @[42rem]/graph:block"
            >
              <CardContent>{legend}</CardContent>
            </Card>
            <div className="pointer-events-auto @[42rem]/graph:hidden">
              <Popover>
                <PopoverTrigger asChild>
                  <Button
                    variant="secondary"
                    size="icon"
                    aria-label={t("options")}
                  >
                    <ListFilter />
                  </Button>
                </PopoverTrigger>
                <PopoverContent width="compact" align="end">
                  {legend}
                </PopoverContent>
              </Popover>
            </div>
          </>
        )}
      </div>
      {(query.isPending ||
        query.error ||
        query.isFetching ||
        expansion.error ||
        expansion.isFetching) && (
        <div
          className={
            query.data
              ? "bg-surface-panel absolute top-16 left-4 z-10 max-w-[calc(100%-2rem)] rounded-lg"
              : "mt-20"
          }
        >
          <KnowledgeReadFeedback
            hasData={Boolean(query.data)}
            state={{
              pending: query.isPending,
              error: query.error || expansion.error,
              refreshing: query.isFetching && !query.isPending,
              retry: () => {
                void query.refetch();
                if (anchors.length) void expansion.refetch();
              },
            }}
            loading={<KnowledgeGraphLoading />}
          />
        </div>
      )}
      <div
        data-graph-reader-panel
        data-state={readerVisible ? "open" : "closed"}
        aria-hidden={!readerVisible}
        inert={!readerVisible}
        className={cn(
          "duration-layout ease-ui absolute top-0 right-0 bottom-0 z-20 max-w-full overflow-hidden transition-[width] motion-reduce:transition-none",
          readerVisible ? "w-120" : "pointer-events-none w-0",
        )}
      >
        <RetainedPresence
          present={readerVisible && Boolean(selected)}
          exitDurationMs={220}
          data-motion="none"
          className="absolute inset-y-0 right-0 w-120 max-w-[100cqw]"
        >
          {selected && (
            <KnowledgeGraphReader
              key={selected.slug}
              resource={resource}
              node={selected}
              center={center}
              visibleNeighbors={adjacency.get(selected.slug)?.size ?? 0}
              close={() => select("")}
              select={jump}
              expand={() => focus(selected.slug)}
              bloom={() => bloom([selected.slug])}
              busy={expansion.isFetching}
              visible={readerVisible}
            />
          )}
        </RetainedPresence>
      </div>
    </section>
  );
}
