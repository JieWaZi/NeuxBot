import { useQueries, useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  getWakerOptions,
  listWakersOptions,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { useExecutionSelection } from "@/shared/hooks/use-execution-selection";
import { Avatar } from "@/shared/ui/avatar";
import { EmptyState } from "@/shared/ui/empty-state";
import { ExecutionConfigurationControls } from "@/shared/ui/execution-configuration-controls";
import { FieldLabel } from "@/shared/ui/field-label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";
import { WorkflowLoadingState, WorkflowQueryError } from "./query-feedback";
import type { ExecutionDefaults } from "../model/types";

const partnerPageSize = 100;

function partnerPageOptions(pageNum: number) {
  return listWakersOptions(
    withLocalErrorSurface({ query: { pageNum, pageSize: partnerPageSize } }),
  );
}

/** 节点表单复用真实伙伴目录与执行配置控件，选择只更新所属草稿。 */
export function WorkerConfigurationFields({
  fieldId,
  resolve,
  defaults,
  disabled,
  onChange,
}: {
  fieldId: string;
  resolve: Record<string, unknown>;
  defaults?: ExecutionDefaults;
  disabled: boolean;
  onChange: (resolve: Record<string, unknown>) => void;
}) {
  const { t } = useTranslation("workflow");
  const value = (key: string) =>
    typeof resolve[key] === "string" ? resolve[key] : undefined;
  const runtimeId = value("runtimeId") || defaults?.runtimeId;
  const sameRuntime = runtimeId === defaults?.runtimeId;
  // 明确的空值使用智能体当前默认值。上级选择变化时，下级值不继续继承创作会话。
  const accountId =
    value("accountId") ?? (sameRuntime ? defaults?.accountId : undefined);
  const sameAccount =
    sameRuntime && (accountId ?? "") === (defaults?.accountId ?? "");
  const model = value("model") ?? (sameAccount ? defaults?.model : undefined);
  const sameModel = sameAccount && (model ?? "") === (defaults?.model ?? "");
  const execution = useExecutionSelection({
    value: {
      runtimeId,
      accountId,
      model,
      reasoning:
        value("reasoning") ?? (sameModel ? defaults?.reasoning : undefined),
      permissionLevel: value("permissionLevel") || defaults?.permissionLevel,
    },
  });
  const firstPage = useQuery(partnerPageOptions(1));
  const additionalPages = useQueries({
    queries: Array.from(
      {
        length: Math.max(
          0,
          Math.ceil((firstPage.data?.totalSize ?? 0) / partnerPageSize) - 1,
        ),
      },
      (_, index) => partnerPageOptions(index + 2),
    ),
  });
  const pages = [firstPage, ...additionalPages];
  const partners = new Map(
    pages.flatMap((page) =>
      (page.data?.resources ?? []).flatMap((partner) =>
        partner.id ? [[partner.id, partner] as const] : [],
      ),
    ),
  );
  const partnerId = value("wakerId") || "";
  const selectedPartner = useQuery({
    ...getWakerOptions(withLocalErrorSurface({ path: { wakerId: partnerId } })),
    enabled: Boolean(partnerId && firstPage.data && !partners.has(partnerId)),
  });
  if (selectedPartner.data?.id === partnerId)
    partners.set(partnerId, partners.get(partnerId) ?? selectedPartner.data);
  const directoryLoading = pages.some((page) => page.isPending);
  const failedPage = pages.find((page) => page.isError);
  return (
    <>
      <div className="grid gap-2">
        <FieldLabel htmlFor={`${fieldId}-partner`}>
          {t("assignedPartner")}
        </FieldLabel>
        <Select
          value={partners.has(partnerId) ? partnerId : ""}
          disabled={disabled || !partners.size}
          onValueChange={(wakerId) => {
            if (partners.has(wakerId)) onChange({ ...resolve, wakerId });
          }}
        >
          <SelectTrigger
            id={`${fieldId}-partner`}
            aria-label={t("assignedPartner")}
          >
            <SelectValue
              placeholder={t(
                !partnerId
                  ? "unboundWaker"
                  : selectedPartner.isFetching || directoryLoading
                    ? "loadingWaker"
                    : "unresolvedWaker",
              )}
            />
          </SelectTrigger>
          <SelectContent>
            {[...partners].map(([id, waker]) => (
              <SelectItem key={id} value={id}>
                <span className="flex items-center gap-2">
                  <Avatar
                    name={waker.name ?? ""}
                    src={waker.avatar}
                    size="xs"
                    animated={false}
                    interactive={false}
                  />
                  <span>{waker.name}</span>
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {directoryLoading ? <WorkflowLoadingState /> : null}
        {failedPage ? (
          <WorkflowQueryError
            error={failedPage.error}
            retrying={failedPage.isFetching}
            onRetry={() => void failedPage.refetch()}
          />
        ) : null}
        {!directoryLoading && !failedPage && !partners.size ? (
          <EmptyState
            density="compact"
            title={t("common:partnerNavigation.noPartner")}
          />
        ) : null}
      </div>
      <div className="grid gap-2">
        <FieldLabel>{t("configuration")}</FieldLabel>
        <div className="flex flex-wrap gap-2">
          <ExecutionConfigurationControls
            state={execution}
            disabled={disabled}
            onChange={(next) => {
              const patch: Record<string, unknown> = {};
              for (const key of [
                "runtimeId",
                "accountId",
                "model",
                "reasoning",
                "permissionLevel",
              ] as const) {
                if (next[key] !== execution.selection[key])
                  patch[key] = next[key];
              }
              onChange({ ...resolve, ...patch });
            }}
          />
        </div>
      </div>
    </>
  );
}
