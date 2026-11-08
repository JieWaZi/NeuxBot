import type { ReactNode } from "react";
import { ArrowRight } from "lucide-react";
import { Button } from "./button";
import { ResourceGradient } from "./resource-gradient";

/** 源端 TryCard：品牌引用嵌于提示词，箭头只准备草稿。 */
export function ResourceCatalogTry({
  seed,
  name,
  logo,
  prompts,
  onTry,
  caption,
}: {
  seed: string;
  name: string;
  logo: ReactNode;
  prompts: readonly string[];
  onTry?: (text: string) => void;
  caption?: string;
}) {
  const card = (
    <div className="catalog-try-card">
      <ResourceGradient seed={seed} />
      {prompts.map((text, index) => (
        <div className="catalog-try-request" key={index}>
          <p>
            <span className="catalog-try-chip" title={name}>
              <span className="catalog-try-chip-icon" aria-hidden="true">
                {logo}
              </span>
              <span className="catalog-try-chip-name">{name}</span>
            </span>{" "}
            {text}
          </p>
          <Button
            className="catalog-try-arrow"
            variant="catalog-ghost"
            size="catalog-icon-lg"
            aria-label={text}
            title={caption}
            aria-description={caption}
            disabled={!onTry}
            onClick={() => onTry?.(text)}
          >
            <ArrowRight />
          </Button>
        </div>
      ))}
    </div>
  );
  return card;
}
