import {
  PageAction,
  PageActionLabel,
  ResourcePageSurface,
} from "@/shared/ui/resource-page";
import { useQuery } from "@tanstack/react-query";
import { MessageCircle, Pencil } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useParams } from "react-router-dom";

import { SkillImportDialog } from "@/modules/skill";
import { ConnectorFormDialog } from "@/modules/connector";
import { AutomaticTaskAttentionSection } from "@/modules/automatic-task";
import { chatDraftHref } from "@/shared/lib/chat-navigation";
import {
  getWakerOptions,
  getWakerWorkRecordOptions,
  listWakerConnectorsOptions,
  listWakerSkillsOptions,
} from "../api/waker-queries";
import { WakerAboutSection } from "../components/waker-about-section";
import { WakerDetailLayout } from "../components/waker-detail-layout";
import { WakerOverviewColumns } from "../components/waker-overview-columns";
import { WakerFormDialog } from "../components/waker-form-dialog";
import { WakerProfileSection } from "../components/waker-profile-section";
import { WakerSkillsToolsSection } from "../components/waker-skills-tools-section";
import { WakerPatternPanel } from "../components/waker-pattern-panel";
import { WakerWorkRecordSection } from "../components/waker-work-record-section";
import { Avatar } from "@/shared/ui/avatar";
import { wakerCompanionActivity } from "@/shared/companion/activity";
import { PageHeader } from "@/shared/ui/page-header";
import { Skeleton } from "@/shared/ui/skeleton";

