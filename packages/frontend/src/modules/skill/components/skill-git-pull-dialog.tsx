import { GitPullRequestArrow } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import { PageAction } from "@/shared/ui/resource-page";

import {
  isVersionGreater,
  suggestNextPatchVersion,
} from "../model/skill-detail";

type SkillGitPullDialogProps = {
  currentVersion: string;
  error: boolean;
  onPull: (version: string, notes: string, onSuccess: () => void) => void;
  pending: boolean;
};

export function SkillGitPullDialog({
  currentVersion,
  error,
  onPull,
  pending,
}: SkillGitPullDialogProps) {
  const { t } = useTranslation("skill");
  const [open, setOpen] = useState(false);
  const [version, setVersion] = useState("");
  const [notes, setNotes] = useState("");
  const valid = isVersionGreater(version, currentVersion);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <PageAction
        intent="secondary"
        label={t("detail.update")}
        onClick={() => {
          setVersion(suggestNextPatchVersion(currentVersion));
          setNotes("");
          setOpen(true);
        }}
      >
        <GitPullRequestArrow />
      </PageAction>
      <DialogContent
        title={t("detail.pullUpdate")}
        description={t("detail.pullDescription")}
        size="sm"
      >
        <div className="grid gap-4 px-6 pb-6">
          <label className="grid gap-1.5">
            <span className="type-label font-medium">
              {t("detail.newVersion")}
            </span>
            <Input
              value={version}
              aria-invalid={!valid}
              onChange={(event) => setVersion(event.target.value)}
            />
          </label>
          <label className="grid gap-1.5">
            <span className="type-label font-medium">
              {t("detail.versionNotes")}
            </span>
            <Input
              value={notes}
              onChange={(event) => setNotes(event.target.value)}
            />
          </label>
          {error ? (
            <p role="alert" className="text-destructive type-caption">
              {t("detail.pullFailed")}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            variant="secondary"
            disabled={pending}
            onClick={() => setOpen(false)}
          >
            {t("common:cancel")}
          </Button>
          <Button
            disabled={!valid || pending}
            onClick={() => onPull(version, notes, () => setOpen(false))}
          >
            {pending ? t("detail.pulling") : t("detail.pullUpdate")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
