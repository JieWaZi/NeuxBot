import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { LoaderCircle } from "lucide-react";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";

import {
  createWakerMutation,
  getWakerQueryKey,
  listWakerDocumentsQueryKey,
  listWakersQueryKey,
  updateWakerMutation,
} from "../api/waker-queries";
import type { Expertise, RolePreset, Waker, WorkMethod } from "../model/types";
import {
  cleanManualProfile,
  ProfileManualEditor,
} from "./profile-manual-editor";
import { RolePresetPicker } from "./role-preset-picker";
import { ApiError } from "@/shared/api/errors";
import { CompanionPicker } from "@/shared/ui/companion-picker";
import {
  companionAvatar,
  randomCompanionAvatar,
} from "@/shared/companion/appearance";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";

const schema = z
  .object({
    rolePresetId: z.string(),
    name: z.string().trim().min(1, "请输入名称").max(80, "名称不能超过 80 字"),
    roleName: z.string().trim().max(80, "角色名称不能超过 80 字"),
    bio: z.string().trim().max(1000, "简介不能超过 1000 字"),
    avatar: z
      .string()
      .trim()
      .regex(/^companion:v[12]:[0-9a-f]{32}$/, "请重新生成形象"),
  })
  .superRefine((values, context) => {
    if (!values.rolePresetId && !values.roleName) {
      context.addIssue({
        code: "custom",
        path: ["rolePresetId"],
        message: "请选择或创建角色预设",
      });
    }
  });

type Values = z.infer<typeof schema>;

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  waker?: Waker;
};

export function WakerFormDialog({ onOpenChange, open, waker }: Props) {
  if (!open) return null;
  return (
    <OpenWakerFormDialog
      open={open}
      onOpenChange={onOpenChange}
      waker={waker}
    />
  );
}

