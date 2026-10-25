import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { Download, FileUp, LoaderCircle, X } from "lucide-react";
import { useMemo, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";

import {
  createRolePresetMutation,
  exportProfileTemplate,
  listRolePresetDocumentsOptions,
  listRolePresetsQueryKey,
  updateRolePresetMutation,
} from "../api/waker-queries";
import {
  parseProfileProjection,
  type EditableProfileDocumentKind,
} from "../model/profile-projection";
import type {
  Expertise,
  RolePreset,
  RolePresetWriteRequest,
  WorkMethod,
} from "../model/types";
import {
  cleanManualProfile,
  ProfileManualEditor,
} from "./profile-manual-editor";
import { ApiError } from "@/shared/api/errors";
import { saveBlob } from "@/shared/platform";
import { CompanionPicker } from "@/shared/ui/companion-picker";
import {
  companionAvatar,
  randomCompanionAvatar,
} from "@/shared/companion/appearance";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { Textarea } from "@/shared/ui/textarea";

const schema = z.object({
  roleName: z
    .string()
    .trim()
    .min(1, "请输入角色名称")
    .max(80, "角色名称不能超过 80 字"),
  defaultAvatar: z
    .string()
    .trim()
    .regex(/^companion:v[12]:[0-9a-f]{32}$/, "请重新生成形象"),
  description: z
    .string()
    .trim()
    .min(1, "请输入角色描述")
    .max(1000, "角色描述不能超过 1000 字"),
  inputMode: z.enum(["manual", "markdown"]),
});

type Values = z.infer<typeof schema>;
type DocumentKind = EditableProfileDocumentKind;
type SelectedFile = {
  name: string;
  size: number;
};

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  preset?: RolePreset;
  onSaved: (preset?: RolePreset) => void;
};

const inputModes = [
  { value: "manual", label: "手动填写" },
  { value: "markdown", label: "上传 Markdown" },
] as const;

const documentSlots: Array<{ kind: DocumentKind; fileName: string }> = [
  { kind: "identity", fileName: "IDENTITY.md" },
  { kind: "persona", fileName: "PERSONA.md" },
  { kind: "playbook", fileName: "PLAYBOOK.md" },
];

export function RolePresetFormDialog({ open, ...props }: Props) {
  if (!open) return null;
  return <OpenRolePresetFormDialog open={open} {...props} />;
}

