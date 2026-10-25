import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import type {
  GetLibraryEmbeddingProfileResponse,
  GetLibraryExecutionDefaultsResponse,
} from "@/shared/api/generated/types.gen";
import {
  libraryEmbeddingProfileOptions,
  readEmbeddingProfile,
} from "./knowledge-embedding-profile";
import { KnowledgeLibraryEditor } from "./knowledge-library-editor";
import { KnowledgeLibraryActivity } from "./knowledge-library-activity";
import {
  createKnowledgeEditorController,
  type KnowledgeEditorController,
} from "./knowledge-editor-controller";
import {
  libraryExecutionDefaultsOptions,
  readExecutionDefaults,
  buildExecutionDefaults,
} from "./knowledge-execution-defaults";
import { buildUpdateLibraryBody } from "./knowledge-library-write-body";
import { knowledgeKeys, sourcesOptions } from "./knowledge-queries";
import { resourceKey } from "./knowledge-transfer";
import type { Library } from "./knowledge-model";
import { KnowledgeLibrarySettingsLoading } from "./knowledge-library-settings-loading";

function LocalLibrarySettingsContent({
  library,
  controllers,
  close,
}: {
  library: Library;
  controllers: Map<string, KnowledgeEditorController>;
  close: () => void;
}) {
  const defaults = useQuery(libraryExecutionDefaultsOptions(library.ref));
  const sources = useQuery(sourcesOptions(library.ref, 0, true));
  const embedding = useQuery(libraryEmbeddingProfileOptions(library.ref));
  const retained = controllers.get(resourceKey(library.ref));
  if (
    (!defaults.data || !sources.data || !embedding.data) &&
    !retained?.getSnapshot().command
  )
    return <KnowledgeLibrarySettingsLoading close={close} />;
  return (
    <LoadedLibrarySettings
      key={library.id}
      library={{
        ...library,
        sourceTotal: sources.data?.total ?? library.sourceTotal,
      }}
      defaults={defaults.data}
      embedding={embedding.data}
      controllers={controllers}
      close={close}
    />
  );
}

