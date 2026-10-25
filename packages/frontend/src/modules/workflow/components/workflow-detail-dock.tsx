import {
  History,
  Check,
  Ellipsis,
  Pencil,
  Play,
  Trash2,
  type LucideIcon,
} from "lucide-react";
import { Fragment } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/button";
import { Dock, DockIcon } from "@/shared/ui/dock";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/dropdown-menu";
import { Separator } from "@/shared/ui/separator";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/shared/ui/tooltip";

type WorkflowDetailDockProps = {
  availableWidth: number;
  showingHistory: boolean;
  historyPaneId: string;
  readOnly: boolean;
  onEdit: () => void;
  onRun: () => void;
  onVersions: () => void;
  onDelete: () => void;
};

type WorkflowDockAction = {
  key: "run" | "versions" | "edit" | "deleteWorkflow";
  icon: LucideIcon;
  onClick: () => void;
  selected?: boolean;
  expanded?: boolean;
  controls?: string;
  disabled?: boolean;
  destructive?: boolean;
};

export function WorkflowDetailDock({
  availableWidth,
  showingHistory,
  historyPaneId,
  readOnly,
  onEdit,
  onRun,
  onVersions,
  onDelete,
}: WorkflowDetailDockProps) {
  const { t } = useTranslation("workflow");
  const navigationActions: WorkflowDockAction[] = [
    { key: "run", icon: Play, onClick: onRun },
    {
      key: "versions",
      icon: History,
      onClick: onVersions,
      selected: showingHistory,
      expanded: showingHistory,
      controls: historyPaneId,
    },
  ];
  const managementActions: WorkflowDockAction[] = [
    { key: "edit", icon: Pencil, onClick: onEdit, disabled: readOnly },
    {
      key: "deleteWorkflow",
      icon: Trash2,
      onClick: onDelete,
      destructive: true,
    },
  ];

  const renderAction = ({
    key,
    icon: Icon,
    onClick,
    ...state
  }: WorkflowDockAction) => (
    <DockIcon key={key}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="dock"
            shape="circle"
            aria-label={t(key)}
            aria-pressed={state.selected}
            aria-expanded={state.expanded}
            aria-controls={state.controls}
            disabled={state.disabled}
            onClick={onClick}
            className={cn(
              state.selected && "text-content-primary",
              state.destructive &&
                "text-destructive hover:bg-destructive/10 hover:text-destructive",
            )}
          >
            <Icon aria-hidden="true" strokeWidth={1.5} />
          </Button>
        </TooltipTrigger>
        <TooltipContent side="top">
          <p>{t(key)}</p>
        </TooltipContent>
      </Tooltip>
    </DockIcon>
  );

  const renderMenu = (
    group: "navigation" | "management" | "all",
    actions: WorkflowDockAction[],
  ) => {
    const label = t(
      group === "management" ? "workflowManagement" : "workflowActions",
    );
    return (
      <DockIcon key={group === "management" ? "management" : "navigation"}>
        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="dock"
                  shape="circle"
                  aria-label={label}
                >
                  <Ellipsis aria-hidden="true" strokeWidth={1.5} />
                </Button>
              </DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent side="top">{label}</TooltipContent>
          </Tooltip>
          <DropdownMenuContent side="top" align="center">
            {actions.map((action, index) => (
              <Fragment key={action.key}>
                {group === "all" && index === navigationActions.length ? (
                  <DropdownMenuSeparator />
                ) : null}
                <DropdownMenuItem
                  disabled={action.disabled}
                  onSelect={action.onClick}
                  className={cn(
                    action.destructive &&
                      "text-destructive data-[highlighted]:text-destructive",
                  )}
                >
                  <action.icon aria-hidden="true" strokeWidth={1.5} />
                  <span className="flex-1">{t(action.key)}</span>
                  {action.selected ? (
                    <Check aria-hidden="true" strokeWidth={1.5} />
                  ) : null}
                </DropdownMenuItem>
              </Fragment>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </DockIcon>
    );
  };

  return (
    <nav
      aria-label={t("title")}
      className="pointer-events-auto max-w-full min-w-0"
    >
      <TooltipProvider>
        <Dock
          direction="middle"
          className="max-w-full"
          disableMagnification={availableWidth < 480}
        >
          {availableWidth < 160
            ? renderMenu("all", [...navigationActions, ...managementActions])
            : [
                ...(availableWidth < 352
                  ? [renderMenu("navigation", navigationActions)]
                  : navigationActions.map(renderAction)),
                <Separator
                  key="groups"
                  orientation="vertical"
                  className="h-full"
                />,
                ...(availableWidth < 216
                  ? [renderMenu("management", managementActions)]
                  : managementActions.map(renderAction)),
              ]}
        </Dock>
      </TooltipProvider>
    </nav>
  );
}
