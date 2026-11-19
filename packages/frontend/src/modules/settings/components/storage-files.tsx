import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { MoreHorizontal, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useSearchParams } from "react-router-dom";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import type { GithubComJieWaZiNeuxBotServerApisStorageStorageFile as StorageFile } from "@/shared/api/generated/types.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Input } from "@/shared/ui/input";
import { PageHeader } from "@/shared/ui/page-header";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";
import { useDialogFocusRestore } from "@/shared/ui/use-dialog-focus-restore";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { WorkspaceFileTypeIcon } from "@/shared/workspace/workspace-file-type-icon";
import {
  loadMoreStorage,
  removeStorageFile,
  storageEntitiesOptions,
  storageFilesOptions,
  storageKeys,
  type StorageFileScope,
} from "../api/storage";
import { StorageBreadcrumbs } from "./storage-breadcrumbs";
import { StorageChatList } from "./storage-chat-list";
import { useStorageDeleteFocus } from "./storage-delete-focus";
import { SettingsCard, SettingsRow } from "./settings-section-layout";
import {
  storageDate,
  StorageError,
  StorageLoading,
  storageSize,
  storageUploadDate,
} from "./storage-feedback";

const searchDelayMs = 250;

function nameOf(file: StorageFile) {
  return file.relativePath?.split("/").at(-1) ?? "";
}

