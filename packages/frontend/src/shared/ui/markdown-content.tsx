import {
  lazy,
  Suspense,
  useState,
  type ReactNode,
  type ComponentPropsWithoutRef,
  type ComponentProps,
} from "react";
import { useTranslation } from "react-i18next";
import { openExternalURL } from "@/shared/platform";

const Streamdown = lazy(() =>
  import("streamdown").then((module) => {
    const rehypePlugins = Object.entries(module.defaultRehypePlugins)
      .filter(([name]) => name !== "harden")
      .map(([, plugin]) => plugin);
    // 保留 HTML 净化；相对引用交给只读 Link 展示为文字，避免附加 blocked 提示。
    return {
      default: (props: ComponentProps<typeof module.Streamdown>) => (
        <module.Streamdown {...props} rehypePlugins={rehypePlugins} />
      ),
    };
  }),
);

/** 资源说明与技能正文共用只读 Markdown；不加载包内或远程图片，不赋予本机文件访问。 */
export function MarkdownContent({ content }: { content: string }) {
  const { t } = useTranslation("plugin");
  const [failed, setFailed] = useState(false);
  function PublicLink({
    href,
    children,
  }: ComponentPropsWithoutRef<"a"> | Record<string, unknown>) {
    const label = children as ReactNode;
    if (typeof href !== "string" || !/^https?:\/\//.test(href))
      return <span>{label}</span>;
    return (
      <a
        href={href}
        className="text-primary underline underline-offset-4"
        onClick={(event) => {
          event.preventDefault();
          setFailed(false);
          void openExternalURL(href).catch(() => setFailed(true));
        }}
      >
        {label}
      </a>
    );
  }
  return (
    <div className="type-body [&_h1]:type-page-title [&_h2]:type-title [&_h3]:type-label min-w-0 space-y-3 break-words [&_code]:font-mono [&_h1]:font-semibold [&_h2]:font-semibold [&_h3]:font-medium [&_pre]:overflow-auto [&_pre]:font-mono [&_table]:block [&_table]:overflow-auto">
      <Suspense fallback={<p className="whitespace-pre-wrap">{content}</p>}>
        <Streamdown
          mode="static"
          controls={false}
          components={{
            a: PublicLink,
            img: ({ alt }) => (
              <span className="text-content-caption">{alt}</span>
            ),
          }}
        >
          {content}
        </Streamdown>
      </Suspense>
      {failed && (
        <p role="alert" className="text-destructive">
          {t("failed")}
        </p>
      )}
    </div>
  );
}
