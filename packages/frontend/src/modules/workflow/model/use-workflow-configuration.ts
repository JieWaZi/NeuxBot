import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import { editWorkflowConfigurationMutation } from "@/shared/api/generated/@tanstack/react-query.gen";
import { withLocalErrorMutation } from "@/shared/api/local-error-surface";
import { refreshWorkflow } from "../api/workflow-queries";

/** 手工表单与 Agent 共享源码、资源校验和 CAS 版本。 */
export function useWorkflowConfiguration(id: string, revisionId: string) {
  const client = useQueryClient();
  const mutation = useMutation(
    withLocalErrorMutation(editWorkflowConfigurationMutation()),
  );
  const request = useRef<{ signature: string; id: string } | null>(null);
  const save = async (edit: {
    target: string;
    id?: string;
    fields: Record<string, unknown>;
    prompt?: string;
  }) => {
    const signature = JSON.stringify([revisionId, edit]);
    if (request.current?.signature !== signature)
      request.current = { signature, id: crypto.randomUUID() };
    try {
      await mutation.mutateAsync({
        path: { id },
        body: {
          requestId: request.current.id,
          expectedRevisionId: revisionId,
          edit,
        },
      });
      await refreshWorkflow(client, id);
      return true;
    } catch {
      return false;
    }
  };
  return { save, pending: mutation.isPending, error: mutation.error };
}
