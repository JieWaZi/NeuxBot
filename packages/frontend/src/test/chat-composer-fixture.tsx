import type { PropsWithChildren } from "react";
import type { QueryClient } from "@tanstack/react-query";
import {
  getWakerRecentProjectQueryKey,
  listAiAccountsQueryKey,
  listRuntimesQueryKey,
  listWakerConnectorsQueryKey,
  listWakerPluginReferencesQueryKey,
  listWakerSkillsQueryKey,
  listWakersQueryKey,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import { nativeModelCatalogQueryKey } from "@/shared/api/runtime-queries";
import { ChatQueueProvider } from "@/modules/chat/runtime/chat-queue-store";
import { WorkbenchProvider } from "@/modules/chat/workbench/workbench-store";

export function seedChatComposerResources(
  client: QueryClient,
  id: string,
  name: string,
) {
  const model = {
    id: "gpt-5",
    name: "GPT-5",
    permissionLevels: ["default", "auto", "full-access"],
  };
  const path = { wakerId: id };
  client.setQueryData(listRuntimesQueryKey(), {
    resources: [
      {
        id: "codex",
        name: "Codex",
        enabled: true,
        installed: true,
        state: "ready",
        defaultModel: model.id,
        models: [model],
      },
    ],
  });
  client.setQueryData(
    nativeModelCatalogQueryKey({ runtimeId: "codex", accountId: "system" }),
    {
      runtimeId: "codex",
      accountId: "system",
      state: "fresh",
      defaultModel: model.id,
      models: [model],
    },
  );
  client.setQueryData(listAiAccountsQueryKey(), { resources: [] });
  client.setQueryData(
    listWakersQueryKey({ query: { pageNum: 1, pageSize: 100 } }),
    { resources: [{ id, name }] },
  );
  client.setQueryData(listWakerSkillsQueryKey({ path }), []);
  client.setQueryData(listWakerPluginReferencesQueryKey({ path }), []);
  client.setQueryData(listWakerConnectorsQueryKey({ path }), []);
  client.setQueryData(getWakerRecentProjectQueryKey({ path }), {});
}

export function ChatComposerProviders({ children }: PropsWithChildren) {
  return (
    <ChatQueueProvider>
      <WorkbenchProvider>{children}</WorkbenchProvider>
    </ChatQueueProvider>
  );
}
