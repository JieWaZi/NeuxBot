import { useMutation } from "@tanstack/react-query";
import { CheckCircle2 } from "lucide-react";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import type { ResourceRef } from "./knowledge-transfer";
import { Input } from "@/shared/ui/input";
import { Button } from "@/shared/ui/button";
import { Field } from "./knowledge-controls";
import type { KnowledgeEmbeddingDraft } from "./knowledge-embedding-profile";
import { useCopy } from "./use-extended-copy";

/** 只测试本库草稿或本库已存密钥；与记忆服务配置没有状态或 API 关联。 */
export function KnowledgeEmbeddingFields({
  value,
  onChange,
  libraryId,
  resource,
  disabled,
  modelDisabled,
  required = false,
}: {
  value: KnowledgeEmbeddingDraft;
  onChange: (value: KnowledgeEmbeddingDraft) => void;
  libraryId?: string;
  resource?: ResourceRef;
  disabled?: boolean;
  modelDisabled?: boolean;
  required?: boolean;
}) {
  const t = useCopy();
  const test = useMutation({
    mutationFn: async () =>
      (
        await knowledgeAdapter(
          resource ?? {
            backend: "local",
            connectionId: "local",
            knowledgeBaseId: libraryId ?? "",
          },
        ).testLibraryEmbedding({
          body: {
            libraryId,
            baseUrl: value.baseUrl.trim(),
            model: value.model.trim(),
            apiKey: value.apiKey || undefined,
          },
          throwOnError: true,
        })
      ).data,
    retry: false,
  });
  const update = (patch: Partial<KnowledgeEmbeddingDraft>) => {
    test.reset();
    onChange({ ...value, ...patch });
  };
  return (
    <section className="grid min-w-0 gap-4">
      <Field label={t("embeddingBaseUrl")} hint={t("embeddingBaseUrlHelp")}>
        <Input
          required={required}
          type="url"
          aria-label={t("embeddingBaseUrl")}
          placeholder="https://api.openai.com/v1"
          disabled={disabled}
          value={value.baseUrl}
          onChange={(event) => update({ baseUrl: event.target.value })}
        />
      </Field>
      <Field label={t("embeddingModel")} hint={t("embeddingModelHelp")}>
        <Input
          required={required}
          aria-label={t("embeddingModel")}
          placeholder="text-embedding-3-small"
          disabled={disabled || modelDisabled}
          value={value.model}
          onChange={(event) => update({ model: event.target.value })}
        />
      </Field>
      <Field label={t("embeddingApiKey")} hint={t("embeddingApiKeyHelp")}>
        <Input
          required={required && !value.hasApiKey}
          type="password"
          autoComplete="new-password"
          aria-label={t("embeddingApiKey")}
          placeholder={t(
            value.hasApiKey
              ? "embeddingApiKeyKeep"
              : "embeddingApiKeyPlaceholder",
          )}
          disabled={disabled}
          value={value.apiKey}
          onChange={(event) => update({ apiKey: event.target.value })}
        />
      </Field>
      <div>
        <Button
          type="button"
          size="sm"
          variant="secondary"
          disabled={
            disabled ||
            test.isPending ||
            !value.baseUrl.trim() ||
            !value.model.trim() ||
            !(value.apiKey || value.hasApiKey)
          }
          onClick={() => test.mutate()}
        >
          {test.isSuccess && <CheckCircle2 className="text-success" />}
          {t(
            test.isPending
              ? "embeddingTesting"
              : test.isSuccess
                ? "embeddingTestSucceeded"
                : "embeddingTestConnection",
          )}
        </Button>
      </div>
      {test.isSuccess && (
        <p role="status" className="type-caption text-success">
          {t("embeddingDimensions", { count: test.data.dimensions ?? 0 })}
        </p>
      )}
    </section>
  );
}
