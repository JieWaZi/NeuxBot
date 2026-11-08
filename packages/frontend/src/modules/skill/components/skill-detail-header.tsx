import type { ComponentProps } from "react";
import { Sparkles } from "lucide-react";
import { PageHeader } from "@/shared/ui/page-header";

/** 独立技能与插件固定版本共用目录身份头。 */
export function SkillDetailHeader({
  description,
  ...props
}: Pick<
  ComponentProps<typeof PageHeader>,
  "title" | "description" | "status" | "backAction" | "actions"
>) {
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
      visual={
        <span
          aria-hidden="true"
          className="bg-resource-raised border-resource-border text-resource-secondary size-resource-detail-icon grid shrink-0 place-items-center rounded-lg border"
        >
          <Sparkles className="size-resource-logo" strokeWidth={1.5} />
        </span>
      }
    />
  );
}
