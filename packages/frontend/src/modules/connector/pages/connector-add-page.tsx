import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { WakerDetailLayout } from "@/modules/waker";
import { resourceListBack } from "@/shared/lib/resource-navigation";
import { PageHeader } from "@/shared/ui/page-header";
import { ResourcePageSurface } from "@/shared/ui/resource-page";
import { SceneBreadcrumbs } from "@/shared/ui/scene-breadcrumbs";
import { ConnectorForm } from "../components/connector-form-dialog";

export function ConnectorAddPage() {
  const { t } = useTranslation("connector");
  const { wakerId = "" } = useParams();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const fallback = wakerId
    ? params.get("returnTo")?.startsWith(`/wakers/${wakerId}/resources`)
      ? `/wakers/${wakerId}/resources?category=connectors`
      : `/wakers/${wakerId}/connectors`
    : "/resources?category=connectors";
  const back = resourceListBack(params.get("returnTo"), fallback);
  return (
    <WakerDetailLayout active="resources" showBreadcrumbs={false}>
      <SceneBreadcrumbs
        ariaLabel={t("title")}
        items={[
          { label: t("title"), href: back },
          { label: t("form.createTitle") },
        ]}
      />
      <ResourcePageSurface appearance="catalog" contentLayout="pane">
        <ConnectorForm
          wakerId={wakerId}
          renderHeader={(transportControl) => (
            <PageHeader
              appearance="compact"
              layout="form"
              title={t("form.createTitle")}
              backAction={{
                label: t("common:backToList"),
                onClick: () => navigate(back),
              }}
              actions={transportControl}
              className="shrink-0"
            />
          )}
          onSaved={(connectorId) => {
            if (!connectorId) return;
            navigate(
              `${wakerId ? `/wakers/${wakerId}` : "/resources"}/connectors/${connectorId}`,
              { replace: true, state: { resourceBack: back } },
            );
          }}
        />
      </ResourcePageSurface>
    </WakerDetailLayout>
  );
}
