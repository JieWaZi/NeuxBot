import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { localApiErrorMessage } from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { FieldLabel } from "@/shared/ui/field-label";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";
import { useWorkflowConfiguration } from "../model/use-workflow-configuration";

export function BasicInformationDialog({
  id,
  revisionId,
  title,
  description,
  open,
  onOpenChange,
}: {
  id: string;
  revisionId: string;
  title: string;
  description: string;
  open: boolean;
  onOpenChange: (value: boolean) => void;
}) {
  const { t } = useTranslation("workflow");
  const formId = useId();
  const [values, setValues] = useState({ title, description });
  const configure = useWorkflowConfiguration(id, revisionId);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={t("editBasicInformation")}
        description={t("basicInformationHelp")}
      >
        <form
          id={formId}
          className="contents"
          onSubmit={(event) => {
            event.preventDefault();
            void configure
              .save({ target: "meta", fields: values })
              .then((saved) => {
                if (saved) onOpenChange(false);
              });
          }}
        >
          <div className="grid gap-5 overflow-y-auto px-6 pb-5">
            <div className="grid gap-2">
              <FieldLabel htmlFor={`${formId}-title`} required>
                {t("titleLabel")}
              </FieldLabel>
              <Input
                id={`${formId}-title`}
                required
                placeholder={t("namePlaceholder")}
                aria-describedby={`${formId}-title-help`}
                value={values.title}
                onChange={(event) =>
                  setValues({ ...values, title: event.target.value })
                }
              />
              <p
                id={`${formId}-title-help`}
                className="text-muted-foreground type-ui"
              >
                {t("titleHelp")}
              </p>
            </div>
            <div className="grid gap-2">
              <FieldLabel htmlFor={`${formId}-description`}>
                {t("description")}
              </FieldLabel>
              <Textarea
                id={`${formId}-description`}
                placeholder={t("descriptionPlaceholder")}
                aria-describedby={`${formId}-description-help`}
                value={values.description}
                onChange={(event) =>
                  setValues({ ...values, description: event.target.value })
                }
              />
              <p
                id={`${formId}-description-help`}
                className="text-muted-foreground type-ui"
              >
                {t("descriptionHelp")}
              </p>
            </div>
            {configure.error ? (
              <p role="alert" className="text-destructive type-body">
                {localApiErrorMessage(configure.error, t("error"))}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="secondary"
              onClick={() => onOpenChange(false)}
            >
              {t("cancel")}
            </Button>
            <Button
              type="submit"
              form={formId}
              disabled={configure.pending || !values.title.trim()}
            >
              {t("save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
