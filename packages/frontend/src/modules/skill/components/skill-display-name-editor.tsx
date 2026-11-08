import { Pencil } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";

import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { ResourceCategoryField } from "@/shared/ui/resource-category-field";
import { Input } from "@/shared/ui/input";
import { PageAction } from "@/shared/ui/resource-page";

type SkillDisplayNameEditorProps = {
  name: string;
  categories: string[];
  onSave: (
    metadata: { name: string; categories: string[] },
    onSuccess?: () => void,
  ) => void;
  pending: boolean;
  error?: unknown;
};

export function SkillDisplayNameEditor({
  name,
  categories,
  onSave,
  pending,
  error,
}: SkillDisplayNameEditorProps) {
  const { t } = useTranslation("skill");
  const [editing, setEditing] = useState(false);
  const [editedName, setEditedName] = useState(name);
  const [editedCategories, setEditedCategories] = useState(categories);
  return (
    <Dialog
      open={editing}
      onOpenChange={(open) => {
        if (!pending) setEditing(open);
      }}
    >
      <PageAction
        appearance="catalog"
        intent="edit"
        label={t("detail.edit")}
        disabled={pending}
        onClick={() => {
          setEditedName(name);
          setEditedCategories(categories);
          setEditing(true);
        }}
      >
        <Pencil />
      </PageAction>
      <DialogContent title={t("detail.edit")} size="sm">
        <div className="grid gap-2 px-6 pb-6">
          <label className="grid gap-1.5">
            <span className="type-label font-medium">
              {t("detail.displayName")}
            </span>
            <Input
              appearance="catalog"
              disabled={pending}
              aria-label={t("detail.displayName")}
              placeholder={t("detail.displayName")}
              value={editedName}
              onChange={(event) => setEditedName(event.target.value)}
            />
          </label>
          <p className="text-content-secondary type-ui">
            {t("detail.displayNameHelp")}
          </p>
          <ResourceCategoryField
            type="skill"
            value={editedCategories}
            onValueChange={setEditedCategories}
            disabled={pending}
          />
          {error ? (
            <p role="alert" className="text-destructive type-body">
              {localApiErrorMessage(error, t("detail.metadataSaveFailed"))}
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
              !editedName.trim() ||
              (editedName.trim() === name &&
                JSON.stringify(editedCategories) ===
                  JSON.stringify(categories)) ||
              pending
            }
            onClick={() =>
              onSave(
                { name: editedName.trim(), categories: editedCategories },
                () => setEditing(false),
              )
            }
          >
            {t("detail.saveMetadata")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