function OpenRolePresetFormDialog({
  onOpenChange,
  onSaved,
  open,
  preset,
}: Props) {
  const queryClient = useQueryClient();
  const [expertise, setExpertise] = useState<Expertise[]>(
    preset?.expertise ?? [],
  );
  const [workStyle, setWorkStyle] = useState<string[]>(preset?.workStyle ?? []);
  const [workMethods, setWorkMethods] = useState<WorkMethod[]>(
    preset?.workMethods ?? [],
  );
  const [documents, setDocuments] = useState<
    Partial<Record<DocumentKind, string>>
  >({});
  const [selectedFiles, setSelectedFiles] = useState<
    Partial<Record<DocumentKind, SelectedFile>>
  >({});
  const [initialAvatar] = useState(() =>
    preset
      ? companionAvatar(preset.defaultAvatar, preset.roleName ?? "")
      : randomCompanionAvatar(),
  );
  const [documentErrors, setDocumentErrors] = useState<
    Partial<Record<DocumentKind, string>>
  >({});
  const [downloading, setDownloading] = useState(false);
  const [downloadError, setDownloadError] = useState("");
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      roleName: preset?.roleName ?? "",
      defaultAvatar: initialAvatar,
      description: preset?.description ?? "",
      inputMode: "manual",
    },
  });
  const mode = useWatch({ control: form.control, name: "inputMode" });
  const avatar = useWatch({ control: form.control, name: "defaultAvatar" });
  const presetDocuments = useQuery({
    ...listRolePresetDocumentsOptions({
      path: { rolePresetId: preset?.id ?? "" },
    }),
    enabled: Boolean(preset?.id),
  });
  const create = useMutation(
    withLocalErrorMutation(createRolePresetMutation()),
  );
  const update = useMutation(
    withLocalErrorMutation(updateRolePresetMutation()),
  );
  const pending = create.isPending || update.isPending;

  const existingDocuments = useMemo(() => {
    const nextDocuments: Partial<Record<DocumentKind, string>> = {};
    for (const document of presetDocuments.data ?? []) {
      if (
        document.kind === "identity" ||
        document.kind === "persona" ||
        document.kind === "playbook"
      ) {
        nextDocuments[document.kind] = document.content ?? "";
      }
    }
    return nextDocuments;
  }, [presetDocuments.data]);
  const resolvedDocuments = { ...existingDocuments, ...documents };

  const chooseMarkdown = async (kind: DocumentKind, file: File | undefined) => {
    if (!file) return;
    if (!file.name.toLowerCase().endsWith(".md")) {
      setDocumentErrors((current) => ({
        ...current,
        [kind]: "请选择 Markdown 文件",
      }));
      return;
    }
    if (file.size > 1024 * 1024) {
      setDocumentErrors((current) => ({
        ...current,
        [kind]: "单份 Markdown 不能超过 1 MiB",
      }));
      return;
    }
    try {
      const content = new TextDecoder("utf-8", { fatal: true }).decode(
        await file.arrayBuffer(),
      );
      setDocuments((current) => ({ ...current, [kind]: content }));
      setSelectedFiles((current) => ({
        ...current,
        [kind]: { name: file.name, size: file.size },
      }));
      setDocumentErrors((current) => omitRecordKey(current, kind));
    } catch {
      setDocumentErrors((current) => ({
        ...current,
        [kind]: "Markdown 必须使用 UTF-8 编码",
      }));
    }
  };

  const removeMarkdownSelection = (kind: DocumentKind) => {
    setDocuments((current) => omitRecordKey(current, kind));
    setSelectedFiles((current) => omitRecordKey(current, kind));
  };

  const changeInputMode = (value: Values["inputMode"]) => {
    if (value === "manual" && mode === "markdown") {
      const projection = parseProfileProjection(resolvedDocuments);
      setExpertise(projection.expertise);
      setWorkStyle(projection.workStyle);
      setWorkMethods(projection.workMethods);
    }
    form.setValue("inputMode", value, { shouldDirty: true });
  };

  const downloadTemplate = async () => {
    setDownloading(true);
    setDownloadError("");
    try {
      const result = await exportProfileTemplate({ parseAs: "blob" });
      if (result.data instanceof Blob) {
        await saveBlob(result.data, "ally-waker-profile-template.zip");
      }
    } catch {
      setDownloadError("空白档案 ZIP 下载失败，请重试。");
    } finally {
      setDownloading(false);
    }
  };

  const submit = form.handleSubmit(async (values) => {
    const body: RolePresetWriteRequest =
      values.inputMode === "manual"
        ? {
            roleName: values.roleName,
            defaultAvatar: values.defaultAvatar,
            description: values.description,
            inputMode: "manual",
            documents: {},
            manual: cleanManualProfile(expertise, workStyle, workMethods),
          }
        : {
            roleName: values.roleName,
            defaultAvatar: values.defaultAvatar,
            description: values.description,
            inputMode: "markdown",
            documents: resolvedDocuments,
            manual: {},
          };
    try {
      let saved: RolePreset | undefined;
      if (preset?.id && preset.source === "user") {
        await update.mutateAsync({
          path: { rolePresetId: preset.id },
          body,
        });
      } else {
        saved = await create.mutateAsync({ body });
      }
      await queryClient.invalidateQueries({
        queryKey: listRolePresetsQueryKey(),
      });
      onSaved(saved);
      onOpenChange(false);
    } catch {
      // React Query 保存错误，由表单字段或底部错误区展示。
    }
  });

  const mutationError = create.error ?? update.error;
  const roleNameConflict =
    mutationError instanceof ApiError &&
    mutationError.code === "RolePresetNameConflict";
  const errorMessage =
    mutationError && !roleNameConflict
      ? "角色预设保存失败，请检查输入后重试。"
      : "";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        layout="form"
        size="xl"
        title={preset ? "编辑角色预设" : "新建角色预设"}
      >
        <form
          className="flex min-h-0 flex-1 flex-col overflow-hidden"
          onSubmit={submit}
        >
          <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-5">
            <div className="grid grid-cols-[1fr_1fr] gap-4">
              <label className="type-label grid gap-2 font-medium">
                <span>
                  角色名称 <b className="text-destructive">*</b>
                </span>
                <Input
                  placeholder="例如：前端开发工程师"
                  {...form.register("roleName")}
                />
                <span className="text-muted-foreground type-caption font-normal">
                  用于展示和选择角色预设，名称不可与已有预设重复。
                </span>
                {form.formState.errors.roleName?.message ? (
                  <span className="text-destructive type-caption">
                    {form.formState.errors.roleName.message}
                  </span>
                ) : roleNameConflict ? (
                  <span className="text-destructive type-caption">
                    角色名称已存在
                  </span>
                ) : null}
              </label>
              <CompanionPicker
                label="默认头像"
                name={form.getValues("roleName") || "伙伴"}
                value={avatar}
                onChange={(value) =>
                  form.setValue("defaultAvatar", value, {
                    shouldDirty: true,
                    shouldValidate: true,
                  })
                }
              />
            </div>
            <label className="type-label mt-4 grid gap-2 font-medium">
              <span>
                角色描述 <b className="text-destructive">*</b>
              </span>
              <Textarea
                placeholder="例如：专注于前端界面设计与实现，重视可用性和工程质量。"
                {...form.register("description")}
              />
              {form.formState.errors.description?.message ? (
                <span className="text-destructive type-caption">
                  {form.formState.errors.description.message}
                </span>
              ) : null}
            </label>

            <SegmentedControl
              className="mt-5"
              ariaLabel="档案录入模式"
              value={mode}
              options={inputModes}
              onValueChange={changeInputMode}
            />

            {mode === "manual" ? (
              <div className="mt-4 grid gap-4">
                <ProfileManualEditor
                  expertise={expertise}
                  onExpertiseChange={setExpertise}
                  workStyle={workStyle}
                  onWorkStyleChange={setWorkStyle}
                  workMethods={workMethods}
                  onWorkMethodsChange={setWorkMethods}
                />
              </div>
            ) : (
              <section className="border-border mt-4 rounded-lg border p-4">
                <header className="flex items-center justify-between gap-4">
                  <div>
                    <h3 className="type-label font-semibold">上传 Markdown</h3>
                    <p className="text-muted-foreground type-caption mt-1">
                      上传槽决定文档类型，原文件名无需匹配。
                    </p>
                  </div>
                  <Button
                    size="sm"
                    variant="secondary"
                    type="button"
                    disabled={downloading}
                    onClick={() => void downloadTemplate()}
                  >
                    <Download />
                    下载空白档案 ZIP
                  </Button>
                </header>
                <div className="mt-3 grid grid-cols-3 gap-3">
                  {documentSlots.map(({ fileName, kind }) => (
                    <div
                      key={kind}
                      className="border-border bg-muted grid min-h-32 rounded-lg border p-3"
                    >
                      <label className="hover:border-border-strong grid flex-1 cursor-pointer place-items-center rounded-md border border-transparent text-center">
                        <span>
                          <FileUp
                            className="text-muted-foreground size-icon-lg mx-auto"
                            strokeWidth={1.5}
                          />
                          <strong className="type-body mt-2 block font-medium">
                            {fileName}
                          </strong>
                          <span className="text-muted-foreground type-caption mt-1 block">
                            {selectedFiles[kind]
                              ? `${selectedFiles[kind].name} · ${formatBytes(selectedFiles[kind].size)}`
                              : existingDocuments[kind] !== undefined
                                ? "已有内容，点击替换"
                                : "选择 .md 文件"}
                          </span>
                        </span>
                        <input
                          type="file"
                          accept=".md,text/markdown,text/plain"
                          className="sr-only"
                          onChange={(event) => {
                            void chooseMarkdown(kind, event.target.files?.[0]);
                            event.target.value = "";
                          }}
                        />
                      </label>
                      {selectedFiles[kind] ? (
                        <Button
                          className="mt-2"
                          size="sm"
                          variant="ghost"
                          type="button"
                          onClick={() => removeMarkdownSelection(kind)}
                        >
                          <X />
                          移除本次选择
                        </Button>
                      ) : (
                        <span className="text-muted-foreground type-caption mt-1 block">
                          未上传时{preset ? "保留已有内容" : "使用空内容"}
                        </span>
                      )}
                      {documentErrors[kind] ? (
                        <span
                          role="alert"
                          className="text-destructive type-caption mt-1"
                        >
                          {documentErrors[kind]}
                        </span>
                      ) : null}
                    </div>
                  ))}
                </div>
                {downloadError ? (
                  <p
                    role="alert"
                    className="text-destructive type-caption mt-2"
                  >
                    {downloadError}
                  </p>
                ) : null}
              </section>
            )}

            <div className="mt-4 grid grid-cols-2 gap-3">
              <div className="bg-muted rounded-md p-3">
                <h3 className="type-body font-medium">技能</h3>
                <p className="text-muted-foreground type-caption mt-1">
                  后续开放
                </p>
              </div>
              <div className="bg-muted rounded-md p-3">
                <h3 className="type-body font-medium">MCP 连接器</h3>
                <p className="text-muted-foreground type-caption mt-1">
                  后续开放
                </p>
              </div>
            </div>
            {errorMessage ? (
              <p role="alert" className="text-destructive type-body mt-3">
                {errorMessage}
              </p>
            ) : null}
          </div>

          <DialogFooter>
            <Button type="submit" disabled={pending}>
              {pending ? <LoaderCircle className="animate-spin" /> : null}
              保存角色预设
            </Button>
            <Button
              type="button"
              variant="secondary"
              onClick={() => onOpenChange(false)}
            >
              取消
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function omitRecordKey<T>(
  record: Partial<Record<DocumentKind, T>>,
  omittedKey: DocumentKind,
): Partial<Record<DocumentKind, T>> {
  return Object.fromEntries(
    Object.entries(record).filter(([key]) => key !== omittedKey),
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KiB`;
}