function LoadedLibrarySettings({
  library,
  defaults,
  embedding,
  controllers,
  close,
}: {
  library: Library;
  defaults: GetLibraryExecutionDefaultsResponse | undefined;
  embedding: GetLibraryEmbeddingProfileResponse | undefined;
  controllers: Map<string, KnowledgeEditorController>;
  close: () => void;
}) {
  const client = useQueryClient();
  const key = resourceKey(library.ref);
  const [controller] = useState(() => {
    const retained = controllers.get(key);
    if (retained?.getSnapshot().command) return retained;
    const created = createKnowledgeEditorController(
      { ...library, revision: embedding?.libraryVersion ?? library.revision },
      readExecutionDefaults(defaults ?? {}),
      defaults?.version ?? 0,
      {
        ...readEmbeddingProfile(embedding ?? {}),
        model: embedding?.model || library.embeddingModelId || "",
      },
      embedding?.version ?? 0,
    );
    controllers.set(key, created);
    return created;
  });
  const finish = () => {
    if (!controller.getSnapshot().command) controllers.delete(key);
    close();
  };
  return (
    <KnowledgeLibraryEditor
      embedded
      opened
      mode="edit"
      controller={controller}
      close={finish}
      activity={<KnowledgeLibraryActivity library={library} />}
      save={async (command) => {
        const desired = command.draft;
        let saved = controller.getSnapshot().saved;
        const desiredBody = buildUpdateLibraryBody("", 1, desired.library);
        const savedBody = buildUpdateLibraryBody("", 1, saved.library);
        if (JSON.stringify(desiredBody) !== JSON.stringify(savedBody)) {
          const response = await knowledgeAdapter(library.ref).updateLibrary({
            path: { libraryId: library.id },
            body: buildUpdateLibraryBody(
              command.coreRequestId,
              command.libraryVersion,
              desired.library,
            ),
            throwOnError: true,
          });
          controller.acknowledgeLibrary({
            ...desired.library,
            revision: response.data.version ?? command.libraryVersion,
          });
        }
        saved = controller.getSnapshot().saved;
        const embeddingChanged =
          desired.embedding.baseUrl !== saved.embedding.baseUrl ||
          desired.embedding.model !== saved.embedding.model ||
          Boolean(desired.embedding.apiKey);
        if (
          embeddingChanged &&
          controller.getSnapshot().acknowledgedEmbeddingRequestId !==
            command.embeddingRequestId
        ) {
          const response = await knowledgeAdapter(
            library.ref,
          ).updateLibraryEmbeddingProfile({
            path: { libraryId: library.id },
            body: {
              requestId: command.embeddingRequestId,
              expectedVersion: command.embeddingVersion,
              expectedLibraryVersion: saved.library.revision,
              baseUrl: desired.embedding.baseUrl,
              model: desired.embedding.model,
              apiKey: desired.embedding.apiKey || undefined,
            },
            throwOnError: true,
          });
          controller.acknowledgeEmbedding(
            readEmbeddingProfile(response.data),
            response.data.version ?? command.embeddingVersion,
            response.data.libraryVersion ?? saved.library.revision,
            command.embeddingRequestId,
          );
        }
        saved = controller.getSnapshot().saved;
        if (
          JSON.stringify(buildExecutionDefaults(desired.execution)) !==
          JSON.stringify(buildExecutionDefaults(saved.execution))
        ) {
          const response = await knowledgeAdapter(
            library.ref,
          ).updateLibraryExecutionDefaults({
            path: { libraryId: library.id },
            body: {
              ...buildExecutionDefaults(desired.execution),
              requestId: command.executionRequestId,
              expectedVersion: command.executionVersion,
            },
            throwOnError: true,
          });
          controller.acknowledgeExecution(
            readExecutionDefaults(response.data),
            response.data.version ?? command.executionVersion,
          );
        }
        await Promise.all([
          client.invalidateQueries({
            queryKey: knowledgeKeys.resource(library.ref),
          }),
          client.invalidateQueries({ queryKey: knowledgeKeys.libraryLists }),
        ]);
      }}
    />
  );
}

/** 远端核心设置不读取本机执行偏好或模型密钥。 */
export function KnowledgeLibrarySettingsContent(props: {
  library: Library;
  controllers: Map<string, KnowledgeEditorController>;
  close: () => void;
}) {
  return knowledgeAdapter(props.library.ref).localConfiguration ? (
    <LocalLibrarySettingsContent {...props} />
  ) : (
    <RemoteLibrarySettingsContent {...props} />
  );
}

function RemoteLibrarySettingsContent({
  library,
  controllers,
  close,
}: {
  library: Library;
  controllers: Map<string, KnowledgeEditorController>;
  close: () => void;
}) {
  const client = useQueryClient();
  const sources = useQuery(sourcesOptions(library.ref, 0, true));
  const [controller] = useState(() => {
    const key = resourceKey(library.ref);
    const retained = controllers.get(key);
    if (retained) return retained;
    const created = createKnowledgeEditorController(library);
    controllers.set(key, created);
    return created;
  });
  return (
    <KnowledgeLibraryEditor
      embedded
      opened
      mode="edit"
      controller={controller}
      close={() => {
        if (!controller.getSnapshot().command)
          controllers.delete(resourceKey(library.ref));
        close();
      }}
      activity={<KnowledgeLibraryActivity library={library} />}
      sourceTotal={sources.data?.total}
      save={async (command) => {
        const desired = command.draft.library;
        const response = await knowledgeAdapter(library.ref).updateLibrary({
          path: { libraryId: library.id },
          body: buildUpdateLibraryBody(
            command.coreRequestId,
            command.libraryVersion,
            desired,
          ),
          throwOnError: true,
        });
        controller.acknowledgeLibrary({
          ...desired,
          revision: response.data.version ?? command.libraryVersion,
        });
        await Promise.all([
          client.invalidateQueries({
            queryKey: knowledgeKeys.resource(library.ref),
          }),
          client.invalidateQueries({ queryKey: knowledgeKeys.libraryLists }),
        ]);
      }}
    />
  );
}
