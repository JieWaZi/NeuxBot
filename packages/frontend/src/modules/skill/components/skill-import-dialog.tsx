import { cn } from "@/shared/lib/cn";
import { useMutation } from "@tanstack/react-query";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { ArrowLeft, GitBranch, LoaderCircle, Upload } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";

import { selectFiles } from "@/shared/platform";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";

import {
  importWakerSkillMutation,
  scanWakerSkillImportMutation,
} from "../api/skill-queries";

type Source = "git" | "zip";

type Candidate = {
  description: string;
  id: string;
  installed: boolean;
  installedSkillId: string;
  name: string;
  selected: boolean;
};

type Props = {
  onImported: () => void;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  wakerId: string;
};

const initialImportVersion = "1.0.0";

export function SkillImportDialog({ open, ...props }: Props) {
  if (!open) return null;
  return <OpenSkillImportDialog {...props} open />;
}

function OpenSkillImportDialog({
  onImported,
  onOpenChange,
  open,
  wakerId,
}: Props) {
  const { t } = useTranslation("skill");
  const [source, setSource] = useState<Source>("git");
  const [location, setLocation] = useState("");
  const [token, setToken] = useState("");
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const scan = useMutation({
    ...withLocalErrorMutation(scanWakerSkillImportMutation()),
    onSuccess: (batch) => {
      setToken(batch.token ?? "");
      setCandidates(
        (batch.candidates ?? []).flatMap((candidate) => {
          if (!candidate.id || !candidate.name) return [];
          return [
            {
              id: candidate.id,
              name: candidate.name,
              description: candidate.description ?? "",
              installed: candidate.installed ?? false,
              installedSkillId: candidate.installedSkillId ?? "",
              selected: !(candidate.installed ?? false),
            },
          ];
        }),
      );
    },
  });
  const install = useMutation({
    ...withLocalErrorMutation(importWakerSkillMutation()),
    onSuccess: () => {
      onImported();
      onOpenChange(false);
    },
  });
  const selected = candidates.filter((candidate) => candidate.selected);
  const sourceReady = Boolean(location.trim());
  const installReady = Boolean(token) && selected.length > 0;
  const pending = scan.isPending || install.isPending;

  const chooseZip = async () => {
    const files = await selectFiles();
    if (files[0]) setLocation(files[0]);
  };
  const updateCandidate = (id: string, update: Partial<Candidate>) => {
    setCandidates((current) =>
      current.map((candidate) =>
        candidate.id === id ? { ...candidate, ...update } : candidate,
      ),
    );
  };
  const scanSource = () => {
    if (!sourceReady) return;
    scan.mutate({
      path: { wakerId },
      body: { kind: source, location: location.trim() },
    });
  };
  const installSelected = () => {
    if (!installReady) return;
    install.mutate({
      path: { wakerId },
      body: {
        token,
        candidates: selected.map((candidate) => ({
          id: candidate.id,
          version: initialImportVersion,
          description: candidate.description,
        })),
      },
    });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!pending) onOpenChange(nextOpen);
      }}
    >
      <DialogContent
        title={t("import.title")}
        description={
          token ? t("import.candidateDescription") : t("import.description")
        }
        layout="form"
        size="lg"
      >
        <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-5">
          {token ? (
            <div className="grid gap-3">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <p className="text-muted-foreground type-body">
                  {t("import.candidateCount", { count: candidates.length })}
                </p>
                <div className="flex flex-wrap items-center gap-3">
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={pending}
                    onClick={() => {
                      setToken("");
                      setCandidates([]);
                      install.reset();
                    }}
                  >
                    <ArrowLeft />
                    {t("import.changeSource")}
                  </Button>
                </div>
              </div>
              {candidates.length > 0 ? null : (
                <div className="border-border grid min-h-32 place-items-center rounded-md border border-dashed p-6 text-center">
                  <div>
                    <p className="type-label font-medium">
                      {t("import.noCandidates")}
                    </p>
                    <p className="text-muted-foreground type-caption mt-1">
                      {t("import.noCandidatesDescription")}
                    </p>
                  </div>
                </div>
              )}
              {candidates.map((candidate) => (
                <div
                  key={candidate.id}
                  className="border-border grid gap-3 rounded-md border p-3"
                >
                  <div className="flex min-w-0 items-start gap-3">
                    <Checkbox
                      checked={candidate.selected}
                      disabled={candidate.installed || pending}
                      aria-label={t("import.selectCandidate", {
                        name: candidate.name,
                      })}
                      onCheckedChange={(checked) =>
                        updateCandidate(candidate.id, {
                          selected: checked === true,
                        })
                      }
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="type-label font-semibold">
                          {candidate.name}
                        </h3>
                        {candidate.installed ? (
                          <span className="bg-muted text-muted-foreground type-caption rounded-sm px-1.5 py-0.5">
                            {t("import.installed")}
                          </span>
                        ) : null}
                        {candidate.installed && candidate.installedSkillId ? (
                          <Button asChild size="sm" variant="ghost">
                            <Link
                              to={`/wakers/${wakerId}/skills/${candidate.installedSkillId}`}
                            >
                              {t("import.viewInstalled")}
                            </Link>
                          </Button>
                        ) : null}
                      </div>
                      <p className="text-muted-foreground type-caption mt-1 line-clamp-1">
                        {candidate.description}
                      </p>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="grid gap-5">
              <div
                role="radiogroup"
                aria-label={t("import.source")}
                className="grid w-full grid-cols-2 gap-2"
              >
                {(
                  [
                    {
                      value: "git" as const,
                      label: t("import.sourceGit"),
                      description: t("import.sourceGitDescription"),
                      icon: GitBranch,
                    },
                    {
                      value: "zip" as const,
                      label: t("import.sourceZip"),
                      description: t("import.sourceZipDescription"),
                      icon: Upload,
                    },
                  ] satisfies Array<{
                    value: Source;
                    label: string;
                    description: string;
                    icon: typeof GitBranch;
                  }>
                ).map((option) => {
                  const Icon = option.icon;
                  const active = source === option.value;
                  return (
                    <button
                      key={option.value}
                      type="button"
                      role="radio"
                      aria-checked={active}
                      className={cn(
                        "border-border hover:bg-muted/60 focus-visible:ring-ring grid min-h-20 grid-cols-[20px_minmax(0,1fr)] items-start gap-2 rounded-md border p-3 text-left transition-colors outline-none focus-visible:ring-2",
                        active && "border-primary/50 bg-primary/5",
                      )}
                      onClick={() => {
                        if (active) return;
                        setSource(option.value);
                        setLocation("");
                      }}
                    >
                      <Icon
                        className={cn(
                          "text-muted-foreground size-icon-sm mt-0.5",
                          active && "text-primary",
                        )}
                        strokeWidth={1.5}
                      />
                      <span className="min-w-0">
                        <span className="type-label block font-semibold">
                          {option.label}
                        </span>
                        <span className="text-muted-foreground type-caption mt-1 block">
                          {option.description}
                        </span>
                      </span>
                    </button>
                  );
                })}
              </div>
              {source === "git" ? (
                <label className="grid gap-1.5">
                  <span className="type-label font-medium">
                    {t("import.gitUrl")}
                  </span>
                  <Input
                    value={location}
                    placeholder={t("import.gitPlaceholder")}
                    type="url"
                    aria-label={t("import.gitUrl")}
                    onChange={(event) => setLocation(event.target.value)}
                  />
                  <span className="text-muted-foreground type-caption">
                    {t("import.gitHelp")}
                  </span>
                </label>
              ) : (
                <button
                  type="button"
                  className="border-border bg-muted/60 hover:bg-muted grid min-h-28 place-items-center rounded-md border border-dashed p-4 text-center transition-colors"
                  onClick={() => void chooseZip()}
                >
                  <span>
                    <Upload
                      className="text-muted-foreground size-icon-lg mx-auto"
                      strokeWidth={1.5}
                    />
                    <span className="type-body mt-2 block font-medium">
                      {location.split(/[\\/]/).pop() || t("import.chooseZip")}
                    </span>
                    <span className="text-muted-foreground type-caption mt-1 block">
                      {t("import.zipHelp")}
                    </span>
                  </span>
                </button>
              )}
            </div>
          )}
          {scan.isError || install.isError ? (
            <p role="alert" className="text-destructive type-caption mt-3">
              {localApiErrorMessage(
                scan.isError ? scan.error : install.error,
                t(scan.isError ? "import.scanFailed" : "import.installFailed"),
              )}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button
            size="md"
            variant="secondary"
            disabled={pending}
            onClick={() => onOpenChange(false)}
          >
            {t("common:cancel")}
          </Button>
          <Button
            size="md"
            disabled={
              token ? !installReady || pending : !sourceReady || pending
            }
            onClick={token ? installSelected : scanSource}
          >
            {pending ? <LoaderCircle className="animate-spin" /> : null}
            {token
              ? install.isPending
                ? t("import.installing")
                : t("import.installSelected", { count: selected.length })
              : scan.isPending
                ? t("import.scanning")
                : t("import.scan")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
