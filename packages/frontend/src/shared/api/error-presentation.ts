import { i18n } from "@/shared/i18n";
import { statusErrorCatalog } from "./status-error-catalog.generated";

type TemplateNode = {
  text?: string;
  field?: string;
  if?: string;
  then?: TemplateNode[];
  else?: TemplateNode[];
};
type Definition = { code: number; en: TemplateNode[]; zh: TemplateNode[] };
const definitions: Record<string, Definition> = statusErrorCatalog;
export type ErrorParameters = Record<string, unknown>;
export type ErrorMessageSource = "public" | "localized";

function render(nodes: TemplateNode[], params: ErrorParameters): string {
  return nodes
    .map((node) => {
      if (node.text) return node.text;
      if (node.field) {
        const value = params[node.field];
        return typeof value === "string" ||
          typeof value === "number" ||
          typeof value === "boolean"
          ? String(value)
          : "";
      }
      return render(
        node.if && params[node.if] ? (node.then ?? []) : (node.else ?? []),
        params,
      );
    })
    .join("");
}

/** 稳定身份只在此处查找；数值码和名称共用服务端生成的同一目录。 */
export function applicationErrorKey(code?: string): string | undefined {
  if (!code) return;
  if (Object.hasOwn(definitions, code)) return code;
  return Object.entries(definitions).find(
    ([, definition]) => String(definition.code) === code,
  )?.[0];
}

/** 应用错误按当前语言重绘；只有公开原因可以越过本地化。缺少公开来源的正文不参与展示。 */
export function applicationErrorMessage({
  code,
  message,
  params = {},
  messageSource,
  fallback = i18n.t("common:serviceUnrecognizedError"),
}: {
  code?: string;
  message?: string;
  params?: ErrorParameters;
  messageSource?: ErrorMessageSource;
  fallback?: string;
}): string {
  const text = message?.trim();
  if (messageSource === "public" && text) return text;
  const key = applicationErrorKey(code);
  if (key) {
    const definition = definitions[key];
    if (definition)
      return render(
        i18n.resolvedLanguage?.startsWith("en") ? definition.en : definition.zh,
        params,
      );
  }
  return fallback;
}
