import { FileText } from "lucide-react";

import type { Expertise, RolePreset } from "../model/types";
import { Avatar } from "@/shared/ui/avatar";
import { Sheet, SheetContent } from "@/shared/ui/sheet";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  preset?: RolePreset;
};

export function RolePresetDetailSheet({ onOpenChange, open, preset }: Props) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent title="角色预设详情">
        {preset ? (
          <div className="min-h-0 flex-1 overflow-y-auto p-5">
            <div className="grid grid-cols-[56px_minmax(0,1fr)] items-center gap-4">
              <Avatar
                name={preset.roleName ?? "角色"}
                src={preset.defaultAvatar}
                size="lg"
              />
              <div className="min-w-0">
                <h2 className="type-title truncate font-semibold">
                  {preset.roleName}
                </h2>
                <p className="text-muted-foreground type-body mt-1">
                  {preset.description}
                </p>
              </div>
            </div>

            <ExpertiseDetailList items={preset.expertise ?? []} />
            <DetailList title="工作风格" items={preset.workStyle ?? []} />

            <section className="border-border mt-5 border-t pt-5">
              <h3 className="type-label font-semibold">工作方式</h3>
              <div className="mt-2 grid gap-2">
                {(preset.workMethods ?? []).length > 0 ? (
                  preset.workMethods?.map((method) => (
                    <div key={method.name} className="bg-muted rounded-md p-3">
                      <strong className="type-body font-medium">
                        {method.name}
                      </strong>
                      <p className="text-muted-foreground type-caption mt-1">
                        {(method.steps ?? []).join(" → ")}
                      </p>
                    </div>
                  ))
                ) : (
                  <p className="text-muted-foreground type-body">
                    还没有工作方式摘要。
                  </p>
                )}
              </div>
            </section>

            <section className="border-border mt-5 border-t pt-5">
              <h3 className="type-label font-semibold">初始档案</h3>
              <div className="mt-2 grid gap-2">
                {["IDENTITY.md", "PERSONA.md", "PLAYBOOK.md"].map(
                  (fileName) => (
                    <div
                      key={fileName}
                      className="bg-muted min-h-row-default type-body flex items-center gap-2 rounded-md px-3"
                    >
                      <FileText
                        className="text-muted-foreground size-icon-sm"
                        strokeWidth={1.5}
                      />
                      {fileName}
                    </div>
                  ),
                )}
              </div>
            </section>

            <section className="border-border mt-5 grid grid-cols-2 gap-3 border-t pt-5">
              <div className="bg-muted rounded-md p-3">
                <h3 className="type-body font-medium">技能</h3>
                <p className="text-muted-foreground type-caption mt-1">
                  后续开放
                </p>
              </div>
              <div className="bg-muted rounded-md p-3">
                <h3 className="type-body font-medium">MCP 连接器</h3>
                <p className="text-muted-foreground type-caption mt-1">
                  后续开放
                </p>
              </div>
            </section>
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}

function ExpertiseDetailList({ items }: { items: Expertise[] }) {
  return (
    <section className="border-border mt-5 border-t pt-5">
      <h3 className="type-label font-semibold">擅长领域</h3>
      {items.length > 0 ? (
        <div className="mt-2 grid gap-2">
          {items.map((item, index) => (
            <div
              key={`${item.title ?? "expertise"}-${index}`}
              className="bg-muted rounded-md p-3"
            >
              <strong className="type-body font-medium">{item.title}</strong>
              {item.description ? (
                <p className="text-muted-foreground type-caption mt-1">
                  {item.description}
                </p>
              ) : null}
            </div>
          ))}
        </div>
      ) : (
        <p className="text-muted-foreground type-body mt-2">还没有摘要。</p>
      )}
    </section>
  );
}

function DetailList({ items, title }: { items: string[]; title: string }) {
  return (
    <section className="border-border mt-5 border-t pt-5">
      <h3 className="type-label font-semibold">{title}</h3>
      {items.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {items.map((item) => (
            <span
              key={item}
              className="bg-muted text-muted-foreground type-caption rounded-full px-2 py-1"
            >
              {item}
            </span>
          ))}
        </div>
      ) : (
        <p className="text-muted-foreground type-body mt-2">还没有摘要。</p>
      )}
    </section>
  );
}
