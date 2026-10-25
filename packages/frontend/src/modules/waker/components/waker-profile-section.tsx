import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Download, FileText } from "lucide-react";
import { useState } from "react";

import {
  exportWakerProfile,
  listWakerDocumentsOptions,
} from "../api/waker-queries";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import type { ProfileDocument, Waker } from "../model/types";
import { ProfileDocumentEditorDialog } from "./profile-document-editor-dialog";
import { saveBlob, sanitizeDownloadFileName } from "@/shared/platform";
import { Button } from "@/shared/ui/button";

import { WakerAccentBlock } from "./waker-accent-block";

type Props = {
  waker: Waker;
};

export function WakerProfileSection({ waker }: Props) {
  const { t } = useTranslation("waker");
  const wakerId = waker.id ?? "";
  const query = useQuery(
    listWakerDocumentsOptions(withLocalErrorSurface({ path: { wakerId } })),
  );
  const [selectedDocument, setSelectedDocument] = useState<ProfileDocument>();
  const [downloading, setDownloading] = useState(false);
  const [downloadError, setDownloadError] = useState("");
  const documents = query.data ?? [];

  const downloadProfile = async () => {
    if (!wakerId) return;
    setDownloading(true);
    setDownloadError("");
    try {
      const result = await exportWakerProfile({
        path: { wakerId },
        parseAs: "blob",
      });
      if (result.data instanceof Blob) {
        await saveBlob(
          result.data,
          sanitizeDownloadFileName(
            `waker-${waker.name ?? "profile"}-profile.zip`,
          ),
        );
      }
    } catch {
      setDownloadError(t("profile.downloadError"));
    } finally {
      setDownloading(false);
    }
  };

  return (
    <>
      <section>
        <header className="mb-3 flex items-center justify-between gap-3">
          <div className="min-w-0">
            <h2 className="type-label font-semibold">{t("profile.title")}</h2>
            <p className="text-content-secondary type-caption mt-1">
              {t("profile.lead")}
            </p>
          </div>
          <Button
            size="icon"
            variant="ghost"
            aria-label={t("profile.download")}
            title={t("profile.download")}
            disabled={downloading || !wakerId}
            onClick={() => void downloadProfile()}
          >
            <Download />
          </Button>
        </header>

        <div>
          {downloadError ? (
            <p role="alert" className="text-destructive type-body mb-3">
              {downloadError}
            </p>
          ) : null}
          {query.isPending ? (
            <p className="text-muted-foreground type-body">
              {t("profile.loading")}
            </p>
          ) : query.isError ? (
            <div className="flex flex-wrap items-center justify-between gap-4">
              <p className="text-destructive type-body">
                {t("profile.loadError")}
              </p>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => void query.refetch()}
              >
                {t("profile.retry")}
              </Button>
            </div>
          ) : (
            <WakerAccentBlock divided className="p-0">
              {documents.map((document) => (
                <button
                  key={document.kind}
                  type="button"
                  className="hover:bg-card-hover focus-visible:ring-ring duration-quick min-h-row-default type-body flex w-full min-w-0 items-center gap-3 px-3 py-2 text-left transition-colors outline-none focus-visible:ring-2 motion-reduce:transition-none"
                  onClick={() => setSelectedDocument(document)}
                >
                  <FileText
                    aria-hidden="true"
                    className="text-muted-foreground size-icon-md shrink-0"
                    strokeWidth={1.5}
                  />
                  <span className="min-w-0 flex-1 font-mono [overflow-wrap:anywhere]">
                    {document.fileName}
                  </span>
                  {document.kind === "memory" ? (
                    <span className="bg-surface-scene text-muted-foreground type-caption border-border-subtle rounded-full border px-2 py-0.5">
                      {t("profile.memory")}
                    </span>
                  ) : (
                    <span className="text-muted-foreground type-caption">
                      {t("profile.edit")}
                    </span>
                  )}
                </button>
              ))}
            </WakerAccentBlock>
          )}
        </div>
      </section>

      {selectedDocument ? (
        <ProfileDocumentEditorDialog
          open
          document={selectedDocument}
          wakerId={wakerId}
          onOpenChange={(nextOpen) => {
            if (!nextOpen) setSelectedDocument(undefined);
          }}
        />
      ) : null}
    </>
  );
}
