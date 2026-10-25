import { useMutation } from "@tanstack/react-query";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { uploadWorkflowFile } from "@/shared/api/generated/sdk.gen";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Input } from "@/shared/ui/input";

export function SchemaFileField({
  id,
  workflowId,
  value,
  path,
  description,
  onChange,
  onValidityChange,
}: {
  id: string;
  workflowId?: string;
  value: unknown;
  path: string;
  description?: string;
  onChange: (value: string) => void;
  onValidityChange?: (path: string, valid: boolean) => void;
}) {
  const { t } = useTranslation("workflow");
  const upload = useMutation({
    mutationFn: async (file: File) => {
      const result = await uploadWorkflowFile(
        withLocalErrorSurface({
          path: { id: workflowId ?? "" },
          body: { file },
          throwOnError: true,
        }),
      );
      if (result.data.reference) onChange(result.data.reference);
      return result.data;
    },
  });
  useEffect(() => {
    onValidityChange?.(`${path}/$upload`, !upload.isPending);
  }, [onValidityChange, path, upload.isPending]);
  useEffect(
    () => () => onValidityChange?.(`${path}/$upload`, true),
    [onValidityChange, path],
  );
  return (
    <div className="grid min-w-0 gap-2">
      <Input
        id={id}
        type="file"
        disabled={!workflowId || upload.isPending}
        aria-describedby={description}
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (file) {
            onValidityChange?.(`${path}/$upload`, false);
            upload.mutate(file);
          }
        }}
      />
      <p
        className="text-content-secondary type-caption break-all"
        role="status"
      >
        {upload.isPending
          ? t("uploadingFile")
          : typeof value === "string" && value
            ? t("selectedFile", { name: value.split("/").at(-1) })
            : t("fileInputHelp")}
      </p>
      {upload.error ? (
        <p role="alert" className="text-destructive type-body">
          {localApiErrorMessage(upload.error, t("fileUploadError"))}
        </p>
      ) : null}
    </div>
  );
}
