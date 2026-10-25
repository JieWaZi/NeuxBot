import type { LabelHTMLAttributes } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";

/** 表单标签共用必填标记；控件另行声明 required / aria-required。 */
export function FieldLabel({
  required = false,
  children,
  className,
  ...props
}: LabelHTMLAttributes<HTMLLabelElement> & { required?: boolean }) {
  const { t } = useTranslation("common");
  return (
    <label
      className={cn("type-ui flex items-center gap-1 font-medium", className)}
      {...props}
    >
      {children}
      {required ? (
        <span
          aria-hidden="true"
          title={t("fieldRequired")}
          className="text-destructive"
        >
          *
        </span>
      ) : null}
    </label>
  );
}
