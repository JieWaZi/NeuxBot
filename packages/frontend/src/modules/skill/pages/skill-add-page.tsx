import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Link,
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { LoaderCircle } from "lucide-react";
import { uploadFileBytesLimit } from "@neuxbot/platform-contracts/upload-byte-limits";
import { WakerDetailLayout } from "@/modules/waker";
import {
  PluginInstallDialog,
  invalidatePluginQueries,
  type PluginDetail,
} from "@/modules/plugin";
import {
  scanLibrarySkillImportMutation,
  importLibrarySkillsMutation,
  installLibrarySkillMutation,
  createPluginFromSkillsMutation,
} from "@/shared/api/generated/@tanstack/react-query.gen";
import {
  localApiErrorMessage,
  withLocalErrorMutation,
} from "@/shared/api/local-error-surface";
import { resourceListBack } from "@/shared/lib/resource-navigation";
import {
  selectFiles,
  stageAttachment,
  discardAttachment,
} from "@/shared/platform";
import { ApiError } from "@/shared/api/errors";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";
import { Card, CardContent } from "@/shared/ui/card";
import { FileDropZone } from "@/shared/ui/file-drop-zone";
import { Skeleton } from "@/shared/ui/skeleton";
import { PageHeader } from "@/shared/ui/page-header";
import {
  ResourcePageFooter,
  ResourcePageSurface,
} from "@/shared/ui/resource-page";
import {
  ResourceCatalogRow,
  ResourceCatalogRows,
} from "@/shared/ui/resource-catalog";
import { SceneBreadcrumbs } from "@/shared/ui/scene-breadcrumbs";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import { invalidateVisibleSkillQueries } from "../api/skill-queries";

type Source = "git" | "zip";

type Candidate = {
  description: string;
  id: string;
  installed: boolean;
  installedSkillId: string;
  name: string;
  selected: boolean;
};

const initialImportVersion = "1.0.0";

