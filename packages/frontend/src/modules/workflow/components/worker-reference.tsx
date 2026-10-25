import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { getWakerOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { Avatar } from "@/shared/ui/avatar";
import { record } from "../model/types";

/** 使用真实目录展示伙伴身份，定义上的模型未覆盖时明确显示继承语义。 */
export function WorkerReference({
  resolve,
  showModel = true,
}: {
  resolve: unknown;
  showModel?: boolean;
}) {
  const { t } = useTranslation("workflow");
  const binding = record(resolve);
  const wakerId = typeof binding?.wakerId === "string" ? binding.wakerId : "";
  const waker = useQuery({
    ...getWakerOptions({ path: { wakerId } }),
    enabled: Boolean(wakerId),
    staleTime: 30_000,
    retry: false,
  });
  const model = typeof binding?.model === "string" ? binding.model : "";
  return (
    <span className="text-muted-foreground type-caption flex min-w-0 items-center gap-2">
      {waker.data ? (
        <Avatar
          name={waker.data.name ?? ""}
          src={waker.data.avatar}
          size="xs"
          animated={false}
          interactive={false}
        />
      ) : null}
      <span className="grid min-w-0 gap-1">
        <span className="truncate">
          {waker.data?.name ||
            (wakerId
              ? waker.isError
                ? t("unresolvedWaker", { id: wakerId })
                : t("loadingWaker", { id: wakerId })
              : t("unboundWaker"))}
        </span>
        {showModel ? (
          <span className="truncate">{model || t("inheritedModel")}</span>
        ) : null}
      </span>
    </span>
  );
}
