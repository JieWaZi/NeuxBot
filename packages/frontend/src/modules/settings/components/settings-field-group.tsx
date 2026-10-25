import { useId, type ReactNode } from "react";

/** 设置参数共用浅底分组，同级字段之间只使用分隔线。 */
export function SettingsFieldGroup({
  title,
  description,
  children,
}: {
  title?: string;
  description?: string;
  children: ReactNode;
}) {
  const descriptionId = useId();
  return (
    <fieldset
      className="@container min-w-0"
      aria-describedby={description ? descriptionId : undefined}
    >
      {title ? (
        <legend className="type-body mb-2 font-medium">{title}</legend>
      ) : null}
      {description ? (
        <p id={descriptionId} className="type-ui text-content-secondary mb-3">
          {description}
        </p>
      ) : null}
      <div className="bg-surface-tertiary divide-border-subtle divide-y overflow-hidden rounded-lg">
        {children}
      </div>
    </fieldset>
  );
}

/** 窄容器改为上下排列，控件密度保持不变。 */
export function SettingsFieldRow({
  label,
  htmlFor,
  children,
}: {
  label: ReactNode;
  htmlFor?: string;
  children: ReactNode;
}) {
  return (
    <div className="grid min-w-0 grid-cols-1 items-center gap-2 px-4 py-3 @lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] @lg:gap-4">
      {htmlFor ? (
        <label
          htmlFor={htmlFor}
          className="type-ui text-content-primary font-medium"
        >
          {label}
        </label>
      ) : (
        <div className="type-ui text-content-primary font-medium">{label}</div>
      )}
      <div className="min-w-0">{children}</div>
    </div>
  );
}
