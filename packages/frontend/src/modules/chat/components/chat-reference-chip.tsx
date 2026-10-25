import type { ReactNode } from "react";
import { Cable, Sparkles, Package } from "lucide-react";
import { Link } from "react-router-dom";

export const chatComposerReferenceChipClass =
  "bg-action-quiet-hover text-content-primary type-body inline-flex max-w-full shrink-0 items-center gap-1 rounded-full px-2.5 py-0.5 font-medium";

export const chatMessageReferenceChipClass =
  "bg-border/60 text-foreground type-body inline-flex max-w-full shrink-0 items-center gap-1 rounded-md px-2 py-0.5 font-medium";

export const chatMessageConnectorReferenceChipClass =
  "bg-success/20 text-foreground type-body inline-flex max-w-full shrink-0 items-center gap-1 rounded-md px-2 py-0.5 font-medium";

type ChatComposerReferenceChipProps = {
  label: string;
  marker?: { name: string; value: string };
};

export function ChatComposerReferenceChip({
  label,
  marker,
}: ChatComposerReferenceChipProps) {
  return (
    <span
      {...(marker ? { [marker.name]: marker.value } : {})}
      className={chatComposerReferenceChipClass}
    >
      <Sparkles className="size-icon-xs shrink-0" aria-hidden="true" />
      <span className="max-w-40 truncate">{label}</span>
    </span>
  );
}

type ChatMessageReferenceChipProps = {
  label: string;
  tone?: "default" | "connector" | "plugin";
  href?: string;
  unavailable?: boolean;
  unavailableTitle?: string;
  linkAriaLabel?: string;
};

export function ChatMessageReferenceChip({
  label,
  tone = "default",
  href,
  unavailable = false,
  unavailableTitle,
  linkAriaLabel,
}: ChatMessageReferenceChipProps) {
  const className =
    tone === "connector"
      ? chatMessageConnectorReferenceChipClass
      : chatMessageReferenceChipClass;
  const Icon =
    tone === "connector" ? Cable : tone === "plugin" ? Package : Sparkles;
  const content = (
    <>
      <Icon className="size-icon-xs shrink-0" aria-hidden="true" />
      <span className="truncate">{label}</span>
    </>
  );

  if (unavailable || !href) {
    return (
      <span
        className={`${className}${unavailable ? "opacity-70" : ""}`}
        title={unavailableTitle}
      >
        {content}
      </span>
    );
  }

  return (
    <Link
      to={href}
      aria-label={linkAriaLabel}
      className={`${className} ${
        tone === "connector" ? "hover:bg-success/30" : "hover:bg-border/80"
      }`}
    >
      {content}
    </Link>
  );
}

export function ChatReferenceChipList({ children }: { children: ReactNode }) {
  return <>{children}</>;
}
