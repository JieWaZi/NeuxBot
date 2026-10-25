import {
  defaultRehypePlugins,
  Streamdown,
  type StreamdownProps,
} from "streamdown";
import { openExternalURL } from "@/shared/platform";

/** 静态知识正文复用 Markdown 引擎；禁用原始 HTML 与远程图片自动加载。 */
export function StaticMarkdown({
  source,
  onLink,
  remarkPlugins,
}: {
  source: string;
  onLink: (href: string) => void;
  remarkPlugins?: StreamdownProps["remarkPlugins"];
}) {
  return (
    <Streamdown
      mode="static"
      remarkPlugins={remarkPlugins}
      skipHtml
      // Wiki 相对地址由宿主处理；保留净化，避免默认绝对地址校验阻断内部链接。
      rehypePlugins={Object.entries(defaultRehypePlugins)
        .filter(([key]) => key === "sanitize")
        .map(([, plugin]) => plugin)}
      controls={false}
      linkSafety={{ enabled: false }}
      className="type-body [&_h1]:type-page-title [&_h2]:type-title [&_h3]:type-label min-w-0 space-y-5 break-words [&_h1]:mt-8 [&_h2]:mt-7 [&_h3]:mt-6 [&_li]:my-1 [&_pre]:overflow-x-auto [&_table]:block [&_table]:max-w-full [&_table]:overflow-x-auto"
      components={{
        a: ({ href, children }) => (
          <a
            href={href}
            className="text-link hover:text-link-hover focus-visible:ring-ring decoration-link/50 rounded-sm underline underline-offset-4 outline-none hover:decoration-current focus-visible:ring-2"
            onClick={(event) => {
              if (!href) return;
              event.preventDefault();
              if (/^(https?:|mailto:)/i.test(href)) void openExternalURL(href);
              else onLink(href);
            }}
          >
            {children}
          </a>
        ),
        img: ({ alt }) => <span>{alt}</span>,
      }}
    >
      {source}
    </Streamdown>
  );
}
