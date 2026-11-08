import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  useLocation,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { WakerDetailLayout } from "@/modules/waker";
import { Button } from "@/shared/ui/button";
import { SceneBreadcrumbs } from "@/shared/ui/scene-breadcrumbs";
import { SearchField } from "@/shared/ui/search-field";
import {
  ResourcePageHeader,
  ResourcePageSurface,
} from "@/shared/ui/resource-page";
import { SkillResourceCollection } from "./skill-resource-collection";

export function WakerSkillsPage() {
  const { t } = useTranslation("skill");
  const { wakerId = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const search = params.get("search") ?? "";
  const category = params.get("skillCategory");
  const location = useLocation();
  const navigate = useNavigate();
  const categoryName = category || t("category.uncategorized");
  const searchLabel =
    category === null
      ? t("search")
      : t("category.searchIn", { name: categoryName });
  const libraryParams = new URLSearchParams(params);
  libraryParams.delete("skillCategory");
  const libraryHref = `/wakers/${wakerId}/skills${libraryParams.size ? `?${libraryParams}` : ""}`;
  const selectCategory = (value: string | null) =>
    setParams((current) => {
      const next = new URLSearchParams(current);
      if (value === null) next.delete("skillCategory");
      else next.set("skillCategory", value);
      return next;
    });
  return (
    <WakerDetailLayout active="skills" showBreadcrumbs={category === null}>
      {category !== null && (
        <SceneBreadcrumbs
          ariaLabel={t("title")}
          items={[
            { label: t("title"), href: libraryHref },
            { label: categoryName },
          ]}
        />
      )}
      <ResourcePageSurface appearance="catalog">
        <ResourcePageHeader
          appearance="catalog"
          title={
            category === null
              ? t("title")
              : category || t("category.uncategorized")
          }
          description={category === null ? t("libraryDescription") : undefined}
          actions={undefined}
        />
        <SkillResourceCollection
          wakerId={wakerId}
          search={search}
          category={category}
          onCategoryChange={selectCategory}
          onInstall={() => undefined}
          filters={
            <>
              <div className="basis-field-search-min min-w-0 grow">
                <SearchField
                  appearance="catalog"
                  width="full"
                  value={search}
                  onChange={(event) =>
                    setParams((current) => {
                      const next = new URLSearchParams(current);
                      if (event.target.value)
                        next.set("search", event.target.value);
                      else next.delete("search");
                      return next;
                    })
                  }
                  placeholder={searchLabel}
                  aria-label={searchLabel}
                />
              </div>
              {category === null && (
                <Button
                  variant="catalog-primary"
                  size="catalog"
                  className="shrink-0 whitespace-nowrap"
                  onClick={() =>
                    navigate(
                      `/wakers/${wakerId}/skills/new?${new URLSearchParams({ returnTo: location.pathname + location.search })}`,
                    )
                  }
                >
                  <Plus />
                  {t("plugin:addSkill")}
                </Button>
              )}
            </>
          }
        />
      </ResourcePageSurface>
    </WakerDetailLayout>
  );
}
