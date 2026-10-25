import { Folder, FolderOpen } from "lucide-react";
import type { ReactElement } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import type { GithubComJieWaZiNeuxBotServerApisProjectsProjectResource as ProjectResource } from "@/shared/api/generated/types.gen";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "./dropdown-menu";

/** 普通聊天与接入配置共用项目和本地目录选择菜单。 */
export function WorkspaceScopePicker({
  trigger,
  projects,
  projectId,
  ownerId,
  onPickDirectory,
  onSelectProject,
  onSelectManaged,
}: {
  trigger: ReactElement;
  projects: ProjectResource[];
  projectId?: string;
  ownerId?: string;
  onPickDirectory: () => void | Promise<void>;
  onSelectProject: (id: string) => void;
  onSelectManaged?: () => void;
}) {
  const { t } = useTranslation("chat");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        {onSelectManaged ? (
          <DropdownMenuItem onSelect={onSelectManaged}>
            <Folder className="size-icon-md" />
            {t("defaultWorkspace")}
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuItem onSelect={() => void onPickDirectory()}>
          <FolderOpen className="size-icon-md" />
          {t("chooseLocalDirectory")}
        </DropdownMenuItem>
        <DropdownMenuSub>
          <DropdownMenuSubTrigger
            value={projects.find((item) => item.id === projectId)?.name}
          >
            <Folder className="size-icon-md" />
            {t("projects")}
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="w-[320px]">
            <DropdownMenuLabel>{t("availableProjects")}</DropdownMenuLabel>
            <DropdownMenuRadioGroup value={projectId ?? ""}>
              {projects.length === 0 ? (
                <DropdownMenuItem disabled>{t("noProjects")}</DropdownMenuItem>
              ) : (
                projects.map((project) => (
                  <DropdownMenuRadioItem
                    key={project.id}
                    value={project.id ?? ""}
                    disabled={!project.available}
                    onSelect={() => {
                      if (project.id && project.available)
                        onSelectProject(project.id);
                    }}
                  >
                    <span className="grid min-w-0 flex-1">
                      <span className="truncate">{project.name}</span>
                      <span className="text-muted-foreground type-caption truncate">
                        {project.available
                          ? t("projectDirectoryCount", {
                              count: project.sources?.length ?? 0,
                            })
                          : t("projectUnavailable")}
                      </span>
                    </span>
                  </DropdownMenuRadioItem>
                ))
              )}
            </DropdownMenuRadioGroup>
            {ownerId ? (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem asChild>
                  <Link to={`/wakers/${ownerId}/projects`}>
                    {t("manageProjects")}
                  </Link>
                </DropdownMenuItem>
              </>
            ) : null}
          </DropdownMenuSubContent>
        </DropdownMenuSub>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
