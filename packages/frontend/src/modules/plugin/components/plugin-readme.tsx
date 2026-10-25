import { useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogTrigger } from "@/shared/ui/dialog";
import { MarkdownContent } from "@/shared/ui/markdown-content";
import { useMutation } from "@tanstack/react-query";
import {
  getPluginReadme,
  getCatalogPluginReadme,
} from "@/shared/api/generated/sdk.gen";
import { saveBlob } from "@/shared/platform";
import {
  withLocalErrorSurface,
  localApiErrorMessage,
} from "@/shared/api/local-error-surface";

export type ReadmeSource = { locale?: string } & (
  | { pluginId: string; versionId: string; wakerId?: string }
  | { catalogKey: string; sourceCommit?: string; archiveSHA256?: string }
);

/** 只限制预览布局，完整 Markdown 与弹窗始终使用同一份来源正文。 */
export function PluginReadme({
  content,
  status,
  source,
}: {
  content: string;
  status?: string;
  source?: ReadmeSource;
}) {
  const { t } = useTranslation("plugin");
  const preview = useRef<HTMLDivElement>(null);
  const body = useRef<HTMLDivElement>(null);
  const [overflowing, setOverflowing] = useState(false);
  const readable = status === undefined || status === "available";
  useLayoutEffect(() => {
    const element = preview.current;
    const inner = body.current;
    if (!element || !inner) return;
    const measure = () =>
      setOverflowing(element.scrollHeight > element.clientHeight + 1);
    measure();
    // Markdown 延迟加载和容器宽度变化都会改变实际行数。
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(measure);
    observer?.observe(element);
    observer?.observe(inner);
    return () => observer?.disconnect();
  }, [content]);
  return (
    <Dialog>
      <div className="max-w-prose space-y-3">
        {status === "too-large" && (
          <p className="type-body text-content-secondary">
            {t("readmeTooLarge")}
          </p>
        )}
        {status === "invalid" && (
          <p className="type-body text-content-secondary">
            {t("readmeInvalid")}
          </p>
        )}
        <div
          ref={preview}
          className={readable ? "max-h-60 overflow-hidden" : undefined}
        >
          <div ref={body}>
            <MarkdownContent content={content} />
          </div>
        </div>
        {overflowing && readable && (
          <DialogTrigger asChild>
            <Button type="button" variant="ghost">
              {t("viewFullReadme")}
            </Button>
          </DialogTrigger>
        )}
        {status === "too-large" && source && <ReadmeDownload source={source} />}
      </div>
      {readable && (
        <DialogContent title={t("fullReadme")} size="xl" layout="form">
          <div className="min-h-0 overflow-auto px-6 pb-6">
            <MarkdownContent content={content} />
          </div>
        </DialogContent>
      )}
    </Dialog>
  );
}

/** 超出内置阅读预算的原文按固定版本下载，不把截断内容称为完整说明。 */
function ReadmeDownload({ source }: { source: ReadmeSource }) {
  const { t } = useTranslation("plugin");
  const download = useMutation({
    mutationFn: async () => {
      const result =
        "catalogKey" in source
          ? await getCatalogPluginReadme({
              ...withLocalErrorSurface({
                path: { key: source.catalogKey },
                query: {
                  sourceCommit: source.sourceCommit,
                  archiveSHA256: source.archiveSHA256,
                  locale: source.locale,
                },
              }),
              throwOnError: true,
            })
          : await getPluginReadme({
              ...withLocalErrorSurface({
                path: { pluginId: source.pluginId },
                query: {
                  versionId: source.versionId,
                  wakerId: source.wakerId,
                  locale: source.locale,
                },
              }),
              throwOnError: true,
            });
      await saveBlob(
        new Blob([result.data.content ?? ""], {
          type: "text/markdown;charset=utf-8",
        }),
        "README.md",
      );
    },
  });
  return (
    <div className="space-y-2">
      <Button
        type="button"
        variant="secondary"
        disabled={download.isPending}
        onClick={() => download.mutate()}
      >
        {t(download.isPending ? "busy" : "downloadReadme")}
      </Button>
      {download.error && (
        <p role="alert" className="type-body text-destructive">
          {localApiErrorMessage(download.error, t("failed"))}
        </p>
      )}
    </div>
  );
}
