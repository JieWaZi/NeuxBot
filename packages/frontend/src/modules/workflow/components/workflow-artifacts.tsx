import { useMutation } from "@tanstack/react-query";
import {
  Copy,
  Download,
  FileCheck,
  LoaderCircle,
  RefreshCw,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  getWorkflowArtifact,
  downloadWorkflowFile,
} from "@/shared/api/generated/sdk.gen";
import { saveBlob } from "@/shared/platform";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";

function formatFileSize(size: number, locale?: string): string {
  const unit = size < 1024 ? "B" : size < 1024 * 1024 ? "KB" : "MB";
  const value =
    unit === "B" ? size : size / (unit === "KB" ? 1024 : 1024 * 1024);
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(value)} ${unit}`;
}

function FileDownload({
  id,
  runId,
  fileId,
  path,
  name,
  disabled,
}: {
  id: string;
  runId: string;
  fileId?: string;
  path?: string;
  name: string;
  disabled?: boolean;
}) {
  const { t } = useTranslation("workflow");
  const download = useMutation({
    mutationFn: async () => {
      const result = await downloadWorkflowFile(
        withLocalErrorSurface({
          path: { id, runId },
          query: fileId ? { fileId } : { path },
          parseAs: "blob",
          throwOnError: true,
        }),
      );
      await saveBlob(result.data as unknown as Blob, name);
    },
  });
  return (
    <>
      <Button
        size="sm"
        variant="surface"
        className="shrink-0 whitespace-nowrap"
        aria-busy={download.isPending}
        disabled={disabled || download.isPending}
        onClick={() => download.mutate()}
      >
        {download.isPending ? (
          <LoaderCircle className="animate-spin motion-reduce:animate-none" />
        ) : (
          <Download />
        )}
        {t("downloadArtifact")}
      </Button>
      {download.error ? (
        <p
          role="alert"
          className="text-destructive type-body w-full break-words"
        >
          {localApiErrorMessage(download.error, t("error"))}
        </p>
      ) : null}
    </>
  );
}

export function artifactCandidates(value: unknown): string[] {
  const result = new Set<string>();
  function visit(item: unknown, depth: number) {
    if (depth > 8 || result.size >= 20) return;
    if (Array.isArray(item)) {
      for (const child of item.slice(0, 100)) visit(child, depth + 1);
    } else if (item && typeof item === "object") {
      for (const [key, child] of Object.entries(item)) {
        if (
          typeof child === "string" &&
          /^(path|filePath|reportPath|artifactPath|outputPath)$/i.test(key) &&
          child.length <= 4096
        )
          result.add(child);
        else visit(child, depth + 1);
      }
    }
  }
  visit(value, 0);
  return [...result];
}

function ArtifactCandidate({
  id,
  runId,
  path,
}: {
  id: string;
  runId: string;
  path: string;
}) {
  const { t } = useTranslation("workflow");
  const check = useMutation({
    mutationFn: async ({ expectedDigest }: { expectedDigest?: string }) => {
      const result = await getWorkflowArtifact(
        withLocalErrorSurface({
          path: { id, runId },
          query: { path, expectedDigest },
          throwOnError: true,
        }),
      );
      return result.data;
    },
  });
  const file = check.data;
  return (
    <div className="bg-surface-module grid gap-3 rounded-lg p-4">
      <div className="flex flex-wrap items-center gap-2">
        <code className="type-caption min-w-0 flex-1 break-all">{path}</code>
        <Button
          size="icon-sm"
          variant="ghost"
          aria-label={t("copy")}
          onClick={() => void navigator.clipboard.writeText(path)}
        >
          <Copy />
        </Button>
        <Button
          size="sm"
          variant="secondary"
          disabled={check.isPending}
          onClick={() => check.mutate({ expectedDigest: file?.digest })}
        >
          {file ? <RefreshCw /> : <FileCheck />}
          {file ? t("checkAgain") : t("checkArtifact")}
        </Button>
        <FileDownload
          id={id}
          runId={runId}
          path={path}
          name={path.split("/").at(-1) ?? "file"}
        />
      </div>
      {check.error ? (
        <p role="alert" className="text-destructive type-caption">
          {localApiErrorMessage(check.error, t("error"))}
        </p>
      ) : null}
      {file && !file.verified ? (
        <p className="text-muted-foreground type-caption">
          {t("artifactUnavailable", {
            reason: t(`artifactReason.${file.reason}`, {
              defaultValue: file.reason,
            }),
          })}
        </p>
      ) : null}
      {file?.verified ? (
        <>
          <p className="text-muted-foreground type-caption break-all">
            {file.relativePath} · {file.size} B · SHA-256 {file.digest}
          </p>
          {file.changed ? (
            <p role="status" className="text-warning type-caption">
              {t("artifactChanged")}
            </p>
          ) : null}
          {file.text !== undefined ? (
            <pre className="bg-muted type-body max-h-96 overflow-auto rounded-md p-4 whitespace-pre-wrap">
              {file.text}
            </pre>
          ) : null}
          {file.dataUrl && file.mediaType?.startsWith("image/") ? (
            <img
              src={file.dataUrl}
              alt={file.relativePath ?? path}
              className="max-h-96 max-w-full object-contain"
            />
          ) : null}
        </>
      ) : null}
    </div>
  );
}

export function WorkflowArtifacts({
  id,
  runId,
  values,
  files = [],
}: {
  id: string;
  runId: string;
  values: unknown[];
  files?: Array<{
    id?: string;
    name?: string;
    size?: number;
    available?: boolean;
  }>;
}) {
  const { t, i18n } = useTranslation("workflow");
  const paths = files.length
    ? []
    : [...new Set(values.flatMap(artifactCandidates))];
  if (!paths.length && !files.length) return null;
  return (
    <section className="grid gap-3">
      <h2 className="type-label font-medium">{t("artifacts")}</h2>
      {paths.length ? (
        <p className="text-muted-foreground type-caption">
          {t("artifactHelp")}
        </p>
      ) : null}
      {files.map((file) => (
        <div
          key={file.id}
          className="bg-surface-module flex flex-wrap items-center gap-3 rounded-lg p-3"
        >
          <div className="flex min-w-0 flex-1 items-center gap-3">
            <WorkspaceFileTypeIcon
              isDirectory={false}
              name={file.name ?? ""}
              size={16}
            />
            <div className="grid min-w-0 gap-1">
              <p className="type-body font-medium break-all">{file.name}</p>
              {file.size !== undefined ? (
                <p className="text-muted-foreground type-caption">
                  {formatFileSize(file.size, i18n.resolvedLanguage)}
                </p>
              ) : null}
              {file.available === false ? (
                <p className="text-content-secondary type-body">
                  {t("outputFileUnavailable")}
                </p>
              ) : null}
            </div>
          </div>
          <FileDownload
            id={id}
            runId={runId}
            fileId={file.id}
            name={file.name ?? "file"}
            disabled={!file.available || !file.id}
          />
        </div>
      ))}
      {paths.map((path) => (
        <ArtifactCandidate key={path} id={id} runId={runId} path={path} />
      ))}
    </section>
  );
}
