import { useQuery } from "@tanstack/react-query";
import { useId } from "react";
import { useTranslation } from "react-i18next";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import { listResourceCategoriesOptions } from "@/shared/api/generated/@tanstack/react-query.gen";
import { CreatableMultiSelect } from "./creatable-multi-select";
import { QueryRefreshError } from "./query-state";

/** 三类资源共用词库查询与标签输入；创建随所属表单保存，不在编辑草稿时写库。 */
export function ResourceCategoryField({
  type,
  value,
  onValueChange,
  disabled,
}: {
  type: "plugin" | "skill" | "connector";
  value: readonly string[];
  onValueChange: (value: string[]) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation("common");
  const id = useId();
  const query = useQuery(
    listResourceCategoriesOptions(withLocalErrorSurface({ query: { type } })),
  );
  return (
    <div className="grid min-w-0 gap-1.5">
      <label
        htmlFor={id}
        className="type-resource-caption text-resource-secondary font-medium"
      >
        {t("resourceCategory.label")}
      </label>
      <CreatableMultiSelect
        appearance="catalog"
        id={id}
        label={t("resourceCategory.label")}
        placeholder={t("resourceCategory.placeholder")}
        value={value}
        options={(query.data ?? []).flatMap((item) =>
          item.name ? [item.name] : [],
        )}
        onValueChange={onValueChange}
        disabled={disabled}
        createLabel={(name) => t("resourceCategory.create", { name })}
        removeLabel={(name) => t("resourceCategory.remove", { name })}
        emptyLabel={t("resourceCategory.empty")}
        loading={query.isPending}
        loadingLabel={t("resourceCategory.loading")}
      />
      {query.isError && (
        <QueryRefreshError
          error={query.error}
          pending={query.isFetching}
          onRetry={() => void query.refetch()}
        />
      )}
    </div>
  );
}
