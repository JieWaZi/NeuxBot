import { useId, type ReactNode } from "react";

/** 字段共用设置语义表面；已在Card内时使用plain，避免嵌套表面。 */
export function SettingsFieldGroup({
  title,
  description,
  children,
  surface = "card",
}: {
  surface?: "card" | "plain";
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
      <div
        data-slot={surface === "card" ? "settings-card" : undefined}
        className={
          surface === "card"
            ? "bg-resource-surface overflow-hidden rounded-lg"
            : "min-w-0"
        }
      >
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
    <div className="before:bg-border-subtle min-h-row-detail relative grid min-w-0 grid-cols-1 items-center gap-2 px-4 py-3 before:absolute before:inset-x-4 before:top-0 before:h-px first:before:hidden @lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] @lg:gap-4">
      {htmlFor ? (
        <label
          htmlFor={htmlFor}
          className="type-label text-content-primary font-medium"
        >
          {label}
        </label>
      ) : (
        <div className="type-label text-content-primary font-medium">
          {label}
        </div>
      )}
      <div className="min-w-0">{children}</div>
    </div>
  );
}
