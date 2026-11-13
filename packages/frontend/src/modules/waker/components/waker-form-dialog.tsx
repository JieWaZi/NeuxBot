import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle } from "lucide-react";
import { useId, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { ApiError } from "@/shared/api/errors";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import {
  companionAvatar,
  isCompanionAvatar,
} from "@/shared/companion/appearance";
import { Button } from "@/shared/ui/button";
import { CompanionPicker } from "@/shared/ui/companion-picker";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";
import {
  getWakerQueryKey,
  listWakersQueryKey,
  updateWakerMutation,
} from "../api/waker-queries";
import type { Waker, WakerDefaultModel } from "../model/types";
import {
  cleanManualProfile,
  ProfileManualEditor,
} from "./profile-manual-editor";
import { WakerDefaultModelField } from "./waker-default-model-field";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  waker: Waker;
};

export function WakerFormDialog(props: Props) {
  return props.open ? <OpenWakerFormDialog {...props} /> : null;
}

function OpenWakerFormDialog({ onOpenChange, waker }: Props) {
  const { t } = useTranslation(["waker", "common"]);
  const id = useId();
  const queryClient = useQueryClient();
  const [initial] = useState(() => ({
    expertise: waker.expertise ?? [],
    workStyle: waker.workStyle ?? [],
    workMethods: waker.workMethods ?? [],
    overviewRevision: waker.overviewRevision,
    defaultModel: waker.defaultModel,
  }));
  const [expertise, setExpertise] = useState(initial.expertise);
  const [workStyle, setWorkStyle] = useState(initial.workStyle);
  const [workMethods, setWorkMethods] = useState(initial.workMethods);
  const [defaultModel, setDefaultModel] = useState<
    WakerDefaultModel | undefined
  >(initial.defaultModel);
  const schema = z.object({
    name: z
      .string()
      .trim()
      .min(1, t("form.nameRequired"))
      .max(80, t("form.nameTooLong")),
    roleName: z.string().trim().max(80, t("form.roleNameTooLong")),
    bio: z.string().trim().max(1000, t("form.bioTooLong")),
    avatar: z
      .string()
      .trim()
      .refine(isCompanionAvatar, t("form.avatarInvalid")),
  });
  const form = useForm({
    resolver: zodResolver(schema),
    defaultValues: {
      name: waker.name ?? "",
      roleName: waker.roleName ?? "",
      bio: waker.bio ?? "",
      avatar: companionAvatar(waker.avatar, waker.name ?? ""),
    },
  });
  const avatar = useWatch({ control: form.control, name: "avatar" });
  const name = useWatch({ control: form.control, name: "name" });
  const update = useMutation(withLocalErrorMutation(updateWakerMutation()));
  const pending = form.formState.isSubmitting;
  const errors = form.formState.errors;

  const submit = form.handleSubmit(async (values) => {
    if (!waker.id) return;
    const wakerQuery = getWakerQueryKey({ path: { wakerId: waker.id } });
    const overviewChanged =
      expertise !== initial.expertise ||
      workStyle !== initial.workStyle ||
      workMethods !== initial.workMethods;
    const refresh = () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: wakerQuery }),
        queryClient.invalidateQueries({ queryKey: listWakersQueryKey() }),
      ]);
    try {
      await update.mutateAsync({
        path: { wakerId: waker.id },
        body: {
          ...values,
          ...(defaultModel === initial.defaultModel
            ? {}
            : { defaultModel: defaultModel ?? {} }),
          ...(overviewChanged
            ? {
                manual: cleanManualProfile(expertise, workStyle, workMethods),
                expectedOverviewRevision: initial.overviewRevision,
              }
            : {}),
        },
      });
      await refresh();
      onOpenChange(false);
    } catch (error) {
      if (error instanceof ApiError && error.code === "WakerProfileChanged")
        void refresh();
    }
  });

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!pending) onOpenChange(open);
      }}
    >
      <DialogContent layout="form" size="xl" title={t("form.edit")}>
        <form
          onSubmit={submit}
          className="flex min-h-0 flex-1 flex-col overflow-hidden"
        >
          <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-5">
            <fieldset disabled={pending} className="grid min-w-0 gap-4">
              <CompanionPicker
                label={t("form.avatar")}
                name={name || t("form.avatar")}
                value={avatar}
                onChange={(value) =>
                  form.setValue("avatar", value, {
                    shouldDirty: true,
                    shouldValidate: true,
                  })
                }
              />
              {errors.avatar?.message ? (
                <p role="alert" className="text-destructive type-caption">
                  {errors.avatar.message}
                </p>
              ) : null}
              <div className="grid gap-2">
                <FieldLabel htmlFor={`${id}-name`} required>
                  {t("form.name")}
                </FieldLabel>
                <Input
                  id={`${id}-name`}
                  required
                  placeholder={t("form.namePlaceholder")}
                  {...form.register("name")}
                />
                {errors.name?.message ? (
                  <p role="alert" className="text-destructive type-caption">
                    {errors.name.message}
                  </p>
                ) : null}
              </div>
              <div className="grid gap-2">
                <FieldLabel htmlFor={`${id}-role`}>
                  {t("form.roleName")}
                </FieldLabel>
                <Input
                  id={`${id}-role`}
                  placeholder={t("form.roleNamePlaceholder")}
                  {...form.register("roleName")}
                />
                {errors.roleName?.message ? (
                  <p role="alert" className="text-destructive type-caption">
                    {errors.roleName.message}
                  </p>
                ) : null}
              </div>
              <div className="grid gap-2">
                <FieldLabel htmlFor={`${id}-bio`}>{t("form.bio")}</FieldLabel>
                <Textarea
                  id={`${id}-bio`}
                  placeholder={t("form.bioPlaceholder")}
                  {...form.register("bio")}
                />
                {errors.bio?.message ? (
                  <p role="alert" className="text-destructive type-caption">
                    {errors.bio.message}
                  </p>
                ) : null}
              </div>
              <WakerDefaultModelField
                value={defaultModel}
                onChange={setDefaultModel}
                disabled={pending}
              />
              <ProfileManualEditor
                expertise={expertise}
                onExpertiseChange={setExpertise}
                workStyle={workStyle}
                onWorkStyleChange={setWorkStyle}
                workMethods={workMethods}
                onWorkMethodsChange={setWorkMethods}
              />
            </fieldset>
            {update.error ? (
              <p role="alert" className="text-destructive type-body mt-3">
                {localApiErrorMessage(update.error, t("form.saveError"))}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button type="submit" disabled={pending}>
              {pending ? <LoaderCircle className="animate-spin" /> : null}
              {t("form.save")}
            </Button>
            <Button
              type="button"
              variant="secondary"
              disabled={pending}
              onClick={() => onOpenChange(false)}
            >
              {t("common:cancel")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
