import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Cpu } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { PageAction } from "@/shared/ui/resource-page";
import { toast } from "@/shared/ui/sonner";
import {
  getWakerQueryKey,
  listWakersQueryKey,
  updateWakerDefaultModelMutation,
} from "../api/waker-queries";
import type { WakerDefaultModel } from "../model/types";
import { WakerDefaultModelField } from "./waker-default-model-field";

export function WakerDefaultModelAction({
  wakerId,
  value,
}: {
  wakerId: string;
  value?: WakerDefaultModel;
}) {
  const { t } = useTranslation("waker");
  const client = useQueryClient();
  const update = useMutation({
    ...withLocalErrorMutation(updateWakerDefaultModelMutation()),
    onSuccess: () =>
      Promise.all([
        client.invalidateQueries({
          queryKey: getWakerQueryKey({ path: { wakerId } }),
        }),
        client.invalidateQueries({ queryKey: listWakersQueryKey() }),
      ]),
    onError: (error) => {
      toast.error(localApiErrorMessage(error, t("form.saveError")));
    },
  });

  return (
    <WakerDefaultModelField
      value={value}
      saving={update.isPending}
      onChange={(defaultModel) =>
        update.mutate({
          path: { wakerId },
          body: { defaultModel: defaultModel ?? {} },
        })
      }
      trigger={
        <PageAction
          intent="edit"
          label={t("form.defaultModel")}
          loading={update.isPending}
        >
          <Cpu />
        </PageAction>
      }
    />
  );
}
