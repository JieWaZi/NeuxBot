import type { ComponentProps } from "react";
import { Cable, Terminal } from "lucide-react";
import { PageHeader } from "@/shared/ui/page-header";

/** 独立连接器与插件固定版本共用同一目录身份头。 */
export function ConnectorDetailHeader({
  description,
  transport,
  ...props
}: Pick<
  ComponentProps<typeof PageHeader>,
  "title" | "description" | "status" | "backAction" | "actions"
> & { transport?: string }) {
  const Icon = transport === "stdio" ? Terminal : Cable;
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
      visual={
        <span
          aria-hidden="true"
          className="bg-resource-raised border-resource-border text-resource-secondary size-resource-detail-icon grid shrink-0 place-items-center rounded-lg border"
        >
          <Icon className="size-resource-logo" strokeWidth={1.5} />
        </span>
      }
    />
  );
}
