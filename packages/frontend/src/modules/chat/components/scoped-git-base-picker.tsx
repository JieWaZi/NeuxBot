import { useQuery } from "@tanstack/react-query";
import { Check } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/shared/lib/cn";
import { listWorkspaceGitBranches } from "@/shared/platform";
import { FieldControlTrigger } from "@/shared/ui/field-control-trigger";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/popover";
import { SearchField } from "@/shared/ui/search-field";

const branchLimit = 100;

type GitComparisonBasePickerProps = {
  active: boolean;
  chatId?: string;
  root: string;
  repository: string;
  value: string;
  label: string;
  automaticLabel: string;
  title?: string;
  onChange: (base: string) => void;
};

/** 菜单输入和查询结果由弹层持有，输入时无需重渲染 Diff。 */
export function GitComparisonBasePicker({
  active,
  label,
  title,
  onChange,
  ...options
}: GitComparisonBasePickerProps) {
  const { t } = useTranslation("chat");
  const [open, setOpen] = useState(false);

  return (
    <Popover open={active && open} onOpenChange={setOpen} modal={false}>
      <PopoverTrigger asChild>
        <FieldControlTrigger
          aria-label={t("workspace.git.diffBase")}
          appearance="toolbar"
          width={null}
          size="sm"
          className="max-w-44 min-w-0"
          title={title}
        >
          <span>{label}</span>
        </FieldControlTrigger>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        width="wide"
        className="flex min-h-0 flex-col overflow-hidden p-1"
        aria-label={t("workspace.git.diffBase")}
      >
        <ComparisonBaseOptions
          {...options}
          onChange={(base) => {
            setOpen(false);
            if (base !== options.value) onChange(base);
          }}
        />
      </PopoverContent>
    </Popover>
  );
}

function ComparisonBaseOptions({
  chatId,
  root,
  repository,
  value,
  automaticLabel,
  onChange,
}: Omit<GitComparisonBasePickerProps, "active" | "label" | "title">) {
  const { t } = useTranslation("chat");
  const id = useId();
  const [input, setInput] = useState("");
  const [search, setSearch] = useState("");
  const [highlight, setHighlight] = useState(-1);
  const term = input.trim();
  useEffect(() => {
    if (term === search) return;
    const timeout = setTimeout(() => setSearch(term), 180);
    return () => clearTimeout(timeout);
  }, [term, search]);
  const branches = useQuery({
    queryKey: [
      "scoped-git-branches",
      chatId,
      root,
      repository,
      search,
      branchLimit,
    ],
    queryFn: ({ signal }) =>
      listWorkspaceGitBranches(root, {
        chatId,
        repository,
        limit: branchLimit,
        signal,
        ...(search ? { query: search } : {}),
      }),
    staleTime: 30_000,
  });
  const loading = term !== search || branches.isPending;
  const normalizedSearch = search.toLowerCase();
  const matching = loading
    ? []
    : (branches.data?.branches ?? []).filter(
        ({ name, isCurrent }) =>
          !isCurrent &&
          name !== "HEAD" &&
          (!normalizedSearch || name.toLowerCase().includes(normalizedSearch)),
      );
  const branchItems = matching
    .slice(0, branchLimit)
    .map(({ name }) => ({ value: name, label: name }));
  const items = term
    ? branchItems
    : [{ value: "", label: automaticLabel }, ...branchItems];
  const highlighted = items[highlight];

  return (
    <>
      <SearchField
        role="combobox"
        aria-label={t("branchSwitch.search")}
        clearLabel={t("common:clearSearch")}
        aria-autocomplete="list"
        aria-expanded
        aria-controls={id}
        aria-activedescendant={highlighted ? `${id}-${highlight}` : undefined}
        placeholder={t("branchSwitch.searchPlaceholder")}
        value={input}
        width="full"
        containerClassName="shrink-0"
        onChange={(event) => {
          setInput(event.target.value);
          setHighlight(-1);
        }}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing) return;
          if (event.key === "Enter" && highlighted) {
            event.preventDefault();
            onChange(highlighted.value);
          }
          if (
            !items.length ||
            (event.key !== "ArrowDown" && event.key !== "ArrowUp")
          )
            return;
          event.preventDefault();
          const next =
            event.key === "ArrowDown"
              ? Math.min(highlight + 1, items.length - 1)
              : highlight < 0
                ? items.length - 1
                : Math.max(highlight - 1, 0);
          setHighlight(next);
          document.getElementById(`${id}-${next}`)?.scrollIntoView?.({
            block: "nearest",
          });
        }}
      />
      <div className="mt-1 max-h-64 min-h-0 overflow-y-auto overscroll-contain">
        <div
          id={id}
          role="listbox"
          aria-label={t("branchSwitch.listLabel")}
          aria-busy={loading}
        >
          {items.map((item, index) => (
            <div
              key={item.value}
              id={`${id}-${index}`}
              role="option"
              aria-selected={item.value === value}
              title={item.label}
              className={cn(
                "type-ui hover:bg-action-quiet-hover min-h-row-compact flex cursor-pointer items-center gap-2 rounded-sm px-2 select-none",
                index === highlight && "bg-action-quiet-hover",
                item.value === value && "bg-selection-surface",
              )}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => onChange(item.value)}
            >
              <span className="flex-1 truncate">{item.label}</span>
              {item.value === value && (
                <Check
                  aria-hidden
                  className="text-accent-content size-icon-sm shrink-0"
                />
              )}
            </div>
          ))}
        </div>
        {loading ? (
          <p
            role="status"
            className="text-content-muted type-caption px-2 py-1"
          >
            {t("branchSwitch.loading")}
          </p>
        ) : branches.isError ? (
          <p role="alert" className="text-destructive type-caption px-2 py-1">
            {t("branchSwitch.loadError")}
          </p>
        ) : term && !matching.length ? (
          <p
            role="status"
            className="text-content-muted type-caption px-2 py-1"
          >
            {t("branchSwitch.empty")}
          </p>
        ) : null}
      </div>
      {!loading && (branches.data?.hasMore || matching.length > branchLimit) ? (
        <p className="text-content-muted type-caption shrink-0 px-2 py-1">
          {t("workspace.git.refineBranchSearch")}
        </p>
      ) : null}
    </>
  );
}
