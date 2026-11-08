import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { WakerDetailLayout } from "@/modules/waker";
import {
  ResourcePageHeader,
  ResourcePageSurface,
} from "@/shared/ui/resource-page";
import { SearchField } from "@/shared/ui/search-field";
import { Button } from "@/shared/ui/button";
import { ConnectorResourceCollection } from "./connector-resource-collection";

export function WakerConnectorsPage() {
  const { t } = useTranslation("connector");
  const { wakerId = "" } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const search = params.get("search") ?? "";
  return (
    <WakerDetailLayout active="connectors">
      <ResourcePageSurface appearance="catalog">
        <ResourcePageHeader
          appearance="catalog"
          actions={null}
          title={t("title")}
          description={t("description")}
        />
        <ConnectorResourceCollection
          wakerId={wakerId}
          search={search}
          onInstall={() => undefined}
          filters={
            <>
              <div className="basis-field-search-min min-w-0 grow">
                <SearchField
                  appearance="catalog"
                  size="md"
                  width="full"
                  value={search}
                  placeholder={t("search")}
                  aria-label={t("search")}
                  onChange={(event) =>
                    setParams((current) => {
                      const next = new URLSearchParams(current);
                      if (event.target.value)
                        next.set("search", event.target.value);
                      else next.delete("search");
                      return next;
                    })
                  }
                />
              </div>
              <Button
                size="catalog"
                variant="catalog-primary"
                onClick={() =>
                  navigate(
                    `/wakers/${wakerId}/connectors/new?${new URLSearchParams({ returnTo: location.pathname + location.search })}`,
                  )
                }
              >
                <Plus />
                {t("form.createTitle")}
              </Button>
            </>
          }
        />
      </ResourcePageSurface>
    </WakerDetailLayout>
  );
}
