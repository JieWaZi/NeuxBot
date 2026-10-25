import type { ReactNode } from "react";
import { Link, useLocation } from "react-router-dom";
import { Eye, MoreHorizontal, Users } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Card, CardBody, CardFooter } from "./card";
import { Button } from "./button";
import { Avatar } from "./avatar";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "./dropdown-menu";

/** 已保存资源统一使用标题、简介和管理底栏；详情与独立操作拥有各自命中区。 */
export function ResourceManagementCard({
  title,
  description,
  icon,
  source,
  to,
  label,
  onClick,
  control,
  usage,
  actions,
  selected = false,
  selection,
}: {
  title: string;
  description?: string;
  icon: ReactNode;
  source?: string;
  to?: string;
  label: string;
  onClick?: () => void;
  control?: ReactNode;
  usage?: ReactNode;
  actions?: ReactNode;
  selected?: boolean;
  selection?: ReactNode;
}) {
  const location = useLocation();
  const linkClass =
    "focus-visible:ring-ring absolute inset-0 rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-inset";
  return (
    <Card
      asChild
      variant="resource"
      data-state={selected ? "selected" : undefined}
    >
      <article className="relative">
        {to ? (
          <Link
            to={to}
            state={{ resourceBack: location.pathname + location.search }}
            aria-label={label}
            title={[title, description].filter(Boolean).join("\n")}
            className={linkClass}
          />
        ) : (
          <button
            type="button"
            onClick={onClick}
            aria-label={label}
            title={[title, description].filter(Boolean).join("\n")}
            className={linkClass}
          />
        )}
        <CardBody className="pointer-events-none space-y-4 p-4">
          <div className="flex min-w-0 items-center gap-3">
            {icon}
            <div className="min-w-0 flex-1">
              <h2 className="type-label truncate font-medium" title={title}>
                {title}
              </h2>
              {source && (
                <p
                  className="type-caption text-content-caption mt-1 truncate"
                  title={source}
                >
                  {source}
                </p>
              )}
            </div>
            {control && (
              <div className="pointer-events-auto relative shrink-0">
                {control}
              </div>
            )}
            {selection && (
              <div className="pointer-events-auto relative shrink-0">
                {selection}
              </div>
            )}
          </div>
          <p
            className="type-body text-content-secondary truncate"
            title={description}
          >
            {description}
          </p>
        </CardBody>
        {(usage || actions) && (
          <CardFooter className="flex-wrap gap-2 px-4 py-2">
            <div className="pointer-events-none min-w-0 flex-1">{usage}</div>
            {actions && (
              <div className="relative flex shrink-0 items-center gap-1">
                {actions}
              </div>
            )}
          </CardFooter>
        )}
      </article>
    </Card>
  );
}

/** 使用数来自资源的实际伙伴关系；没有头像资料时展示中性的伙伴图标。 */
export function ResourcePartnerUsage({
  count,
  partners = [],
}: {
  count: number;
  partners?: { id: string; name: string; avatar?: string }[];
}) {
  const { t } = useTranslation("plugin");
  return (
    <span className="type-caption text-content-caption flex min-w-0 items-center gap-2">
      {partners.length ? (
        <span className="flex shrink-0 -space-x-2">
          {partners.slice(0, 4).map((partner) => (
            <span
              key={partner.id}
              className="relative shrink-0"
              title={partner.name}
            >
              <ResourcePartnerAvatar
                name={partner.name}
                avatar={partner.avatar}
                size="xs"
              />
            </span>
          ))}
        </span>
      ) : (
        <Users aria-hidden="true" className="size-icon-md shrink-0" />
      )}
      <span className="truncate">{t("partnerUsage", { count })}</span>
    </span>
  );
}

/** 只绘制真实记录中的头像；资料缺失不按名称生成另一种伙伴形象。 */
export function ResourcePartnerAvatar({
  name,
  avatar,
  size = "md",
}: {
  name: string;
  avatar?: string;
  size?: "xs" | "md";
}) {
  return avatar ? (
    <Avatar
      name={name}
      src={avatar}
      size={size}
      animated={false}
      interactive={false}
    />
  ) : (
    <Users
      aria-hidden="true"
      className={
        size === "xs"
          ? "text-content-caption size-avatar-xs shrink-0"
          : "text-content-caption size-avatar-md shrink-0"
      }
    />
  );
}

export function ResourceMoreAction({
  to,
  name,
  onView,
  viewLabel,
  children,
}: {
  to?: string;
  name: string;
  onView?: () => void;
  viewLabel?: string;
  children?: ReactNode;
}) {
  const { t } = useTranslation("plugin");
  const location = useLocation();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={t("moreNamed", { name })}
        >
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {to ? (
          <DropdownMenuItem asChild>
            <Link
              to={to}
              state={{ resourceBack: location.pathname + location.search }}
            >
              <Eye />
              {viewLabel ?? t("details")}
            </Link>
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem onSelect={onView}>
            <Eye />
            {viewLabel ?? t("details")}
          </DropdownMenuItem>
        )}
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
