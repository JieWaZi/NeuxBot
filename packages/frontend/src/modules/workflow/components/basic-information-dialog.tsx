import { useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import {
  updateWorkflowCategoriesMutation,
  getWorkflowQueryKey,
  listWorkflowsQueryKey,
  listResourceCategoriesQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { ResourceCategoryField } from "@/shared/ui/resource-category-field";
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
  categories = [],
  open,
  onOpenChange,
}: {
  id: string;
  revisionId: string;
  title: string;
  description: string;
  categories?: string[];
  open: boolean;
  onOpenChange: (value: boolean) => void;
}) {
  const { t } = useTranslation("workflow");
  const formId = useId();
  const [values, setValues] = useState({ title, description });
  const [selectedCategories, setSelectedCategories] = useState(categories);
  const client = useQueryClient();
  const updateCategories = useMutation({
    ...withLocalErrorMutation(updateWorkflowCategoriesMutation()),
    onSuccess: () => {
      void client.invalidateQueries({
        queryKey: getWorkflowQueryKey({ path: { id } }),
      });
      void client.invalidateQueries({ queryKey: listWorkflowsQueryKey() });
      void client.invalidateQueries({
        queryKey: listResourceCategoriesQueryKey({
          query: { type: "workflow" },
        }),
      });
    },
  });
  const configure = useWorkflowConfiguration(id, revisionId);
  const pending = configure.pending || updateCategories.isPending;
  const save = async () => {
    if (JSON.stringify(selectedCategories) !== JSON.stringify(categories)) {
      try {
        await updateCategories.mutateAsync({
          path: { id },
          body: { categories: selectedCategories },
        });
      } catch {
        return;
      }
    }
    if (values.title !== title || values.description !== description) {
      if (!(await configure.save({ target: "meta", fields: values }))) return;
    }
    onOpenChange(false);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!pending) onOpenChange(value);
      }}
    >
      <DialogContent
        title={t("editBasicInformation")}
        description={t("basicInformationHelp")}
      >
        <form
          id={formId}
          className="contents"
          onSubmit={(event) => {
            event.preventDefault();
            void save();
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
            <ResourceCategoryField
              type="workflow"
              value={selectedCategories}
              onValueChange={setSelectedCategories}
              disabled={pending}
            />
            {configure.error || updateCategories.error ? (
              <p role="alert" className="text-destructive type-body">
                {localApiErrorMessage(
                  configure.error ?? updateCategories.error,
                  t("error"),
                )}
              </p>
            ) : null}
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="secondary"
              disabled={pending}
              onClick={() => onOpenChange(false)}
            >
              {t("cancel")}
            </Button>
            <Button
              type="submit"
              form={formId}
              disabled={pending || !values.title.trim()}
            >
              {t("save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
