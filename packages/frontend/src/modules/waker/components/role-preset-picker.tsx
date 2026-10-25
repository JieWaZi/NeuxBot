import { useQuery } from "@tanstack/react-query";
import { Plus, UsersRound } from "lucide-react";
import { useState } from "react";

import { listRolePresetsOptions } from "../api/waker-queries";
import type { RolePreset } from "../model/types";
import { RolePresetCard } from "./role-preset-card";
import { RolePresetDeleteDialog } from "./role-preset-delete-dialog";
import { RolePresetDetailSheet } from "./role-preset-detail-sheet";
import { RolePresetFormDialog } from "./role-preset-form-dialog";
import { Button } from "@/shared/ui/button";

type Props = {
  onClearSelection: () => void;
  selectedId: string;
  onSelect: (preset: RolePreset) => void;
};

export function RolePresetPicker({
  onClearSelection,
  onSelect,
  selectedId,
}: Props) {
  const query = useQuery(
    listRolePresetsOptions({ query: { pageNum: 1, pageSize: 100 } }),
  );
  const [creating, setCreating] = useState(false);
  const [viewing, setViewing] = useState<RolePreset>();
  const [editing, setEditing] = useState<RolePreset>();
  const [deleting, setDeleting] = useState<RolePreset>();
  const presets = query.data?.resources ?? [];

  if (query.isPending) {
    return (
      <div className="border-border text-muted-foreground type-body grid min-h-40 place-items-center rounded-lg border">
        正在载入角色预设…
      </div>
    );
  }

  if (query.isError) {
    return (
      <div className="border-border grid min-h-40 place-items-center rounded-lg border text-center">
        <div>
          <p className="type-body">暂时无法载入角色预设</p>
          <Button
            className="mt-3"
            size="sm"
            variant="secondary"
            onClick={() => void query.refetch()}
          >
            重新载入
          </Button>
        </div>
      </div>
    );
  }

  return (
    <>
      <div className="mb-3 flex items-center justify-between gap-4">
        <div>
          <h3 className="type-label font-semibold">选择一个角色预设</h3>
          <p className="text-muted-foreground type-caption mt-1">
            创建时复制档案，之后由伙伴独立维护。
          </p>
        </div>
        <Button
          type="button"
          size="sm"
          variant="secondary"
          onClick={() => setCreating(true)}
        >
          <Plus />
          自定义角色
        </Button>
      </div>

      {presets.length > 0 ? (
        <div
          role="radiogroup"
          aria-label="选择角色预设"
          className="grid grid-cols-2 gap-2"
        >
          {presets.map((preset) => (
            <RolePresetCard
              key={preset.id}
              preset={preset}
              selected={selectedId === preset.id}
              onSelect={() => onSelect(preset)}
              onView={() => setViewing(preset)}
              onEdit={() => setEditing(preset)}
              onDelete={() => setDeleting(preset)}
            />
          ))}
        </div>
      ) : (
        <div className="border-border grid min-h-40 place-items-center rounded-lg border text-center">
          <div>
            <UsersRound
              className="text-muted-foreground size-icon-lg mx-auto"
              strokeWidth={1.5}
            />
            <p className="type-body mt-3 font-medium">还没有角色预设</p>
            <p className="text-muted-foreground type-caption mt-1">
              请点击右上角“自定义角色”新建，再继续创建伙伴。
            </p>
          </div>
        </div>
      )}

      <RolePresetDetailSheet
        open={Boolean(viewing)}
        preset={viewing}
        onOpenChange={(open) => {
          if (!open) setViewing(undefined);
        }}
      />
      <RolePresetFormDialog
        open={creating}
        onOpenChange={setCreating}
        onSaved={(preset) => {
          if (preset) onSelect(preset);
        }}
      />
      <RolePresetFormDialog
        open={Boolean(editing)}
        preset={editing}
        onOpenChange={(open) => {
          if (!open) setEditing(undefined);
        }}
        onSaved={() => {
          setEditing(undefined);
        }}
      />
      <RolePresetDeleteDialog
        open={Boolean(deleting)}
        preset={deleting}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setDeleting(undefined);
        }}
        onDeleted={() => {
          if (deleting?.id === selectedId) {
            onClearSelection();
          }
          setDeleting(undefined);
        }}
      />
    </>
  );
}
