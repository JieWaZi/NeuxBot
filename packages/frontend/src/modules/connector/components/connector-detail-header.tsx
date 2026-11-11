import type { ComponentProps } from "react";
import { PageHeader } from "@/shared/ui/page-header";
import { ResourceCategoryIcon } from "@/shared/ui/resource-category-icon";

/** 独立连接器与插件固定版本共用同一目录身份头。 */
export function ConnectorDetailHeader({
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
      layout="detail"
      appearance="compact"
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
      visual={<ResourceCategoryIcon category={categories?.[0]} size="detail" />}
    />
  );
}
