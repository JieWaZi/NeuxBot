import type { ComponentProps } from "react";

import { DialogContent } from "@/shared/ui/dialog";

type Props = Omit<ComponentProps<typeof DialogContent>, "layout" | "size">;

/** 统一承载记忆设置、编辑与完整列表的工作型弹窗。 */
export function MemoryWorkspaceDialogContent(props: Props) {
  return <DialogContent {...props} layout="form" size="lg" />;
}