function OpenWakerFormDialog({ onOpenChange, open, waker }: Props) {
  const queryClient = useQueryClient();
  const [initialAvatar] = useState(() =>
    waker
      ? companionAvatar(waker.avatar, waker.name ?? "")
      : randomCompanionAvatar(),
  );
  const [expertise, setExpertise] = useState<Expertise[]>(
    waker?.expertise ?? [],
  );
  const [workStyle, setWorkStyle] = useState<string[]>(waker?.workStyle ?? []);
  const [workMethods, setWorkMethods] = useState<WorkMethod[]>(
    waker?.workMethods ?? [],
  );
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      rolePresetId: "",
      name: waker?.name ?? "",
      roleName: waker?.roleName ?? "",
      bio: waker?.bio ?? "",
      avatar: initialAvatar,
    },
  });

  const create = useMutation(withLocalErrorMutation(createWakerMutation()));
  const update = useMutation(withLocalErrorMutation(updateWakerMutation()));
  const pending = create.isPending || update.isPending;
  const avatar = useWatch({ control: form.control, name: "avatar" });
  const selectedPresetId = useWatch({
    control: form.control,
    name: "rolePresetId",
  });

  const choosePreset = (preset: RolePreset) => {
    form.setValue("rolePresetId", preset.id ?? "", {
      shouldDirty: true,
      shouldValidate: true,
    });
  };

  const submit = form.handleSubmit(async (values) => {
    try {
      if (waker?.id) {
        await update.mutateAsync({
          path: { wakerId: waker.id },
          body: {
            name: values.name,
            roleName: values.roleName,
            bio: values.bio,
            avatar: values.avatar,
            manual: cleanManualProfile(expertise, workStyle, workMethods),
          },
        });
        await Promise.all([
          queryClient.invalidateQueries({
            queryKey: getWakerQueryKey({ path: { wakerId: waker.id } }),
          }),
          queryClient.invalidateQueries({
            queryKey: listWakerDocumentsQueryKey({
              path: { wakerId: waker.id },
            }),
          }),
        ]);
      } else {
        await create.mutateAsync({
          body: {
            rolePresetId: values.rolePresetId,
            name: values.name,
            bio: values.bio,
            avatar: values.avatar,
          },
        });
      }
      await queryClient.invalidateQueries({ queryKey: listWakersQueryKey() });
      onOpenChange(false);
    } catch {
      // React Query 保存错误，表单在字段附近展示稳定错误。
    }
  });

  const mutationError = create.error ?? update.error;
  const nameConflict =
    mutationError instanceof ApiError &&
    mutationError.code === "WakerNameConflict";
  const errorMessage =
    mutationError && !nameConflict ? "保存失败，请检查输入后重试。" : "";

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        onOpenChange(nextOpen);
      }}
    >
      <DialogContent
        layout="form"
        size="xl"
        title={waker ? "编辑伙伴" : "新建伙伴"}
      >
        <form
          onSubmit={submit}
          className="flex min-h-0 flex-1 flex-col overflow-hidden"
        >
          <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-5">
            <div className="grid gap-4">
              <div className="grid grid-cols-[1fr_1fr] gap-4">
                <label className="type-label grid gap-2 font-medium">
                  <span>
                    名称 <b className="text-destructive">*</b>
                  </span>
                  <Input placeholder="例如：小艾" {...form.register("name")} />
                  <span className="text-muted-foreground type-caption font-normal">
                    用于识别这位伙伴，名称不可与已有伙伴重复。
                  </span>
                  {form.formState.errors.name?.message ? (
                    <span className="text-destructive type-caption">
                      {form.formState.errors.name.message}
                    </span>
                  ) : nameConflict && mutationError instanceof ApiError ? (
                    <span className="text-destructive type-caption">
                      {mutationError.message}
                    </span>
                  ) : null}
                </label>
                <CompanionPicker
                  label="头像"
                  name={form.getValues("name") || "伙伴"}
                  value={avatar}
                  onChange={(value) =>
                    form.setValue("avatar", value, {
                      shouldDirty: true,
                      shouldValidate: true,
                    })
                  }
                />
              </div>

              {waker ? (
                <label className="type-label grid gap-2 font-medium">
                  <span>
                    角色名称 <b className="text-destructive">*</b>
                  </span>
                  <Input
                    placeholder="例如：前端开发工程师"
                    {...form.register("roleName")}
                  />
                  <span className="text-muted-foreground type-caption font-normal">
                    描述这位伙伴当前承担的角色，不再与创建时的角色预设关联。
                  </span>
                  {form.formState.errors.roleName?.message ? (
                    <span className="text-destructive type-caption">
                      {form.formState.errors.roleName.message}
                    </span>
                  ) : null}
                </label>
              ) : null}

              <label className="type-label grid gap-2 font-medium">
                简介
                <Textarea
                  placeholder="例如：专注于把复杂需求转化为清晰、可靠的产品实现。"
                  {...form.register("bio")}
                />
                {form.formState.errors.bio?.message ? (
                  <span className="text-destructive type-caption">
                    {form.formState.errors.bio.message}
                  </span>
                ) : null}
              </label>

              {!waker ? (
                <div>
                  <RolePresetPicker
                    selectedId={selectedPresetId}
                    onSelect={choosePreset}
                    onClearSelection={() =>
                      form.setValue("rolePresetId", "", {
                        shouldDirty: true,
                        shouldValidate: true,
                      })
                    }
                  />
                  {form.formState.errors.rolePresetId?.message ? (
                    <p className="text-destructive type-caption mt-2">
                      {form.formState.errors.rolePresetId.message}
                    </p>
                  ) : null}
                </div>
              ) : null}

              {waker ? (
                <ProfileManualEditor
                  expertise={expertise}
                  onExpertiseChange={setExpertise}
                  workStyle={workStyle}
                  onWorkStyleChange={setWorkStyle}
                  workMethods={workMethods}
                  onWorkMethodsChange={setWorkMethods}
                />
              ) : null}
            </div>
            {errorMessage ? (
              <p role="alert" className="text-destructive type-body mt-3">
                {errorMessage}
              </p>
            ) : null}
          </div>

          <DialogFooter>
            <Button
              type="submit"
              disabled={pending || (!waker && !selectedPresetId)}
            >
              {pending ? <LoaderCircle className="animate-spin" /> : null}
              {waker ? "保存修改" : "创建伙伴"}
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
