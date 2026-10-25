import { useMutation, useQueryClient } from "@tanstack/react-query";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { LoaderCircle, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
  invalidateVisibleSkillQueries,
  uninstallSkillMutation,
} from "../api/skill-queries";
import { ApiError } from "@/shared/api/errors";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";

export type DeletableSkill = {
  affectedWakerCount: number;
  id: string;
  name: string;
  scope: "private" | "public";
};

type Props = {
  onDeleted: (skillIds: string[]) => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  skills: DeletableSkill[];
  wakerId: string;
};

export function SkillDeleteDialog({
  onDeleted,
  onOpenChange,
  open,
  skills,
  wakerId,
}: Props) {
  const { t } = useTranslation("skill");
  const queryClient = useQueryClient();
  const remove = useMutation(withLocalErrorMutation(uninstallSkillMutation()));
  const [remainingSkills, setRemainingSkills] = useState(skills);
  const [failedCount, setFailedCount] = useState(0);
  const [isDeleting, setIsDeleting] = useState(false);
  const batch = skills.length > 1;
  const publicSkills = remainingSkills.filter(
    (skill) => skill.scope === "public",
  );
  const publicAffectedWakerCount = Math.max(
    0,
    ...publicSkills.map((skill) => skill.affectedWakerCount),
  );

  const close = () => {
    if (isDeleting) return;
    remove.reset();
    onOpenChange(false);
  };

  const confirm = async () => {
    setIsDeleting(true);
    setFailedCount(0);
    const deletedIds: string[] = [];
    const failedSkills: DeletableSkill[] = [];

    for (const skill of remainingSkills) {
      try {
        await remove.mutateAsync({
          path: { wakerId, skillId: skill.id },
          query: wakerId
            ? undefined
            : {
                confirmed: true,
                expectedAffectedCount: skill.affectedWakerCount,
              },
        });
        deletedIds.push(skill.id);
      } catch {
        failedSkills.push(skill);
      }
    }

    await invalidateVisibleSkillQueries(queryClient);
    if (deletedIds.length > 0) onDeleted(deletedIds);

    setIsDeleting(false);
    if (failedSkills.length === 0) {
      onOpenChange(false);
      return;
    }
    setRemainingSkills(failedSkills);
    setFailedCount(failedSkills.length);
  };

  const singleError =
    !batch && failedCount > 0 && remove.error instanceof ApiError
      ? remove.error.message
      : null;

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) close();
      }}
    >
      <DialogContent
        size="sm"
        title={t(batch ? "delete.batchTitle" : "delete.title")}
        description={t("delete.description")}
      >
        <div className="px-6 pb-5">
          <div className="bg-destructive/8 text-destructive type-body flex gap-2 rounded-md p-3">
            <TriangleAlert className="size-icon-md mt-0.5 shrink-0" />
            <p>
              {batch
                ? publicSkills.length > 0
                  ? t("delete.batchPublicWarning", {
                      count: remainingSkills.length,
                      publicCount: publicSkills.length,
                      wakerCount: publicAffectedWakerCount,
                    })
                  : t("delete.batchPrivateWarning", {
                      count: remainingSkills.length,
                    })
                : remainingSkills[0]?.scope === "public"
                  ? t("delete.publicWarning", {
                      name: remainingSkills[0]?.name ?? "",
                      count: remainingSkills[0]?.affectedWakerCount ?? 0,
                    })
                  : t("delete.privateWarning", {
                      name: remainingSkills[0]?.name ?? "",
                    })}
            </p>
          </div>
          {failedCount > 0 ? (
            <p role="alert" className="text-destructive type-body mt-3">
              {singleError ?? t("delete.failed", { count: failedCount })}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="secondary"
            disabled={isDeleting}
            onClick={close}
          >
            {t("detail.cancel")}
          </Button>
          <Button
            type="button"
            variant="danger"
            disabled={isDeleting || remainingSkills.length === 0}
            onClick={() => void confirm()}
          >
            {isDeleting ? <LoaderCircle className="animate-spin" /> : null}
            {t(
              failedCount > 0
                ? "delete.retry"
                : batch
                  ? "delete.confirmBatch"
                  : "delete.confirm",
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
