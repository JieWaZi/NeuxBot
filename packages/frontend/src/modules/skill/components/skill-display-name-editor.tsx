import { Pencil } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";

import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import { PageAction } from "@/shared/ui/resource-page";

type SkillDisplayNameEditorProps = {
  name: string;
  onSave: (name: string, onSuccess?: () => void) => void;
  pending: boolean;
  error?: unknown;
};

export function SkillDisplayNameEditor({
  name,
  onSave,
  pending,
  error,
}: SkillDisplayNameEditorProps) {
  const { t } = useTranslation("skill");
  const [editing, setEditing] = useState(false);
  const [editedName, setEditedName] = useState(name);
  return (
    <Dialog
      open={editing}
      onOpenChange={(open) => {
        if (!pending) setEditing(open);
      }}
    >
      <PageAction
        intent="edit"
        label={t("detail.editDisplayName")}
        disabled={pending}
        onClick={() => {
          setEditedName(name);
          setEditing(true);
        }}
      >
        <Pencil />
      </PageAction>
      <DialogContent title={t("detail.editDisplayName")} size="sm">
        <div className="grid gap-2 px-6 pb-6">
          <label className="grid gap-1.5">
            <span className="type-label font-medium">
              {t("detail.displayName")}
            </span>
            <Input
              aria-label={t("detail.displayName")}
              placeholder={t("detail.displayName")}
              value={editedName}
              onChange={(event) => setEditedName(event.target.value)}
            />
          </label>
          <p className="text-content-secondary type-ui">
            {t("detail.displayNameHelp")}
          </p>
          {error ? (
            <p role="alert" className="text-destructive type-body">
              {localApiErrorMessage(error, t("detail.displayNameSaveFailed"))}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            variant="secondary"
            disabled={pending}
            onClick={() => setEditing(false)}
          >
            {t("detail.cancel")}
          </Button>
          <Button
            disabled={
              !editedName.trim() || editedName.trim() === name || pending
            }
            onClick={() => onSave(editedName.trim(), () => setEditing(false))}
          >
            {t("detail.saveDisplayName")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
