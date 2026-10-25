import { lazy, Suspense, useState } from "react";
import type { ExecutionSelection } from "@/shared/hooks/use-execution-selection";
import {
  KnowledgeLibraryDialogShell,
  KnowledgeLibraryDialogLoading,
} from "./knowledge-library-dialog-shell";
import { useCopy } from "./use-copy";

const KnowledgeLibraryEditor = lazy(() =>
  import("./knowledge-library-editor").then((module) => ({
    default: module.KnowledgeLibraryEditor,
  })),
);
import {
  createKnowledgeEditorController,
  type KnowledgeEditorController,
} from "./knowledge-editor-controller";
import type { Library } from "./knowledge-model";

/** 新建仅提供入口，草稿、字段、校验与布局和编辑使用同一个 Editor。 */
export function CreateLibrary({
  opened,
  close,
  create,
  controller: retained,
}: {
  opened: boolean;
  close: () => void;
  create: (
    library: Library,
    execution: ExecutionSelection,
    requestId: string,
    controller: KnowledgeEditorController,
  ) => Promise<void>;
  controller?: KnowledgeEditorController;
}) {
  const t = useCopy();
  const [local, setLocal] = useState(createKnowledgeEditorController);
  const controller = retained ?? local;
  const finish = () => {
    if (controller.getSnapshot().pending) return;
    if (!retained && !controller.getSnapshot().command)
      setLocal(createKnowledgeEditorController());
    close();
  };
  return (
    <KnowledgeLibraryDialogShell
      opened={opened}
      title={t("create")}
      close={finish}
    >
      <Suspense fallback={<KnowledgeLibraryDialogLoading close={finish} />}>
        <KnowledgeLibraryEditor
          embedded
          key={controller.id}
          opened={opened}
          mode="create"
          controller={controller}
          close={finish}
          save={(command, current) =>
            create(
              command.draft.library,
              command.draft.execution,
              command.coreRequestId,
              current,
            )
          }
        />
      </Suspense>
    </KnowledgeLibraryDialogShell>
  );
}
