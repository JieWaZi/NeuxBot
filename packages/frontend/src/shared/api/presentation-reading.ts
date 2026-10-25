import { useEffect, useRef, type RefObject } from "react";
import { observeContentPresentation } from "@/shared/lib/content-presentation";

/** 阅读统一按呈现身份去重；请求失败保留未读，在重新呈现或权威查询恢复后再尝试。 */
export function usePresentationReading({
  root,
  selector,
  identity,
  revision,
  read,
}: {
  root: RefObject<HTMLElement | null>;
  selector?: string;
  identity?: string;
  revision?: number;
  read: () => Promise<unknown>;
}) {
  const submit = useRef({ identity, read });
  useEffect(() => {
    submit.current = { identity, read };
  }, [identity, read]);
  const sent = useRef("");
  useEffect(() => {
    const element = root.current;
    if (!identity || !element) return;
    return observeContentPresentation({
      root: element,
      selector,
      onVisible: () => {
        if (submit.current.identity !== identity || sent.current === identity)
          return;
        sent.current = identity;
        void submit.current.read().catch(() => {
          // 旧请求晚到的失败不能抹掉新版本的提交身份；重渲染本身不形成请求循环。
          if (sent.current === identity) sent.current = "";
        });
      },
    });
  }, [root, selector, identity, revision]);
}
