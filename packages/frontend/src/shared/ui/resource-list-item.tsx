import type { ReactNode, ComponentProps } from "react";
import { Link, useLocation } from "react-router-dom";
import { Card, CardBody } from "./card";
import { Button } from "./button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "./tooltip";

/** 资源目录使用轻量条目；详情链接与安装、选择等操作保持独立命中区。 */
export function ResourceListItem({
  title,
  description,
  icon,
  to,
  label,
  metadata,
  footer,
  actions,
  selected,
  selection,
  appearance = "card",
  onClick,
  descriptionLines = 2,
  headingLevel = 2,
}: {
  title: string;
  description?: string;
  icon: ReactNode;
  to?: string;
  onClick?: () => void;
  descriptionLines?: 1 | 2;
  headingLevel?: 2 | 3;
  label: string;
  metadata?: ReactNode;
  footer?: ReactNode;
  actions?: ReactNode;
  selected?: boolean;
  selection?: ReactNode;
  appearance?: "card" | "row";
}) {
  const location = useLocation();
  const Heading = headingLevel === 3 ? "h3" : "h2";
  const hitProps = {
    "aria-label": label,
    title: [title, description].filter(Boolean).join("\n"),
    className:
      "catalog-focus focus-visible:ring-ring absolute inset-0 rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-inset",
  };
  if (appearance === "row")
    return (
      <article
        data-slot="resource-list-item"
        data-appearance="row"
        data-state={selected ? "selected" : undefined}
        className="duration-quick hover:bg-resource-hover data-[state=selected]:bg-resource-hover text-resource-content relative flex min-w-0 items-center gap-3 rounded-lg p-2 transition-colors motion-reduce:transition-none"
      >
        {to ? (
          <Link
            {...hitProps}
            to={to}
            state={{ resourceBack: location.pathname + location.search }}
          />
        ) : (
          <button {...hitProps} type="button" onClick={onClick} />
        )}
        {selection && <div className="relative shrink-0">{selection}</div>}
        <div className="pointer-events-none shrink-0">{icon}</div>
        <div className="pointer-events-none min-w-0 flex-1 space-y-0.5">
          <Heading
            className="type-resource-row-title truncate font-medium"
            title={title}
          >
            {title}
          </Heading>
          {description && (
            <p
              className="type-resource-row-description text-resource-muted truncate"
              title={description}
            >
              {description}
            </p>
          )}
          {(metadata || footer) && (
            <div className="type-caption text-content-caption flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
              {metadata}
              {footer}
            </div>
          )}
        </div>
        {actions && (
          <div className="relative flex max-w-full shrink-0 flex-wrap items-center gap-1">
            {actions}
          </div>
        )}
      </article>
    );
  return (
    <Card
      asChild
      variant="resource"
      data-slot="resource-list-item"
      data-state={selected ? "selected" : undefined}
    >
      <article className="relative">
        <CardBody className="flex min-w-0 items-start gap-3 p-4">
          {to ? (
            <Link
              to={to}
              state={{ resourceBack: location.pathname + location.search }}
              aria-label={label}
              title={[title, description].filter(Boolean).join("\n")}
              className="focus-visible:ring-ring absolute inset-0 rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-inset"
            />
          ) : (
            <button
              type="button"
              onClick={onClick}
              aria-label={label}
              title={[title, description].filter(Boolean).join("\n")}
              className="focus-visible:ring-ring absolute inset-0 rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-inset"
            />
          )}
          {selection && <div className="relative shrink-0">{selection}</div>}
          {icon}
          <div className="pointer-events-none flex min-w-0 flex-1 flex-col self-stretch">
            <Heading className="type-label truncate font-medium">
              {title}
            </Heading>
            <p
              className={`type-body text-content-secondary mt-1 ${descriptionLines === 1 ? "truncate" : "line-clamp-2"}`}
            >
              {description}
            </p>
            {metadata && (
              <div className="type-caption text-content-caption mt-2 flex flex-wrap items-center gap-x-3 gap-y-1">
                {metadata}
              </div>
            )}
            {footer && (
              <div className="type-caption text-content-secondary min-h-control-sm mt-auto flex flex-wrap items-center justify-between gap-2 pt-4">
                {footer}
              </div>
            )}
          </div>
          {actions && (
            <div className="relative flex shrink-0 items-center gap-1">
              {actions}
            </div>
          )}
        </CardBody>
      </article>
    </Card>
  );
}

export function ResourceItemAction({
  label,
  children,
  ...props
}: Omit<ComponentProps<typeof Button>, "size"> & { label: string }) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button {...props} size="icon" aria-label={label}>
            {children}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
