import { lazy, Suspense } from "react";
import type { Library } from "./knowledge-model";
import type { KnowledgeEditorController } from "./knowledge-editor-controller";
import { resourceKey } from "./knowledge-transfer";
import { KnowledgeLibraryDialogShell } from "./knowledge-library-dialog-shell";
import { KnowledgeLibrarySettingsLoading } from "./knowledge-library-settings-loading";
import { useCopy } from "./use-copy";
const Content = lazy(() =>
  import("./knowledge-library-settings-content").then((module) => ({
    default: module.KnowledgeLibrarySettingsContent,
  })),
);
export function KnowledgeLibrarySettings({
  library,
  controllers,
  close,
}: {
  library: Library;
  controllers: Map<string, KnowledgeEditorController>;
  close: () => void;
}) {
  const t = useCopy();
  const finish = () => {
    if (!controllers.get(resourceKey(library.ref))?.getSnapshot().pending)
      close();
  };
  return (
    <KnowledgeLibraryDialogShell title={t("librarySettings")} close={finish}>
      <Suspense fallback={<KnowledgeLibrarySettingsLoading close={finish} />}>
        <Content library={library} controllers={controllers} close={finish} />
      </Suspense>
    </KnowledgeLibraryDialogShell>
  );
}
