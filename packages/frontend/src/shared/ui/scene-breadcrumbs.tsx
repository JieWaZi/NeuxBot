import { ChevronRight } from "lucide-react";
import { Fragment } from "react";
import { Link } from "react-router-dom";

import { useRouteSceneActive } from "@/shared/workspace/app-scene-workspace";
import { Button } from "./button";
import { SceneChromeLayout, SceneChromePortal } from "./scene-chrome-portal";

export type SceneBreadcrumbItem = {
  readonly label: string;
  readonly href?: string;
  readonly state?: unknown;
};

/** 页面只贡献已读取的展示路径；后台保活结果不能写入当前顶栏。 */
export function SceneBreadcrumbs({
  ariaLabel,
  items,
}: {
  ariaLabel: string;
  items: readonly SceneBreadcrumbItem[];
}) {
  const active = useRouteSceneActive();
  if (!active || !items.length) return null;
  return (
    <SceneChromePortal>
      <SceneChromeLayout>
        <nav
          aria-label={ariaLabel}
          data-desktop-no-drag-region
          className="min-w-0 overflow-x-auto"
        >
          <ol className="type-body flex min-w-0 items-center gap-1 whitespace-nowrap">
            {items.map((item, index) => (
              <Fragment key={item.href ?? "current"}>
                {index > 0 ? (
                  <li aria-hidden="true">
                    <ChevronRight className="text-content-tertiary size-icon-sm shrink-0" />
                  </li>
                ) : null}
                <li className="min-w-0">
                  {index < items.length - 1 && item.href ? (
                    <Button asChild variant="ghost" size="sm">
                      <Link
                        to={item.href}
                        state={item.state}
                        title={item.label}
                      >
                        <span className="max-w-48 truncate">{item.label}</span>
                      </Link>
                    </Button>
                  ) : (
                    <span
                      aria-current={
                        index === items.length - 1 ? "page" : undefined
                      }
                      title={item.label}
                      className="text-content-primary block max-w-64 truncate px-2"
                    >
                      {item.label}
                    </span>
                  )}
                </li>
              </Fragment>
            ))}
          </ol>
        </nav>
      </SceneChromeLayout>
    </SceneChromePortal>
  );
}
