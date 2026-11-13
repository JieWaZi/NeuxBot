import { useMutation, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle } from "lucide-react";
import { useId, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";

import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { randomCompanionAvatar } from "@/shared/companion/appearance";
import { chatHref } from "@/shared/lib/chat-navigation";
import { Button } from "@/shared/ui/button";
import { CompanionPicker } from "@/shared/ui/companion-picker";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { Textarea } from "@/shared/ui/textarea";
import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { createWakerMutation, listWakersQueryKey } from "../api/waker-queries";
import { WakerDefaultModelField } from "../components/waker-default-model-field";
import type { WakerDefaultModel } from "../model/types";

export function WakerCreatePage() {
  const { t } = useTranslation("waker");
  const navigate = useNavigate();
  const client = useQueryClient();
  const create = useMutation(withLocalErrorMutation(createWakerMutation()));
  const id = useId();
  const [avatar, setAvatar] = useState(() => randomCompanionAvatar());
  const [name, setName] = useState("");
  const [bio, setBio] = useState("");
  const [defaultModel, setDefaultModel] = useState<WakerDefaultModel>();
  const [validationError, setValidationError] = useState("");
  // 输入完全相同的重试复用标识，即使中途编辑后又恢复了原输入。
  const requests = useRef(new Map<string, string>());
  const submitting = useRef(false);
  const sceneActive = useRouteSceneActive();
  const canNavigate = useRef(false);
  useLayoutEffect(() => {
    canNavigate.current = sceneActive;
    return () => {
      canNavigate.current = false;
    };
  }, [sceneActive]);
  const openPartner = ({
    wakerId,
    chatId,
    startError,
  }: NonNullable<typeof create.data>) => {
    if (!wakerId) return;
    if (chatId) navigate(chatHref(chatId, wakerId), { replace: true });
    else
      navigate(`/wakers/${encodeURIComponent(wakerId)}`, {
        replace: true,
        state: { onboardingStartError: startError },
      });
  };
  const accepted = Boolean(create.data?.wakerId);
  const disabled = create.isPending || accepted;

  return (
    <ResourcePageSurface contentClassName="flex flex-col pb-4">
      <form
        aria-label={t("create")}
        className="m-auto grid w-full max-w-[26.25rem] shrink-0 gap-3 py-4"
        onSubmit={(event) => {
          event.preventDefault();
          if (submitting.current || accepted) return;
          if (!name.trim()) {
            setValidationError(t("form.nameRequired"));
            return;
          }
          setValidationError("");
          const body = {
            name: name.trim(),
            bio: bio.trim(),
            avatar,
            defaultModel,
          };
          const identity = JSON.stringify(body);
          const requestId =
            requests.current.get(identity) ?? crypto.randomUUID();
          requests.current.set(identity, requestId);
          submitting.current = true;
          void create
            .mutateAsync({ body: { ...body, requestId } })
            .then((result) => {
              if (!result.wakerId) {
                setValidationError(t("form.createError"));
                return;
              }
              void client.invalidateQueries({ queryKey: listWakersQueryKey() });
              if (canNavigate.current) openPartner(result);
            })
            .catch(() => {
              // Mutation 保留失败；同一份表单可以原样重试。
            })
            .finally(() => {
              submitting.current = false;
            });
        }}
      >
        <fieldset disabled={disabled} className="grid min-w-0 gap-3">
          <CompanionPicker
            label={t("form.avatar")}
            name={name || t("form.avatar")}
            value={avatar}
            onChange={setAvatar}
            inline
          />
          <div className="grid gap-2">
            <FieldLabel htmlFor={`${id}-name`} required>
              {t("form.name")}
            </FieldLabel>
            <Input
              id={`${id}-name`}
              placeholder={t("form.namePlaceholder")}
              required
              maxLength={80}
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <FieldLabel htmlFor={`${id}-bio`}>{t("form.bio")}</FieldLabel>
            <Textarea
              id={`${id}-bio`}
              size="compact"
              rows={2}
              placeholder={t("form.bioPlaceholder")}
              maxLength={1000}
              value={bio}
              onChange={(event) => setBio(event.target.value)}
            />
          </div>
          <WakerDefaultModelField
            value={defaultModel}
            onChange={setDefaultModel}
            disabled={disabled}
            layout="row"
          />
        </fieldset>
        {validationError || create.error ? (
          <p role="alert" className="text-destructive type-body text-center">
            {validationError ||
              localApiErrorMessage(create.error, t("form.createError"))}
          </p>
        ) : null}
        <div className="mt-1 flex justify-center">
          {accepted && create.data ? (
            <Button
              type="button"
              className="min-w-32"
              onClick={() => create.data && openPartner(create.data)}
            >
              {t("form.openPartner")}
            </Button>
          ) : (
            <Button
              type="submit"
              className="min-w-32"
              disabled={create.isPending}
            >
              {create.isPending ? (
                <LoaderCircle className="animate-spin" />
              ) : null}
              {t("form.create")}
            </Button>
          )}
        </div>
      </form>
    </ResourcePageSurface>
  );
}
