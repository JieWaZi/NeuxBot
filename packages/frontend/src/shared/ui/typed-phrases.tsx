import { useEffect, useState, type ElementType } from "react";

import { cn } from "@/shared/lib/cn";

const typeDelayMs = 100;
const deleteDelayMs = 55;
const phraseHoldMs = 2200;
const phraseGapMs = 450;
const accessiblePhraseIndex = 0;

function prefersReducedMotion() {
  return (
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false
  );
}

export function TypedPhrases({
  as: Tag = "p",
  className,
  loop = true,
  phrases,
  wrap = false,
}: {
  as?: Extract<ElementType, "h1" | "p">;
  className?: string;
  loop?: boolean;
  phrases: readonly string[];
  wrap?: boolean;
}) {
  const [text, setText] = useState("");
  const [reducedMotion, setReducedMotion] = useState(prefersReducedMotion);
  const first = phrases[accessiblePhraseIndex] ?? "";
  const stacked = phrases.join("\n");
  const accessible = first.replaceAll("\n", "");
  const phraseClass = wrap
    ? "col-start-1 row-start-1 whitespace-pre-line"
    : "col-start-1 row-start-1 pr-1 whitespace-nowrap";

  useEffect(() => {
    const media = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    const handleMotionChange = () => setReducedMotion(prefersReducedMotion());
    media?.addEventListener("change", handleMotionChange);
    return () => media?.removeEventListener("change", handleMotionChange);
  }, []);

  useEffect(() => {
    if (reducedMotion || phrases.length === 0) return;

    const characters = phrases.map((phrase) => Array.from(phrase));
    let phraseIndex = 0;
    let characterCount = 0;
    let deleting = false;
    let done = false;
    let completed = "";
    let nextDelay = phraseGapMs;
    let timer: number | undefined;

    const visible = () => {
      const phrase = characters[phraseIndex] ?? [];
      return completed + phrase.slice(0, characterCount).join("");
    };
    const schedule = () => {
      if (!done && !document.hidden) timer = window.setTimeout(tick, nextDelay);
    };
    function tick() {
      if (done) return;
      const phrase = characters[phraseIndex] ?? [];
      characterCount += deleting ? -1 : 1;
      setText(visible());

      if (!deleting && characterCount === phrase.length) {
        if (!loop && phraseIndex < characters.length - 1) {
          completed += `${phrase.join("")}\n`;
          phraseIndex += 1;
          characterCount = 0;
          setText(completed);
          nextDelay = phraseGapMs;
        } else if (!loop) {
          done = true;
          return;
        } else {
          deleting = true;
          nextDelay = phraseHoldMs;
        }
      } else if (deleting && characterCount === 0) {
        deleting = false;
        phraseIndex = (phraseIndex + 1) % characters.length;
        nextDelay = phraseGapMs;
      } else {
        nextDelay = deleting ? deleteDelayMs : typeDelayMs;
      }
      schedule();
    }
    const handleVisibilityChange = () => {
      window.clearTimeout(timer);
      schedule();
    };

    schedule();
    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => {
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, [loop, phrases, reducedMotion]);

  if (phrases.length === 0) return null;

  return (
    <Tag aria-label={accessible} className={cn("grid min-w-0", className)}>
      {wrap ? (
        <span aria-hidden="true" className={cn("invisible", phraseClass)}>
          {stacked}
        </span>
      ) : (
        phrases.map((phrase) => (
          <span
            aria-hidden="true"
            className={cn("invisible", phraseClass)}
            key={phrase}
          >
            {phrase}
          </span>
        ))
      )}
      <span aria-hidden="true" className={phraseClass} data-typed-phrase="">
        {reducedMotion ? (wrap ? stacked : first) : text}
        <span className="bg-content-primary ml-0.5 inline-block h-5 w-px align-[-0.1em] motion-safe:animate-pulse" />
      </span>
    </Tag>
  );
}
