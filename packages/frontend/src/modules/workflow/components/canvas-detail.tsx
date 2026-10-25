import { type ComponentProps, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { DialogContent } from "@/shared/ui/dialog";
import { Badge } from "@/shared/ui/badge";
import { workflowStatusAppearance } from "../model/status-presentation";

/** 参数、阶段与节点共用独立于画布平移缩放的居中浮层。 */
export function CanvasDetail({
  title,
  status,
  children,
  scrollable = true,
  "aria-label": ariaLabel = title,
  ...props
}: Omit<ComponentProps<typeof DialogContent>, "title" | "children"> & {
  title: string;
  status?: string;
  children: ReactNode;
  scrollable?: boolean;
}) {
  const { t } = useTranslation("workflow");
  return (
    <DialogContent
      {...props}
      size="lg"
      aria-label={ariaLabel}
      aria-labelledby={undefined}
      aria-describedby={undefined}
      data-workflow-canvas-details
      className="pointer-events-auto"
      title={
        <span className="flex min-w-0 flex-wrap items-center gap-2">
          {status ? (
            <Badge variant={workflowStatusAppearance(status).badge}>
              {t(`status.${status}`)}
            </Badge>
          ) : null}
          <span>{title}</span>
        </span>
      }
    >
      {scrollable ? (
        <div className="min-h-0 overflow-y-auto overscroll-contain">
          {children}
        </div>
      ) : (
        children
      )}
    </DialogContent>
  );
}
