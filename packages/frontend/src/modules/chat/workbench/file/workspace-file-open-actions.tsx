import { useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import type { WorkspaceFileOpenTarget } from "@neuxbot/platform-contracts/workspace-file-editors";
import { ChevronDown, FolderOpen } from "lucide-react";
import { useTranslation } from "react-i18next";

import cursorDarkLogo from "@/shared/assets/editor-logos/cursor-dark.svg?no-inline";
import cursorLogo from "@/shared/assets/editor-logos/cursor.svg?no-inline";
import fileExplorerLogo from "@/shared/assets/editor-logos/file-explorer.svg?no-inline";
import finderLogo from "@/shared/assets/editor-logos/finder.svg?no-inline";
import vscodeLogo from "@/shared/assets/editor-logos/vscode.svg?no-inline";
import vscodiumLogo from "@/shared/assets/editor-logos/vscodium.svg?no-inline";
import zedDarkLogo from "@/shared/assets/editor-logos/zed-dark.svg?no-inline";
import zedLogo from "@/shared/assets/editor-logos/zed.svg?no-inline";
import { installationPlatform } from "@/shared/lib/cli-installation";
import {
  listWorkspaceFileOpenTargets,
  openWorkspaceFileTarget,
} from "@/shared/platform";
import { useTheme } from "@/shared/theme";
import { Button } from "@/shared/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { toast } from "@/shared/ui/sonner";
type OpenTargetDetails = { name: string; logo?: string; darkLogo?: string };

const editors: Record<
  Exclude<WorkspaceFileOpenTarget, "file-manager">,
  OpenTargetDetails
> = {
  vscode: { name: "VS Code", logo: vscodeLogo },
  "vscode-insiders": { name: "VS Code Insiders", logo: vscodeLogo },
  vscodium: { name: "VSCodium", logo: vscodiumLogo },
  cursor: { name: "Cursor", logo: cursorLogo, darkLogo: cursorDarkLogo },
  zed: { name: "Zed", logo: zedLogo, darkLogo: zedDarkLogo },
};

function FileOpenTargetIcon({
  details,
  dark,
}: {
  details: OpenTargetDetails;
  dark: boolean;
}) {
  const logo = (dark && details.darkLogo) || details.logo;
  if (!logo) {
    return <FolderOpen aria-hidden="true" strokeWidth={1.5} />;
  }
  return (
    <img
      alt=""
      aria-hidden="true"
      className="size-icon-sm shrink-0 object-contain"
      draggable={false}
      src={logo}
    />
  );
}

const preferenceKey = "bot.workspace-file-open-target";
const preferenceListeners = new Set<() => void>();
let sessionPreference: WorkspaceFileOpenTarget | undefined;

function subscribePreference(listener: () => void) {
  preferenceListeners.add(listener);
  return () => {
    preferenceListeners.delete(listener);
  };
}
function preferredTarget() {
  if (sessionPreference) return sessionPreference;
  try {
    return localStorage.getItem(preferenceKey);
  } catch {
    return null;
  }
}

function rememberTarget(target: WorkspaceFileOpenTarget) {
  try {
    localStorage.setItem(preferenceKey, target);
    sessionPreference = undefined;
  } catch {
    sessionPreference = target;
  }
  for (const listener of preferenceListeners) listener();
}

export function WorkspaceFileOpenActions({
  active,
  chatId,
  root,
  relativePath,
  line,
}: {
  active: boolean;
  chatId?: string;
  root?: string;
  relativePath?: string;
  line?: number;
}) {
  const { t } = useTranslation("chat");
  const { resolvedTheme } = useTheme();
  const dark = resolvedTheme === "dark";
  const preference = useSyncExternalStore(
    subscribePreference,
    preferredTarget,
    () => null,
  );
  const { data: targets, refetch } = useQuery({
    queryKey: ["workspace-file-open-targets"],
    queryFn: listWorkspaceFileOpenTargets,
    staleTime: 60_000,
    retry: false,
    enabled: active,
  });
  const primary =
    targets?.find((target) => target === preference) ?? targets?.[0];
  if (!primary) return null;

  const platform = installationPlatform();
  const fileManager =
    platform === "macos"
      ? { name: "Finder", logo: finderLogo }
      : platform === "windows"
        ? { name: t("workspace.fileExplorer"), logo: fileExplorerLogo }
        : { name: t("workspace.fileManager") };
  const targetDetails = (target: WorkspaceFileOpenTarget): OpenTargetDetails =>
    target === "file-manager" ? fileManager : editors[target];
  const primaryTarget = targetDetails(primary);
  const label =
    primary === "file-manager"
      ? t("workspace.revealWorkspaceFile", { manager: primaryTarget.name })
      : t("workspace.openInFileEditor", { editor: primaryTarget.name });
  const open = async (target: WorkspaceFileOpenTarget) => {
    if (!root || !relativePath) return;
    try {
      if (
        !(await openWorkspaceFileTarget({
          chatId,
          root,
          relativePath,
          target,
          line,
        }))
      )
        throw new Error("External file launch was rejected");
      rememberTarget(target);
    } catch {
      toast.error(
        t("workspace.openInFileEditorError", {
          editor: targetDetails(target).name,
        }),
      );
    }
  };
  const chooseLabel = t("workspace.chooseFileEditor");
  return (
    <div role="group" className="flex shrink-0 items-center">
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            aria-label={label}
            attached="start"
            className="border-button-outline border"
            disabled={!root || !relativePath}
            size="icon-sm"
            variant="ghost"
            onClick={() => void open(primary)}
          >
            <FileOpenTargetIcon details={primaryTarget} dark={dark} />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
      <DropdownMenu
        onOpenChange={(opened) => {
          if (opened) void refetch();
        }}
      >
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button
                aria-label={chooseLabel}
                attached="end"
                className="border-button-outline -ml-px border"
                size="icon-sm"
                variant="ghost"
              >
                <ChevronDown aria-hidden="true" strokeWidth={1.5} />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent>{chooseLabel}</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end">
          <DropdownMenuRadioGroup
            value={primary}
            onValueChange={(value) => {
              const target = targets?.find((option) => option === value);
              if (target) rememberTarget(target);
            }}
          >
            {targets?.map((target) => {
              const details = targetDetails(target);
              return (
                <DropdownMenuRadioItem key={target} value={target}>
                  <FileOpenTargetIcon details={details} dark={dark} />
                  {details.name}
                </DropdownMenuRadioItem>
              );
            })}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