export function SkillAddPage() {
  const { t } = useTranslation("skill");
  const formId = useId();
  const gitHelpId = useId();
  const pluginHelpId = useId();
  const { wakerId = "" } = useParams();
  const routeLocation = useLocation();
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const client = useQueryClient();
  const fallback = wakerId
    ? routeLocation.pathname.includes("/resources/")
      ? `/wakers/${wakerId}/resources?category=skills`
      : `/wakers/${wakerId}/skills`
    : "/resources?category=skills";
  const back = resourceListBack(params.get("returnTo"), fallback);
  const source: Source = params.get("source") === "zip" ? "zip" : "git";
  const setSource = (value: Source) => {
    releaseZip();
    setParams(
      (current) => {
        const next = new URLSearchParams(current);
        if (value === "git") next.delete("source");
        else next.set("source", value);
        return next;
      },
      { replace: true },
    );
    setLocation("");
    resetCandidates();
    scan.reset();
    setFileError(null);
    setZipName("");
  };
  const [location, setLocation] = useState("");
  const [zipName, setZipName] = useState("");
  const [uploadingZip, setUploadingZip] = useState(false);
  const ownedZip = useRef("");
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      if (ownedZip.current)
        void discardAttachment(ownedZip.current).catch(() => undefined);
    };
  }, []);
  const releaseZip = () => {
    const previous = ownedZip.current;
    ownedZip.current = "";
    if (previous) void discardAttachment(previous).catch(() => undefined);
  };
  const acceptZip = async (path: string, name: string) => {
    if (!alive.current) {
      await discardAttachment(path);
      return;
    }
    releaseZip();
    ownedZip.current = path;
    setLocation(path);
    setZipName(name);
    resetCandidates();
    scan.reset();
  };
  const [token, setToken] = useState("");
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [packaging, setPackaging] = useState(false);
  const [pluginName, setPluginName] = useState("");
  const [pluginDescription, setPluginDescription] = useState("");
  const [installingCreatedPlugin, setInstallingCreatedPlugin] = useState(false);
  const scan = useMutation({
    ...withLocalErrorMutation(scanLibrarySkillImportMutation()),
    onSuccess: (batch) => {
      // 扫描成功后暂存上传已由 Core 接纳，页面不再拥有释放权。
      ownedZip.current = "";
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
  const [savedResources, setSavedResources] = useState<
    { id: string; name: string }[]
  >([]);
  const [installingPartners, setInstallingPartners] = useState(false);
  const [partnerError, setPartnerError] = useState<unknown>(null);
  const partnerInstall = useMutation(
    withLocalErrorMutation(installLibrarySkillMutation()),
  );
  const installForPartner = async (
    resources: { id: string; name: string }[],
  ) => {
    setInstallingPartners(true);
    setPartnerError(null);
    const failed: { id: string; name: string }[] = [];
    for (const resource of resources) {
      try {
        await partnerInstall.mutateAsync({
          path: { skillId: resource.id },
          body: { wakerIds: [wakerId] },
        });
      } catch (error) {
        failed.push(resource);
        setPartnerError(error);
      }
    }
    setSavedResources(failed);
    await invalidateVisibleSkillQueries(client);
    setInstallingPartners(false);
    if (failed.length === 0) navigate(back);
  };
  const install = useMutation({
    ...withLocalErrorMutation(importLibrarySkillsMutation()),
    onSuccess: async (items) => {
      await invalidateVisibleSkillQueries(client);
      if (!wakerId) {
        navigate(back);
        return;
      }
      const resources = items.flatMap((item) =>
        item.id ? [{ id: item.id, name: item.name ?? "" }] : [],
      );
      setSavedResources(resources);
      await installForPartner(resources);
    },
  });
  const openCreatedPlugin = (detail: PluginDetail) => {
    const id = detail.plugin?.id;
    if (!id) throw new ApiError(t("import.pluginCreateFailed"));
    const version = detail.resolvedVersionId;
    navigate(
      `/resources/plugins/${encodeURIComponent(id)}${version ? `?version=${encodeURIComponent(version)}` : ""}`,
      {
        state: { resourceBack: "/resources?category=plugins" },
      },
    );
  };
  const createPlugin = useMutation({
    ...withLocalErrorMutation(createPluginFromSkillsMutation()),
    onSuccess: async (detail) => {
      await invalidatePluginQueries(client);
      if (!alive.current) return;
      if (wakerId) setInstallingCreatedPlugin(true);
      else openCreatedPlugin(detail);
    },
  });
  const selected = candidates.filter((candidate) => candidate.selected);
  const displayedCandidates = packaging ? selected : candidates;
  const sourceReady = Boolean(location.trim());
  const installReady = Boolean(token) && selected.length > 0;
  const pluginNameTooLong = Array.from(pluginName.trim()).length > 120;
  const pluginSelectionTooLarge = selected.length > 100;
  const pluginReady =
    Boolean(token && pluginName.trim()) &&
    selected.length >= 2 &&
    !pluginNameTooLong &&
    !pluginSelectionTooLarge;
  const pending =
    uploadingZip ||
    scan.isPending ||
    install.isPending ||
    createPlugin.isPending ||
    installingPartners;
  const registered = savedResources.length > 0 || installingPartners;

  const [fileError, setFileError] = useState<unknown>(null);
  const resetCandidates = () => {
    setToken("");
    setCandidates([]);
    setPackaging(false);
    setPluginName("");
    setPluginDescription("");
    createPlugin.reset();
    install.reset();
  };
  const reset = () => {
    releaseZip();
    setLocation("");
    resetCandidates();
    scan.reset();
    setFileError(null);
    setZipName("");
    partnerInstall.reset();
  };
  const changeSource = () => {
    resetCandidates();
    scan.reset();
    // 上传引用在成功扫描后已被 Core 消费，必须重新选择 ZIP。
    if (source === "zip" && location.startsWith("upload:")) {
      setLocation("");
      setZipName("");
    }
  };
  const chooseZip = async () => {
    setUploadingZip(true);
    try {
      const files = await selectFiles();
      const file = files[0];
      if (files.length > 1) {
        await Promise.allSettled(files.map(discardAttachment));
        throw new ApiError(t("import.zipSingle"));
      }
      if (file) {
        if (!file.startsWith("upload:") && !/\.zip$/i.test(file))
          throw new ApiError(t("import.zipOnly"));
        await acceptZip(
          file,
          file.startsWith("upload:")
            ? t("import.zipSelected")
            : file.split(/[\\/]/).pop() || "",
        );
      }
      setFileError(null);
    } catch (error) {
      setFileError(error);
    } finally {
      if (alive.current) setUploadingZip(false);
    }
  };
  const dropZip = async (files: File[]) => {
    if (!files.length || pending || token) return;
    setFileError(null);
    if (files.length !== 1) {
      setFileError(new ApiError(t("import.zipSingle")));
      return;
    }
    const file = files[0];
    if (!file || !/\.zip$/i.test(file.name)) {
      setFileError(new ApiError(t("import.zipOnly")));
      return;
    }
    if (file.size > uploadFileBytesLimit) {
      setFileError(new ApiError(t("import.zipTooLarge")));
      return;
    }
    setUploadingZip(true);
    try {
      const path = await stageAttachment(file);
      await acceptZip(path, file.name);
    } catch (error) {
      setFileError(error);
    } finally {
      if (alive.current) setUploadingZip(false);
    }
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
      body: { kind: source, location: location.trim() },
    });
  };
  const installSelected = () => {
    if (!installReady) return;
    install.mutate({
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

  const createSelectedPlugin = () => {
    if (!pluginReady || pending || createPlugin.data) return;
    createPlugin.mutate({
      body: {
        token,
        candidateIds: selected.map((candidate) => candidate.id),
        name: pluginName.trim(),
        description: pluginDescription.trim() || undefined,
      },
    });
  };

  const error =
    fileError ??
    (registered
      ? partnerError
      : packaging
        ? createPlugin.error
        : scan.isError
          ? scan.error
          : install.error);
  const dirty = Boolean(location || token || error || uploadingZip);
  return (
    <WakerDetailLayout active="resources" showBreadcrumbs={false}>
      <SceneBreadcrumbs
        ariaLabel={t("title")}
        items={[
          { label: t("title"), href: back },
          { label: t("import.title") },
        ]}
      />
      <ResourcePageSurface appearance="catalog" contentLayout="form">
        <PageHeader
          appearance="compact"
          layout="form"
          title={t("import.title")}
          backAction={{
            label: t("detail.backToList"),
            onClick: () => navigate(back),
          }}
          actions={
            !packaging && (
              <SegmentedControl
                appearance="catalog-form"
                size="sm"
                ariaLabel={t("import.source")}
                value={source}
                disabled={pending || registered}
                options={[
                  { value: "git", label: t("import.sourceGit") },
                  { value: "zip", label: t("import.sourceZip") },
                ]}
                onValueChange={setSource}
              />
            )
          }
          className="mb-6"
        />
        <form
          id={formId}
          className="flex min-w-0 flex-1 flex-col gap-6"
          onSubmit={(event) => {
            event.preventDefault();
            if (pending) return;
            if (registered) void installForPartner(savedResources);
            else if (packaging) createSelectedPlugin();
            else if (token) installSelected();
            else scanSource();
          }}
        >
          {packaging ? (
            <div className="grid min-w-0 gap-4">
              <label className="grid gap-2">
                <span className="type-body font-medium">
                  {t("import.pluginName")}
                </span>
                <Input
                  appearance="catalog"
                  value={pluginName}
                  aria-label={t("import.pluginName")}
                  aria-describedby={pluginHelpId}
                  aria-invalid={pluginNameTooLong || undefined}
                  autoFocus
                  onChange={(event) => setPluginName(event.target.value)}
                  placeholder={t("import.pluginNamePlaceholder")}
                  required
                  disabled={pending || Boolean(createPlugin.data)}
                />
                <span
                  id={pluginHelpId}
                  className="text-resource-muted type-caption"
                >
                  {t("import.pluginNameHelp")}
                </span>
                {pluginNameTooLong && (
                  <p role="alert" className="text-destructive type-caption">
                    {t("import.pluginNameTooLong")}
                  </p>
                )}
              </label>
              <label className="grid gap-2">
                <span className="type-body font-medium">
                  {t("import.pluginDescription")}
                </span>
                <Textarea
                  appearance="catalog"
                  value={pluginDescription}
                  onChange={(event) => setPluginDescription(event.target.value)}
                  placeholder={t("import.pluginDescriptionPlaceholder")}
                  disabled={pending || Boolean(createPlugin.data)}
                />
              </label>
            </div>
          ) : source === "git" ? (
            <label className="grid min-w-0 gap-2">
              <span className="type-body font-medium">
                {t("import.gitUrl")}
              </span>
              <Input
                appearance="catalog"
                value={location}
                placeholder={t("import.gitPlaceholder")}
                type="url"
                disabled={pending || Boolean(token)}
                aria-label={t("import.gitUrl")}
                aria-describedby={gitHelpId}
                onChange={(event) => setLocation(event.target.value)}
              />
              <span id={gitHelpId} className="text-resource-muted type-caption">
                {t("import.gitHelp")}
              </span>
            </label>
          ) : (
            <div className="grid min-w-0 gap-2">
              <span className="type-body font-medium">
                {t("import.zipFile")}
              </span>
              <FileDropZone
                label={t("import.chooseZip")}
                description={t(
                  uploadingZip ? "import.zipUploading" : "import.zipDropHelp",
                )}
                fileName={zipName}
                disabled={pending || Boolean(token)}
                onChoose={() => void chooseZip()}
                onFiles={(files) => void dropZip(files)}
              />
              <p className="text-resource-muted type-caption">
                {t("import.zipHelp")}
              </p>
            </div>
          )}
          {registered && (
            <section className="grid gap-3" aria-label={t("import.savedTitle")}>
              <p className="text-resource-muted type-body">
                {t("import.savedDescription")}
              </p>
              <ResourceCatalogRows>
                {savedResources.map((resource) => (
                  <ResourceCatalogRow key={resource.id}>
                    <Link
                      className="type-resource-body text-resource-content hover:underline"
                      to={`/resources/skills/${resource.id}`}
                    >
                      {resource.name}
                    </Link>
                  </ResourceCatalogRow>
                ))}
              </ResourceCatalogRows>
            </section>
          )}
          {!registered && (
            <section
              className="grid min-w-0 gap-3"
              aria-label={t(
                packaging ? "import.pluginSkills" : "import.detectedTitle",
              )}
              aria-busy={scan.isPending}
            >
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h2 className="type-body font-medium">
                  {t(
                    packaging ? "import.pluginSkills" : "import.detectedTitle",
                  )}
                </h2>
                {token && (
                  <span className="text-resource-muted type-caption">
                    {t("import.candidateCount", {
                      count: displayedCandidates.length,
                    })}
                  </span>
                )}
                {token && !packaging && (
                  <Button
                    type="button"
                    size="catalog"
                    variant="ghost"
                    disabled={pending || registered}
                    onClick={changeSource}
                  >
                    {t("import.changeSource")}
                  </Button>
                )}
              </div>
              <Card>
                <CardContent>
                  {scan.isPending ? (
                    <div
                      role="status"
                      aria-busy="true"
                      aria-label={t("import.scanning")}
                      className="grid min-h-36 gap-3 py-3"
                    >
                      {Array.from({ length: 3 }, (_, index) => (
                        <div
                          key={index}
                          className="flex min-w-0 items-center gap-4"
                          aria-hidden="true"
                        >
                          <Skeleton className="size-4 shrink-0" />
                          <div className="grid min-w-0 flex-1 gap-2">
                            <Skeleton className="h-4 w-1/3" />
                            <Skeleton className="h-4 w-2/3" />
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : !token || candidates.length === 0 ? (
                    <div
                      className="grid min-h-36 content-center gap-1 py-6 text-center"
                      role="status"
                      aria-live="polite"
                    >
                      <p className="type-body font-medium">
                        {t(
                          scan.isError
                            ? "import.scanFailed"
                            : token
                              ? "import.noCandidates"
                              : "import.awaitingScan",
                        )}
                      </p>
                      <p className="text-resource-muted type-caption">
                        {t(
                          token || scan.isError
                            ? "import.noCandidatesDescription"
                            : "import.awaitingScanHelp",
                        )}
                      </p>
                    </div>
                  ) : (
                    <ResourceCatalogRows>
                      {displayedCandidates.map((candidate) => (
                        <ResourceCatalogRow key={candidate.id}>
                          {!packaging && (
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
                          )}
                          <div className="min-w-0 flex-1">
                            <h3
                              className="type-resource-body truncate font-medium"
                              title={candidate.name}
                            >
                              {candidate.name}
                            </h3>
                            <p
                              className="text-resource-muted type-resource-caption truncate"
                              title={candidate.description}
                            >
                              {candidate.description}
                            </p>
                          </div>
                          {!packaging &&
                            candidate.installed &&
                            (candidate.installedSkillId ? (
                              <Button asChild size="catalog" variant="ghost">
                                <Link
                                  to={`/resources/skills/${candidate.installedSkillId}`}
                                  state={{ resourceBack: back }}
                                >
                                  {t("import.viewInstalled")}
                                </Link>
                              </Button>
                            ) : (
                              <span className="text-resource-muted type-caption">
                                {t("import.installed")}
                              </span>
                            ))}
                        </ResourceCatalogRow>
                      ))}
                    </ResourceCatalogRows>
                  )}
                </CardContent>
              </Card>
              {packaging && pluginSelectionTooLarge && (
                <p role="alert" className="text-destructive type-caption">
                  {t("import.pluginSelectionTooLarge")}
                </p>
              )}
            </section>
          )}
          {dirty && (
            <ResourcePageFooter aria-label={t("import.saveStatus")}>
              <p
                role={error ? "alert" : "status"}
                className={
                  error
                    ? "text-destructive type-caption"
                    : "text-resource-muted type-caption"
                }
              >
                {error
                  ? localApiErrorMessage(
                      error,
                      t(
                        registered
                          ? "import.partnerInstallFailed"
                          : packaging
                            ? "import.pluginCreateFailed"
                            : fileError
                              ? "import.zipFailed"
                              : scan.isError
                                ? "import.scanFailed"
                                : "import.installFailed",
                      ),
                    )
                  : t(
                      registered
                        ? "import.savedTitle"
                        : createPlugin.data
                          ? "import.pluginCreated"
                          : "import.unsaved",
                    )}
              </p>
              <div className="ml-auto flex flex-wrap items-center gap-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="catalog"
                  disabled={pending || registered}
                  onClick={packaging ? () => setPackaging(false) : reset}
                >
                  {t(packaging ? "import.cancelPlugin" : "import.reset")}
                </Button>
                {!packaging && !registered && token && selected.length >= 2 && (
                  <Button
                    type="button"
                    variant="catalog-outline"
                    size="catalog"
                    disabled={pending}
                    onClick={() => setPackaging(true)}
                  >
                    {t("import.addAsPlugin")}
                  </Button>
                )}
                <Button
                  type="submit"
                  form={formId}
                  size="catalog"
                  disabled={
                    pending ||
                    (!registered &&
                      (packaging
                        ? !pluginReady || Boolean(createPlugin.data)
                        : token
                          ? !installReady
                          : !sourceReady))
                  }
                >
                  {pending && <LoaderCircle className="animate-spin" />}
                  {packaging
                    ? createPlugin.isPending
                      ? t("import.pluginCreating")
                      : t("import.createPlugin")
                    : registered
                      ? installingPartners
                        ? t("import.partnerInstalling")
                        : t("import.retryPartnerInstall")
                      : token
                        ? install.isPending
                          ? t("import.installing")
                          : t("import.installSelected", {
                              count: selected.length,
                            })
                        : scan.isPending
                          ? t("import.scanning")
                          : t("import.scan")}
                </Button>
              </div>
            </ResourcePageFooter>
          )}
        </form>
        {installingCreatedPlugin && createPlugin.data && (
          <PluginInstallDialog
            detail={createPlugin.data}
            wakerId={wakerId}
            onClose={() => {
              setInstallingCreatedPlugin(false);
              if (createPlugin.data) openCreatedPlugin(createPlugin.data);
            }}
          />
        )}
      </ResourcePageSurface>
    </WakerDetailLayout>
  );
}
