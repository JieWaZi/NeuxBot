import type { ComponentProps } from "react";
import { PageHeader } from "@/shared/ui/page-header";
import { ResourceCategoryIcon } from "@/shared/ui/resource-category-icon";

/** 独立技能与插件固定版本共用目录身份头。 */
export function SkillDetailHeader({
  categories,
  description,
  ...props
}: Pick<
  ComponentProps<typeof PageHeader>,
  "title" | "description" | "status" | "backAction" | "actions"
> & { categories?: string[] }) {
  return (
    <PageHeader
      {...props}
      description={
        description ? (
          <span
            className="block truncate"
            title={typeof description === "string" ? description : undefined}
          >
            {description}
          </span>
        ) : undefined
      }
      layout="detail"
      appearance="compact"
      visual={<ResourceCategoryIcon category={categories?.[0]} size="detail" />}
    />
  );
}
