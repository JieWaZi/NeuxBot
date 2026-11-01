import { Fragment } from "react";
import { useTranslation } from "react-i18next";
import type { GetChatMessageFileChangeResponse } from "@/shared/api/generated/types.gen";
import { cn } from "@/shared/lib/cn";

type Comparison = NonNullable<GetChatMessageFileChangeResponse["comparison"]>;

/** 只布局已持久化的有界 hunk，主题和分栏切换均不重新比较全文。 */
export function SnapshotDiffView({
  comparison,
  split = false,
}: {
  comparison: Comparison;
  split?: boolean;
}) {
  const { t } = useTranslation("chat");
  if (comparison.state !== "text")
    return (
      <p className="type-body text-muted-foreground p-4">
        {t(
          `fileChanges.${comparison.state === "binary" ? "binary" : comparison.state === "oversized" ? "oversized" : "unreadable"}`,
        )}
      </p>
    );
  return (
    <div className="type-body overflow-auto font-mono" data-snapshot-diff>
      {comparison.beforeMode &&
      comparison.afterMode &&
      comparison.beforeMode !== comparison.afterMode ? (
        <p className="type-caption text-muted-foreground px-4 py-2">
          {t("fileChanges.mode", {
            before: comparison.beforeMode.toString(8),
            after: comparison.afterMode.toString(8),
          })}
        </p>
      ) : null}
      {!comparison.hunks?.length ? (
        <p className="type-body text-muted-foreground px-4 py-2">
          {t(
            comparison.beforeExists === false
              ? "fileChanges.emptyAdded"
              : comparison.afterExists === false
                ? "fileChanges.emptyDeleted"
                : "fileChanges.unchangedText",
          )}
        </p>
      ) : null}
      {comparison.coarse ? (
        <p className="type-caption text-muted-foreground px-4 py-2">
          {t("fileChanges.coarse")}
        </p>
      ) : null}
      <table className="w-full border-collapse text-left whitespace-pre">
        <tbody>
          {comparison.hunks?.map((hunk, hunkIndex) => {
            let oldLine = hunk.oldStart ?? 0;
            let newLine = hunk.newStart ?? 0;
            return (
              <Fragment key={hunkIndex}>
                <tr className="bg-muted text-muted-foreground">
                  <td colSpan={split ? 4 : 3} className="px-4 py-1">
                    @@ -{hunk.oldStart},{hunk.oldLines} +{hunk.newStart},
                    {hunk.newLines} @@
                  </td>
                </tr>
                {hunk.lines?.map((line, lineIndex) => {
                  const before = line.kind === "add" ? undefined : oldLine++;
                  const after = line.kind === "delete" ? undefined : newLine++;
                  const prefix =
                    line.kind === "add"
                      ? "+"
                      : line.kind === "delete"
                        ? "-"
                        : " ";
                  const style =
                    line.kind === "add"
                      ? "bg-success/10"
                      : line.kind === "delete"
                        ? "bg-destructive/10"
                        : "";
                  const text = (
                    <>
                      {line.text}
                      {line.noNewline ? (
                        <span className="type-caption text-muted-foreground ml-4">
                          {t("fileChanges.noNewline")}
                        </span>
                      ) : null}
                    </>
                  );
                  return (
                    <tr key={lineIndex} className={split ? undefined : style}>
                      <td
                        className={cn(
                          "text-muted-foreground w-10 px-2 text-right align-top select-none",
                          split && before !== undefined && style,
                        )}
                      >
                        {before}
                      </td>
                      {split ? (
                        <td
                          className={cn(
                            "w-1/2 px-3 align-top",
                            before !== undefined && style,
                          )}
                        >
                          {before !== undefined ? text : null}
                        </td>
                      ) : null}
                      <td
                        className={cn(
                          "text-muted-foreground w-10 px-2 text-right align-top select-none",
                          split && after !== undefined && style,
                        )}
                      >
                        {after}
                      </td>
                      <td
                        className={cn(
                          "px-3 align-top",
                          split && "w-1/2",
                          split && after !== undefined && style,
                        )}
                      >
                        {split ? (
                          after !== undefined ? (
                            text
                          ) : null
                        ) : (
                          <>
                            {prefix} {text}
                          </>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </Fragment>
            );
          })}
        </tbody>
      </table>
      {comparison.truncated ? (
        <p className="type-caption text-muted-foreground px-4 py-2">
          {t("fileChanges.diffTruncated")}
        </p>
      ) : null}
    </div>
  );
}

export function snapshotDiffText(comparison: Comparison): string {
  return (comparison.hunks ?? [])
    .map(
      (hunk) =>
        `@@ -${hunk.oldStart},${hunk.oldLines} +${hunk.newStart},${hunk.newLines} @@\n` +
        (hunk.lines ?? [])
          .map(
            (line) =>
              `${line.kind === "add" ? "+" : line.kind === "delete" ? "-" : " "}${line.text ?? ""}${line.noNewline ? "\n\\ No newline at end of file" : ""}`,
          )
          .join("\n"),
    )
    .join("\n");
}
