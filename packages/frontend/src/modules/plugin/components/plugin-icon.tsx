import { Package } from "lucide-react";
import { cn } from "@/shared/lib/cn";
import { ResourceCategoryIcon } from "@/shared/ui/resource-category-icon";
import type { PluginCatalogEntry } from "../model/plugin-view";

type PluginIcons = NonNullable<
  NonNullable<PluginCatalogEntry["package"]>["presentation"]
>["icons"];

/** 只呈现包内已校验图标，缺失时使用统一包占位。 */
export function PluginIcon({
  pkg,
  icon,
  icons,
  hero = false,
  size = "list",
  category,
}: {
  pkg?: PluginCatalogEntry["package"];
  icon?: string;
  icons?: PluginIcons;
  hero?: boolean;
  size?: "list" | "detail" | "navigation" | "mention";
  category?: string;
}) {
  const themedIcons = icons ?? pkg?.presentation?.icons;
  const lightIcon = themedIcons?.default ?? icon ?? pkg?.icon;
  const defaultIcon = lightIcon ?? themedIcons?.dark;
  const darkIcon = themedIcons?.dark ?? defaultIcon;
  if (!defaultIcon && !hero && (size === "list" || size === "detail")) {
    return (
      <ResourceCategoryIcon
        category={category ?? pkg?.presentation?.category}
        size={size}
      />
    );
  }
  const imageSize = hero
    ? "size-10"
    : size === "navigation"
      ? "size-icon-md"
      : size === "mention"
        ? "size-icon-inline"
        : "size-resource-brand-logo";
  // 用户气泡和应用表面的明暗相反，单套品牌图沿用其原本适配的底色。
  const mentionSurface =
    darkIcon !== defaultIcon
      ? "bg-resource-raised"
      : lightIcon
        ? "bg-resource-raised dark:bg-user-message-background"
        : "bg-user-message-background dark:bg-resource-raised";
  return (
    <span
      aria-hidden="true"
      className={cn(
        "grid shrink-0 place-items-center",
        (hero || size !== "mention") && "text-content-primary",
        hero
          ? "[&_svg]:size-icon-xl size-14"
          : size === "detail"
            ? "bg-resource-raised border-resource-border size-resource-detail-icon [&_svg]:size-resource-logo overflow-hidden rounded-lg border"
            : size === "navigation"
              ? "[&_svg]:size-icon-md size-resource-navigation-icon"
              : size === "mention"
                ? defaultIcon
                  ? cn("size-icon-md rounded-xs", mentionSurface)
                  : "size-icon-inline [&_svg]:size-icon-inline"
                : "bg-resource-raised border-resource-border size-resource-icon [&_svg]:size-resource-logo overflow-hidden rounded-md border",
      )}
    >
      {defaultIcon ? (
        <>
          <img
            src={defaultIcon}
            alt=""
            className={cn(
              darkIcon !== defaultIcon && "dark:hidden",
              "col-start-1 row-start-1 object-contain object-center",
              imageSize,
            )}
          />
          {darkIcon && darkIcon !== defaultIcon && (
            <img
              src={darkIcon}
              alt=""
              className={cn(
                "hidden dark:block",
                "col-start-1 row-start-1 object-contain object-center",
                imageSize,
              )}
            />
          )}
        </>
      ) : (
        <Package strokeWidth={1.5} />
      )}
    </span>
  );
}
