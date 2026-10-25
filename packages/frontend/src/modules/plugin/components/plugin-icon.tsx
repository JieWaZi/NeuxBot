import { Package } from "lucide-react";
import { cn } from "@/shared/lib/cn";
import type { PluginCatalogEntry } from "../model/plugin-view";

/** 只呈现包内已校验图标，缺失时使用统一包占位。 */
export function PluginIcon({
  pkg,
  hero = false,
}: {
  pkg?: PluginCatalogEntry["package"];
  hero?: boolean;
}) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "text-content-primary grid shrink-0 place-items-center",
        hero ? "[&_svg]:size-icon-xl size-14" : "[&_svg]:size-icon-lg size-10",
      )}
    >
      {pkg?.icon ? (
        <img
          src={pkg.icon}
          alt=""
          className={hero ? "size-10 object-contain" : "size-8 object-contain"}
        />
      ) : (
        <Package />
      )}
    </span>
  );
}