export function StoragePartnerDetails({
  scope,
  active,
  disabled,
  onBack,
  showSceneChrome,
}: {
  scope: StorageFileScope;
  active: boolean;
  disabled: boolean;
  onBack: () => void;
  showSceneChrome: boolean;
}) {
  const { t } = useTranslation("storage");
  const client = useQueryClient();
  const [params, setParams] = useSearchParams();
  const location = useLocation();
  const tab = params.get("tab") === "chats" ? "chats" : "files";
  const search = (params.get("search") ?? "").slice(0, 200);
  const trimmedSearch = search.trim();
  const [value, setValue] = useState(trimmedSearch);
  useEffect(() => {
    const timeout = setTimeout(() => setValue(trimmedSearch), searchDelayMs);
    return () => clearTimeout(timeout);
  }, [trimmedSearch]);
  const sort = params.get("sort") === "largest" ? "largest" : "latest";
  const [selected, setSelected] = useState<StorageFile>();
  const details = useRef<HTMLDivElement>(null);
  const fileList = useRef<HTMLDivElement>(null);
  const selectedTab = () =>
    details.current?.querySelector<HTMLElement>(
      'button[role="radio"][aria-checked="true"]',
    ) ?? null;
  const deleteFocus = useStorageDeleteFocus(selectedTab);
  const focus = useDialogFocusRestore(deleteFocus.target);
  const partner = useQuery({
    ...getWakerOptions(withLocalErrorSurface({ path: { wakerId: scope.id } })),
    enabled: active,
    select: (row) => row.name,
  });
  const fileOptions = storageFilesOptions(scope, value, sort);
  const files = useInfiniteQuery({
    ...fileOptions,
    enabled: active && tab === "files" && value === trimmedSearch,
    placeholderData: keepPreviousData,
  });
  const waitingForSearch = value !== trimmedSearch || files.isPlaceholderData;
  const pages = waitingForSearch ? [] : (files.data?.pages ?? []);
  const first = pages[0];
  const rows = pages.flatMap((page) => page.items ?? []);
  const titlePage = files.data?.pages[0];
  const rankedTitle = client
    .getQueryData(storageEntitiesOptions("wakers").queryKey)
    ?.pages.flatMap((page) => page.items ?? [])
    .find((row) => row.id === scope.id)?.title;
  const title = partner.data || titlePage?.title || rankedTitle || t("wakers");
  const remove = useMutation({
    mutationFn: async (file: StorageFile) => {
      if (!file.id || !file.version) throw new Error();
      return removeStorageFile(scope, file.id, file.version);
    },
    onSuccess: async () => {
      deleteFocus.markRemoved();
      await Promise.all(
        [storageKeys.usage, storageKeys.entities, storageKeys.files].map(
          (queryKey) => client.invalidateQueries({ queryKey }),
        ),
      );
      setSelected(undefined);
    },
    onError: () =>
      void client.invalidateQueries({ queryKey: storageKeys.files }),
  });
  function fileLabel(file: StorageFile) {
    if (file.source !== "attachment") return file.relativePath || nameOf(file);
    return [
      nameOf(file),
      file.chatTitle || t("chatFallback"),
      file.modifiedAt ? storageUploadDate(file.modifiedAt) : undefined,
      file.messageId
        ? t("uploadReference", { reference: file.messageId.slice(-8) })
        : undefined,
    ]
      .filter(Boolean)
      .join(" · ");
  }
  function changeFilter(key: "search" | "sort" | "tab", text: string) {
    const next = new URLSearchParams(params);
    if (text) next.set(key, text);
    else next.delete(key);
    setParams(next, { replace: true, state: location.state });
  }
  return (
    <div ref={details} className="grid min-w-0 gap-4">
      {showSceneChrome ? (
        <>
          <StorageBreadcrumbs partnerTitle={title} />
          <h1 className="sr-only">{title}</h1>
        </>
      ) : (
        <PageHeader
          layout="detail"
          title={title}
          backAction={{ label: t("backToStorage"), onClick: onBack }}
        />
      )}
      <SegmentedControl
        appearance="shell"
        ariaLabel={t("title")}
        value={tab}
        options={[
          { value: "files", label: t("files") },
          { value: "chats", label: t("chatTab") },
        ]}
        onValueChange={(value) =>
          changeFilter("tab", value === "files" ? "" : value)
        }
      />
      {disabled ? (
        <p role="status" className="type-body text-content-secondary">
          {t("offline")}
        </p>
      ) : null}
      {tab === "chats" ? (
        <StorageChatList
          active={active}
          disabled={disabled}
          wakerId={scope.id}
          fallbackFocus={selectedTab}
        />
      ) : (
        <>
          <div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-2">
            <Select
              value={sort}
              onValueChange={(text) =>
                changeFilter("sort", text === "latest" ? "" : text)
              }
            >
              <SelectTrigger size="md" aria-label={t("fileSort")}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="latest">{t("latestFiles")}</SelectItem>
                <SelectItem value="largest">{t("largestFiles")}</SelectItem>
              </SelectContent>
            </Select>
            <Input
              size="md"
              className="min-w-0"
              type="search"
              aria-label={t("searchFiles")}
              placeholder={t("searchFiles")}
              value={search}
              maxLength={200}
              onChange={(event) => changeFilter("search", event.target.value)}
            />
          </div>
          {files.isPending || waitingForSearch ? <StorageLoading /> : null}
          {files.isError && !waitingForSearch ? (
            <StorageError
              error={files.error}
              retry={() => void files.refetch()}
              stale={rows.length > 0}
            />
          ) : null}
          {first ? (
            <p className="type-ui text-content-secondary">
              {t("fileCount", { count: first.total ?? 0 })} ·{" "}
              {first.complete
                ? storageSize(first.totalBytes ?? 0)
                : t("partial", { size: storageSize(first.totalBytes ?? 0) })}
            </p>
          ) : null}
          {rows.length ? (
            <SettingsCard ref={fileList}>
              <TooltipProvider>
                {rows.map((file) => {
                  const name = nameOf(file);
                  const folder =
                    file.source === "workspace" &&
                    file.relativePath?.includes("/")
                      ? file.relativePath.slice(
                          0,
                          file.relativePath.lastIndexOf("/"),
                        )
                      : undefined;
                  const description = [
                    folder,
                    storageSize(file.bytes ?? 0),
                    file.chatTitle ||
                      t(
                        file.source === "attachment"
                          ? "attachmentFile"
                          : "workspaceFile",
                      ),
                    file.modifiedAt ? storageDate(file.modifiedAt) : undefined,
                  ]
                    .filter(Boolean)
                    .join(" · ");
                  return (
                    <SettingsRow
                      key={file.id}
                      grouped
                      title={
                        <span className="flex items-center gap-2">
                          <WorkspaceFileTypeIcon
                            name={name}
                            isDirectory={false}
                          />
                          <span className="min-w-0 wrap-anywhere">{name}</span>
                        </span>
                      }
                      description={description}
                    >
                      <DropdownMenu>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <DropdownMenuTrigger asChild>
                              <Button
                                size="icon"
                                variant="ghost"
                                aria-label={t("fileActions", {
                                  name: fileLabel(file),
                                })}
                                data-storage-delete-action={file.id}
                                disabled={disabled || remove.isPending}
                              >
                                <MoreHorizontal aria-hidden="true" />
                              </Button>
                            </DropdownMenuTrigger>
                          </TooltipTrigger>
                          <TooltipContent>
                            {t("fileActions", { name })}
                          </TooltipContent>
                        </Tooltip>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem
                            variant="destructive"
                            onSelect={() => {
                              if (!file.id) return;
                              deleteFocus.capture(fileList.current, file.id);
                              remove.reset();
                              setSelected(file);
                            }}
                          >
                            <Trash2 />
                            {t("deleteFile")}
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </SettingsRow>
                  );
                })}
              </TooltipProvider>
            </SettingsCard>
          ) : first && !files.isError ? (
            <p className="type-body text-content-secondary">
              {t(value ? "noFileMatches" : "noFiles")}
            </p>
          ) : null}
          {first && files.hasNextPage ? (
            <div>
              <Button
                size="settings"
                variant="settings-outline"
                disabled={disabled || files.isFetching}
                onClick={() =>
                  void loadMoreStorage(client, fileOptions.queryKey, files)
                }
              >
                {t("more")}
              </Button>
            </div>
          ) : null}
        </>
      )}
      {selected ? (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open && !remove.isPending) setSelected(undefined);
          }}
        >
          <DialogContent
            {...focus}
            title={t("deleteFileTitle")}
            description={t(
              selected.source === "attachment"
                ? "deleteAttachmentImpact"
                : "deleteWorkspaceFileImpact",
              { name: fileLabel(selected) },
            )}
          >
            {remove.error ? (
              <div className="px-6 pb-4">
                <StorageError error={remove.error} />
              </div>
            ) : null}
            <DialogFooter>
              <Button
                size="settings"
                variant="settings-outline"
                disabled={remove.isPending}
                onClick={() => setSelected(undefined)}
              >
                {t("cancel")}
              </Button>
              <Button
                size="settings"
                variant="danger"
                disabled={disabled || remove.isPending || remove.isError}
                onClick={() => remove.mutate(selected)}
              >
                {t("deleteFile")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      ) : null}
    </div>
  );
}