export function WakerDetailPage() {
  const { wakerId = "" } = useParams();
  const { t } = useTranslation("waker");
  const [editing, setEditing] = useState(false);
  const [skillImportOpen, setSkillImportOpen] = useState(false);
  const [connectorCreateOpen, setConnectorCreateOpen] = useState(false);
  const query = useQuery(getWakerOptions({ path: { wakerId } }));
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const workRecordQuery = useQuery(
    getWakerWorkRecordOptions({
      path: { wakerId },
      query: { timezone },
    }),
  );
  const skillsQuery = useQuery({
    ...listWakerSkillsOptions({ path: { wakerId } }),
    enabled: Boolean(wakerId),
  });
  const connectorsQuery = useQuery({
    ...listWakerConnectorsOptions({ path: { wakerId } }),
    enabled: Boolean(wakerId),
  });
  const waker = query.data;

  if (query.isPending) {
    return (
      <WakerDetailLayout active="overview">
        <ResourcePageSurface
          aria-busy="true"
          aria-label={t("detail.loading")}
          role="status"
        >
          <div className="w-full">
            <div className="flex flex-wrap items-center gap-5 pb-2">
              <Skeleton className="size-avatar-xl shrink-0 rounded-full" />
              <div className="flex min-w-0 flex-1 basis-80 flex-col gap-3">
                <Skeleton className="h-3.5 w-48 max-w-full" />
                <Skeleton className="h-2.5 w-full max-w-xl" />
                <Skeleton className="h-2.5 w-2/3 max-w-md" />
              </div>
            </div>
            <div className="border-border-subtle mt-3 grid gap-8 border-t pt-3">
              <AutomaticTaskAttentionSection wakerId={wakerId} />
              <Skeleton className="h-64 rounded-xl" />
              <div className="grid gap-5 @[60rem]/resource-page:grid-cols-[minmax(0,1.65fr)_minmax(20rem,1fr)]">
                <Skeleton className="h-96 rounded-xl" />
                <div className="grid gap-5">
                  <Skeleton className="h-64 rounded-xl" />
                  <Skeleton className="h-48 rounded-xl" />
                </div>
              </div>
            </div>
          </div>
        </ResourcePageSurface>
      </WakerDetailLayout>
    );
  }
  if (!waker || query.isError) {
    return (
      <WakerDetailLayout active="overview">
        <div className="type-body grid h-full place-items-center" role="alert">
          {t("detail.notFound")}
        </div>
      </WakerDetailLayout>
    );
  }

  const joined = waker.createdAt
    ? new Intl.DateTimeFormat("zh-CN", {
        year: "numeric",
        month: "long",
        day: "numeric",
      }).format(new Date(waker.createdAt))
    : t("detail.today");
  const shortId = waker.id ? waker.id.slice(0, 8) : "";
  const activityLabel = workRecordQuery.isSuccess
    ? workRecordQuery.data.activeToday
      ? t("detail.activeToday")
      : t("detail.inactiveToday")
    : null;
  return (
    <>
      <WakerDetailLayout active="overview">
        <ResourcePageSurface>
          <div className="w-full">
            <WakerPatternPanel
              className="overflow-visible rounded-xl px-4 pt-4 pb-3"
              pattern="wash-header"
              surface="scene"
            >
              <PageHeader
                layout="detail"
                visual={
                  <Avatar
                    name={waker.name ?? "伙伴"}
                    src={waker.avatar}
                    activity={wakerCompanionActivity(waker.activityState)}
                    size="xl"
                  />
                }
                title={
                  <span className="flex min-w-0 flex-wrap items-center gap-2.5">
                    <span className="[overflow-wrap:anywhere]">
                      {waker.name}
                    </span>
                    <span className="bg-muted text-muted-foreground type-ui max-w-full rounded-md px-2 py-1 font-normal [overflow-wrap:anywhere]">
                      {waker.roleName}
                    </span>
                  </span>
                }
                description={waker.bio || undefined}
                actions={
                  <>
                    <PageAction
                      intent="edit"
                      label={t("detail.editProfile")}
                      onClick={() => setEditing(true)}
                    >
                      <Pencil />
                      <PageActionLabel>
                        {t("detail.editProfile")}
                      </PageActionLabel>
                    </PageAction>
                    <PageAction
                      asChild
                      intent="primary"
                      label={t("detail.startChat")}
                    >
                      <Link
                        to={chatDraftHref(wakerId)}
                        aria-label={t("detail.startChat")}
                      >
                        <MessageCircle />
                        <PageActionLabel>
                          {t("detail.startChat")}
                        </PageActionLabel>
                      </Link>
                    </PageAction>
                  </>
                }
              >
                <div className="text-muted-foreground type-ui mt-2 flex flex-wrap items-center gap-x-4 gap-y-2">
                  {shortId ? (
                    <span className="font-mono">ID: {shortId}</span>
                  ) : null}
                  <span className="min-w-0">
                    {t("detail.skillConnectorCount", {
                      connectors:
                        connectorsQuery.data?.filter(
                          (item) => item.connector?.id,
                        ).length ?? 0,
                      skills:
                        skillsQuery.data?.filter((item) => item.skill?.id)
                          .length ?? 0,
                    })}
                  </span>
                  <span className="min-w-0">
                    {t("detail.joinedAt", { date: joined })}
                    {activityLabel ? ` · ${activityLabel}` : null}
                  </span>
                </div>
              </PageHeader>
            </WakerPatternPanel>

            <div className="border-border-subtle mt-3 grid gap-8 border-t pt-3">
              <AutomaticTaskAttentionSection wakerId={wakerId} />
              <WakerWorkRecordSection
                wakerId={wakerId}
                createdAt={waker.createdAt}
              />

              <WakerOverviewColumns
                about={
                  <WakerAboutSection
                    expertise={waker.expertise}
                    workStyle={waker.workStyle}
                    workMethods={waker.workMethods}
                  />
                }
                aside={
                  <>
                    <WakerSkillsToolsSection
                      wakerId={wakerId}
                      skills={skillsQuery.data ?? []}
                      connectors={connectorsQuery.data ?? []}
                      skillsPending={skillsQuery.isPending}
                      skillsError={skillsQuery.isError}
                      connectorsPending={connectorsQuery.isPending}
                      connectorsError={connectorsQuery.isError}
                      onAddSkill={() => setSkillImportOpen(true)}
                      onAddConnector={() => setConnectorCreateOpen(true)}
                    />

                    <WakerProfileSection waker={waker} />
                  </>
                }
              />
            </div>
          </div>
        </ResourcePageSurface>
      </WakerDetailLayout>

      <WakerFormDialog open={editing} onOpenChange={setEditing} waker={waker} />
      <SkillImportDialog
        open={skillImportOpen}
        wakerId={wakerId}
        onOpenChange={setSkillImportOpen}
        onImported={() => void skillsQuery.refetch()}
      />
      <ConnectorFormDialog
        open={connectorCreateOpen}
        wakerId={wakerId}
        onOpenChange={setConnectorCreateOpen}
      />
    </>
  );
}
