import { Card } from "@/shared/ui/card";
import { Eye, MoreHorizontal, Pencil, Trash2 } from "lucide-react";
import type { KeyboardEvent } from "react";

import type { RolePreset } from "../model/types";
import { cn } from "@/shared/lib/cn";
import { Avatar } from "@/shared/ui/avatar";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";

type Props = {
  preset: RolePreset;
  selected: boolean;
  onSelect: () => void;
  onView: () => void;
  onEdit: () => void;
  onDelete: () => void;
};

export function RolePresetCard({
  onDelete,
  onEdit,
  onSelect,
  onView,
  preset,
  selected,
}: Props) {
  const selectFromKeyboard = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onSelect();
    }
  };

  return (
    <Card
      asChild
      variant="resource"
      data-state={selected ? "selected" : undefined}
    >
      <div
        role="radio"
        tabIndex={0}
        aria-checked={selected}
        onClick={onSelect}
        onKeyDown={selectFromKeyboard}
        className={cn(
          "hover:bg-muted focus-visible:ring-status-success-content grid min-h-20 cursor-pointer grid-cols-[40px_minmax(0,1fr)_auto] items-center gap-3 px-4 py-3 transition-colors outline-none focus-visible:ring-2",
        )}
      >
        <Avatar
          interactive={false}
          name={preset.roleName ?? "角色"}
          src={preset.defaultAvatar}
          size="md"
        />
        <div className="min-w-0">
          <strong className="type-body block truncate font-medium">
            {preset.roleName}
          </strong>
          <p className="text-muted-foreground type-caption mt-1 line-clamp-2">
            {preset.description}
          </p>
        </div>
        <div className="flex items-center">
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={(event) => {
              event.stopPropagation();
              onView();
            }}
          >
            查看详情
          </Button>
          {preset.source === "user" ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  aria-label="角色预设更多操作"
                  size="icon-sm"
                  variant="ghost"
                  onClick={(event) => event.stopPropagation()}
                >
                  <MoreHorizontal className="size-icon-md" strokeWidth={1.5} />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent
                align="end"
                onClick={(event) => event.stopPropagation()}
              >
                <DropdownMenuItem onSelect={onView}>
                  <Eye />
                  查看详情
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={onEdit}>
                  <Pencil />
                  编辑预设
                </DropdownMenuItem>
                <DropdownMenuItem
                  className="text-destructive data-[highlighted]:text-destructive"
                  onSelect={onDelete}
                >
                  <Trash2 />
                  删除预设
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </div>
      </div>
    </Card>
  );
}
