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
  size?: "list" | "detail" | "navigation";
  category?: string;
}) {
  const themedIcons = icons ?? pkg?.presentation?.icons;
  const defaultIcon =
    themedIcons?.default ?? icon ?? pkg?.icon ?? themedIcons?.dark;
  const darkIcon = themedIcons?.dark ?? defaultIcon;
  if (!defaultIcon && !hero && size !== "navigation") {
    return (
      <ResourceCategoryIcon
        category={category ?? pkg?.presentation?.category}
        size={size}
      />
    );
  }
  return (
    <span
      aria-hidden="true"
      className={cn(
        "text-content-primary grid shrink-0 place-items-center",
        hero
          ? "[&_svg]:size-icon-xl size-14"
          : size === "detail"
            ? "bg-resource-raised border-resource-border size-resource-detail-icon [&_svg]:size-resource-logo overflow-hidden rounded-lg border"
            : size === "navigation"
              ? "[&_svg]:size-icon-md size-resource-navigation-icon"
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
              !hero && size !== "navigation" && "size-resource-brand-logo",
              hero
                ? "size-10"
                : size === "navigation"
                  ? "size-icon-md"
                  : undefined,
            )}
          />
          {darkIcon && darkIcon !== defaultIcon && (
            <img
              src={darkIcon}
              alt=""
              className={cn(
                "hidden dark:block",
                "col-start-1 row-start-1 object-contain object-center",
                !hero && size !== "navigation" && "size-resource-brand-logo",
                hero
                  ? "size-10"
                  : size === "navigation"
                    ? "size-icon-md"
                    : undefined,
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
