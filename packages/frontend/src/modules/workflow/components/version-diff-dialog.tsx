import { generateDiffFile } from "@git-diff-view/file";
import { DiffModeEnum, DiffView } from "@git-diff-view/react";
import "@git-diff-view/react/styles/diff-view-pure.css";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useTheme } from "@/shared/theme";
import { Dialog, DialogContent } from "@/shared/ui/dialog";
import { EmptyState } from "@/shared/ui/empty-state";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import {
  getWorkflowSourceOptions,
  listWorkflowRevisionsOptions,
} from "../api/workflow-queries";
import { WorkflowLoadingState, WorkflowQueryError } from "./query-feedback";

export function VersionDiffDialog({
  id,
  revisionId,
  ordinal,
  open,
  onOpenChange,
}: {
  id: string;
  revisionId: string;
  ordinal: number;
  open: boolean;
  onOpenChange: (value: boolean) => void;
}) {
  const { t } = useTranslation("workflow");
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="xl"
        title={t("versionDiff")}
        description={t("compareHelp")}
      >
        {open ? (
          <VersionDiff id={id} revisionId={revisionId} ordinal={ordinal} />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function VersionDiff({
  id,
  revisionId,
  ordinal,
}: {
  id: string;
  revisionId: string;
  ordinal: number;
}) {
  const { t } = useTranslation("workflow");
  const { resolvedTheme } = useTheme();
  const versions = useQuery(
    listWorkflowRevisionsOptions({ path: { id }, query: { limit: 100 } }),
  );
  const [before, setBefore] = useState("");
  const selected =
    before ||
    versions.data?.items?.find((item) => (item.ordinal ?? 0) < ordinal)?.id ||
    revisionId;
  const previous = useQuery(
    getWorkflowSourceOptions({ path: { id }, query: { revisionId: selected } }),
  );
  const current = useQuery(
    getWorkflowSourceOptions({ path: { id }, query: { revisionId } }),
  );
  const [mode, setMode] = useState("unified");
  const diff = useMemo(() => {
    if (!previous.data || !current.data) return undefined;
    const file = generateDiffFile(
      `workflow.js (${selected})`,
      previous.data.source ?? "",
      `workflow.js (${revisionId})`,
      current.data.source ?? "",
      "javascript",
      "javascript",
    );
    file.initTheme(resolvedTheme);
    file.init();
    if (mode === "split") file.buildSplitDiffLines();
    else file.buildUnifiedDiffLines();
    return file;
  }, [current.data, previous.data, selected, revisionId, mode, resolvedTheme]);
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 px-6 pb-5">
      <div className="flex flex-wrap items-center gap-3">
        <Select value={selected} onValueChange={setBefore}>
          <SelectTrigger aria-label={t("compareVersion")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {versions.data?.items?.flatMap((item) =>
              item.id
                ? [
                    <SelectItem key={item.id} value={item.id}>
                      {t("version", { version: item.ordinal })}
                    </SelectItem>,
                  ]
                : [],
            )}
          </SelectContent>
        </Select>
        <span className="text-muted-foreground type-body">
          → {t("version", { version: ordinal })}
        </span>
        <SegmentedControl
          ariaLabel={t("versionDiff")}
          value={mode}
          onValueChange={setMode}
          options={[
            { value: "unified", label: t("diffUnified") },
            { value: "split", label: t("diffSplit") },
          ]}
        />
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {previous.isError || current.isError || versions.isError ? (
          <WorkflowQueryError
            error={previous.error ?? current.error ?? versions.error}
            retrying={
              previous.isFetching || current.isFetching || versions.isFetching
            }
            onRetry={() => {
              void previous.refetch();
              void current.refetch();
              void versions.refetch();
            }}
          />
        ) : null}
        {!diff ? (
          previous.isPending || current.isPending ? (
            <WorkflowLoadingState className="p-5" />
          ) : null
        ) : previous.data?.sourceDigest === current.data?.sourceDigest ? (
          <EmptyState title={t("noDifference")} />
        ) : (
          <DiffView
            diffFile={diff}
            diffViewMode={
              mode === "split" ? DiffModeEnum.Split : DiffModeEnum.Unified
            }
            diffViewTheme={resolvedTheme}
            diffViewHighlight={(current.data?.source?.length ?? 0) < 100000}
            diffViewWrap={false}
            diffViewFontSize={12}
            className="workspace-diff-view"
          />
        )}
      </div>
    </div>
  );
}
