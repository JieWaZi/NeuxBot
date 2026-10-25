import { createContext, useContext, useMemo, type ReactNode } from "react";
import { createStateStore, type Spec } from "@json-render/core";
import { defineRegistry, Renderer, JSONUIProvider } from "@json-render/react";
import { inputCatalog, type PageDefinition } from "./catalog";
import { catalogDigests } from "./catalog-digests.generated";
import { useTranslation } from "react-i18next";

const Fields = createContext<(path: string) => ReactNode>(() => null);
const { registry } = defineRegistry(inputCatalog, {
  components: {
    Stack: ({ props, children }) => (
      <div
        className={`${props.direction === "horizontal" ? "flex flex-row flex-wrap" : "grid"} min-w-0 ${props.gap === "small" ? "gap-2" : props.gap === "large" ? "gap-6" : "gap-4"}`}
      >
        {children}
      </div>
    ),
    Grid: ({ props, children }) => (
      <div
        className={`grid min-w-0 gap-4 ${props.columns === 4 ? "@md:grid-cols-4" : props.columns === 3 ? "@md:grid-cols-3" : props.columns === 2 ? "@md:grid-cols-2" : "grid-cols-1"}`}
      >
        {children}
      </div>
    ),
    Section: ({ props, children }) => (
      <section className="grid min-w-0 gap-4">
        {props.title ? (
          <h2 className="type-label font-medium">{String(props.title)}</h2>
        ) : null}
        {props.description ? (
          <p className="text-muted-foreground type-caption">
            {String(props.description)}
          </p>
        ) : null}
        {children}
      </section>
    ),
    Field: function SchemaOwnedField({ props }) {
      const render = useContext(Fields);
      return render(props.path);
    },
    Text: ({ props }) => (
      <p className="text-muted-foreground type-body whitespace-pre-wrap">
        {typeof props.value === "string"
          ? props.value
          : JSON.stringify(props.value)}
      </p>
    ),
  },
});

/** 默认布局与创作布局共用官方 Renderer；输入控件由 SchemaForm 的字段适配器提供。 */
export function InputRenderer({
  spec,
  page,
  value,
  renderField,
}: {
  spec: Spec;
  page?: PageDefinition;
  value: unknown;
  renderField: (path: string) => ReactNode;
}) {
  const { t } = useTranslation("workflow");
  const store = useMemo(() => createStateStore({ args: value }), [value]);
  const supported =
    !page ||
    (page.formatVersion === 1 &&
      page.catalogVersion === "workflow-input-v1" &&
      page.catalogDigest === catalogDigests["workflow-input-v1"]);
  return (
    <Fields.Provider value={renderField}>
      <JSONUIProvider store={store} registry={registry}>
        <div className="@container">
          {!supported ? (
            <p role="alert" className="text-muted-foreground type-caption mb-4">
              {t("resultPage.inputLayoutUnavailable")}
            </p>
          ) : null}
          <Renderer
            spec={(supported ? page?.spec : undefined) ?? spec}
            registry={registry}
          />
        </div>
      </JSONUIProvider>
    </Fields.Provider>
  );
}
