import { ApiError } from "@/shared/api/errors";
import {
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type Ref,
} from "react";
import { FileText, Upload, X } from "lucide-react";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { DialogFooter } from "@/shared/ui/dialog";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { useCopy } from "./use-extended-copy";
import { IconAction, Pill } from "./knowledge-controls";
import { reportKnowledgeError } from "./knowledge-error-notice";

import {
  createKnowledgeUploadQueue,
  uploadFingerprint,
  uploadOutcomeUnknown,
  type KnowledgeUploadEntry as UploadEntry,
  type KnowledgeUploadQueue,
} from "./knowledge-upload-queue";

/** 每份原件保留独立请求与结果；取消只停止未完成上传，不回滚已接纳原件。 */
export function UploadSourcesContent({
  dialogRef,
  close,
  upload,
  queue: retainedQueue,
  mode = "separate",
  onRedetect,
}: {
  queue?: KnowledgeUploadQueue;
  mode?: "separate" | "automatic" | "unknown";
  onRedetect?: () => Promise<void>;
  dialogRef: Ref<{ close: () => void }>;
  close: () => void;
  upload: (
    file: File,
    requestId: string,
    signal: AbortSignal,
  ) => Promise<unknown>;
}) {
  const t = useCopy();
  const input = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const [sourceKind, setSourceKind] = useState<"file" | "folder">("file");
  const controller = useRef<AbortController | null>(null);
  const running = useRef(false);
  const [localQueue] = useState(createKnowledgeUploadQueue);
  const queue = retainedQueue ?? localQueue;
  const [entries, renderEntries] = useState(queue.getEntries);
  function setEntries(update: (current: UploadEntry[]) => UploadEntry[]) {
    renderEntries(queue.update(update));
  }
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [confirmationInvalid, setConfirmationInvalid] = useState(false);
  const [redetecting, setRedetecting] = useState(false);
  useEffect(() => () => controller.current?.abort(), []);
  function accept(list: FileList | null, fromFolder = false) {
    if (!list?.length || running.current) return;
    // 目录过滤沿用 WeKnora：隐藏文件与隐藏目录不进入待上传集合。
    const files = Array.from(list).filter(
      (file) =>
        !fromFolder ||
        !(file.webkitRelativePath || file.name)
          .split("/")
          .some((part) => part.startsWith(".")),
    );
    const valid = files.filter(
      (file) =>
        /\.(pdf|docx|md)$/i.test(file.name) && file.size <= 32 * 1024 * 1024,
    );
    setError(
      files.some((file) => file.size > 32 * 1024 * 1024)
        ? t("maxSourceSize")
        : valid.length < files.length
          ? t("formatsHelp")
          : files.length === 0
            ? t("folderEmptyHelp")
            : "",
    );
    setEntries((current) => {
      const seen = new Set(current.map((entry) => entry.file));
      const added: UploadEntry[] = [];
      for (const file of valid) {
        if (seen.has(file)) continue;
        seen.add(file);
        added.push({ id: crypto.randomUUID(), file, state: "ready" });
      }
      return [...current, ...added];
    });
  }
  function patch(id: string, value: Partial<UploadEntry>) {
    setEntries((current) =>
      current.map((entry) =>
        entry.id === id ? { ...entry, ...value } : entry,
      ),
    );
  }
  async function submit() {
    if (running.current || mode === "unknown" || confirmationInvalid) return;
    const pending = queue
      .getEntries()
      .filter((entry) => entry.state !== "done" && entry.state !== "duplicate");
    const abort = new AbortController();
    controller.current = abort;
    running.current = true;
    setSaving(true);
    try {
      for (const entry of pending) {
        if (abort.signal.aborted) {
          patch(entry.id, { state: "cancelled", error: undefined });
          continue;
        }
        let sent = false;
        try {
          const fingerprint =
            entry.fingerprint ?? (await uploadFingerprint(entry.file));
          patch(entry.id, { fingerprint });
          if (
            queue
              .getEntries()
              .some(
                (current) =>
                  current.id !== entry.id &&
                  current.fingerprint === fingerprint &&
                  current.state !== "duplicate",
              )
          ) {
            patch(entry.id, {
              state: "duplicate",
              error: undefined,
              uncertain: false,
            });
            continue;
          }
          if (abort.signal.aborted) {
            patch(entry.id, { state: "cancelled", uncertain: entry.uncertain });
            continue;
          }
          patch(entry.id, { state: "uploading", error: undefined });
          sent = true;
          const result = await upload(entry.file, entry.id, abort.signal);
          const acceptedMode =
            result &&
            typeof result === "object" &&
            "uploadMode" in result &&
            (result.uploadMode === "automatic" ||
              result.uploadMode === "separate")
              ? result.uploadMode
              : undefined;
          patch(entry.id, { state: "done", uncertain: false, acceptedMode });
        } catch (cause) {
          reportKnowledgeError(
            cause,
            `upload-source:${entry.id}`,
            t("importSourceFailed"),
          );
          patch(entry.id, {
            state: abort.signal.aborted ? "cancelled" : "failed",
            uncertain: Boolean(
              entry.uncertain ||
              (sent && uploadOutcomeUnknown(cause, abort.signal.aborted)),
            ),
            error: abort.signal.aborted
              ? undefined
              : localApiErrorMessage(cause, t("importSourceFailed")),
          });
          if (
            cause instanceof ApiError &&
            (cause.code === "KnowledgeUploadModeChanged" ||
              cause.status === 409)
          ) {
            setConfirmationInvalid(true);
            break;
          }
        }
      }
    } finally {
      controller.current = null;
      running.current = false;
      setSaving(false);
    }
  }
  function closeDialog() {
    setEntries((current) =>
      current.filter(
        (entry) => entry.state !== "done" && entry.state !== "duplicate",
      ),
    );
    close();
  }
  const failed = entries.some(
    (entry) => entry.state === "failed" || entry.state === "cancelled",
  );
  useImperativeHandle(dialogRef, () => ({
    close: () => {
      if (!running.current) closeDialog();
    },
  }));
  return (
    <>
      <div className="grid min-h-0 gap-4 overflow-y-auto px-6 pb-6">
        <SegmentedControl
          ariaLabel={t("sourceKind")}
          value={sourceKind}
          options={[
            { value: "file", label: t("sourceFile") },
            { value: "folder", label: t("sourceFolder") },
          ]}
          onValueChange={setSourceKind}
          disabled={saving}
        />
        <div
          className="border-border-strong grid justify-items-center gap-3 rounded-lg border border-dashed px-4 py-6 text-center"
          onDragOver={(event) => event.preventDefault()}
          onDrop={(event) => {
            event.preventDefault();
            accept(event.dataTransfer.files);
          }}
        >
          <Upload
            className="text-content-secondary size-icon-xl"
            aria-hidden="true"
          />
          <p className="type-body">
            {t(sourceKind === "folder" ? "folderHelp" : "dropHelp")}
          </p>
          <p className="text-content-muted type-caption">{t("formatsHelp")}</p>
          <Button
            variant="secondary"
            disabled={saving}
            onClick={() =>
              (sourceKind === "folder" ? folderInput : input).current?.click()
            }
          >
            {t(sourceKind === "folder" ? "chooseFolder" : "chooseFiles")}
          </Button>
        </div>
        <p className="type-caption text-content-secondary">
          {t("uploadSizeHelp")}
        </p>
        <input
          ref={input}
          aria-label={t("chooseFiles")}
          type="file"
          multiple
          accept=".pdf,.docx,.md"
          hidden
          disabled={saving}
          onChange={(event) => {
            accept(event.target.files);
            event.target.value = "";
          }}
        />
        <input
          type="file"
          hidden
          ref={(node) => {
            folderInput.current = node;
            node?.setAttribute("webkitdirectory", "");
          }}
          aria-label={t("chooseFolder")}
          multiple
          disabled={saving}
          onChange={(event) => {
            accept(event.target.files, true);
            event.target.value = "";
          }}
        />
        {entries.map((entry) => (
          <div
            key={entry.id}
            className="flex min-w-0 flex-wrap items-center gap-2"
          >
            <FileText className="size-icon-md shrink-0" aria-hidden="true" />
            <span className="type-body min-w-0 flex-1 break-words">
              {entry.file.webkitRelativePath || entry.file.name}
            </span>
            <Pill tone={entry.state === "failed" ? "failed" : "muted"}>
              {t(
                entry.state === "duplicate"
                  ? "uploadDuplicate"
                  : entry.state === "done"
                    ? entry.acceptedMode === "automatic"
                      ? "uploadRemoteAccepted"
                      : "uploadPendingOrganize"
                    : entry.state === "uploading"
                      ? "uploadingFile"
                      : entry.state === "failed"
                        ? "uploadFailed"
                        : entry.state === "cancelled"
                          ? "uploadCancelled"
                          : "uploadReady",
              )}
            </Pill>
            {entry.state !== "done" && (
              <IconAction
                icon={X}
                label={t("removeUploadFile", {
                  name: entry.file.webkitRelativePath || entry.file.name,
                })}
                disabled={saving || entry.uncertain}
                onClick={() =>
                  setEntries((current) =>
                    current.filter((value) => value.id !== entry.id),
                  )
                }
              />
            )}
            {entry.uncertain && (
              <p className="type-caption text-content-secondary w-full">
                {t("uploadUnconfirmedHelp")}
              </p>
            )}
          </div>
        ))}
        {error && (
          <p role="alert" className="type-body text-destructive">
            {error}
          </p>
        )}
      </div>
      <DialogFooter>
        <div className="min-w-0 flex-1">
          <p className="type-caption text-content-secondary">
            {t(
              mode === "automatic"
                ? "uploadAutomaticNotice"
                : mode === "unknown"
                  ? "uploadUnknownNotice"
                  : "uploadSeparateNotice",
            )}
          </p>
          {confirmationInvalid && (
            <p role="alert" className="type-caption text-warning">
              {t("uploadModeChanged")}
            </p>
          )}
          {onRedetect && (
            <Button
              type="button"
              variant="text"
              size="sm"
              disabled={saving || redetecting}
              onClick={async () => {
                setRedetecting(true);
                try {
                  await onRedetect();
                  setConfirmationInvalid(false);
                } catch (cause) {
                  reportKnowledgeError(
                    cause,
                    "redetect-upload-service",
                    t("knowledgeOperationFailed"),
                  );
                } finally {
                  setRedetecting(false);
                }
              }}
            >
              {t("remoteRedetect")}
            </Button>
          )}
        </div>
        <Button
          variant="ghost"
          onClick={() => {
            if (saving) controller.current?.abort();
            else closeDialog();
          }}
        >
          {t(saving ? "cancelUpload" : "close")}
        </Button>
        <Button
          disabled={
            saving ||
            redetecting ||
            confirmationInvalid ||
            mode === "unknown" ||
            !entries.some(
              (entry) => entry.state !== "done" && entry.state !== "duplicate",
            )
          }
          onClick={() => void submit()}
        >
          {t(failed ? "retryUploadFiles" : "uploadDocuments")}
        </Button>
      </DialogFooter>
    </>
  );
}
