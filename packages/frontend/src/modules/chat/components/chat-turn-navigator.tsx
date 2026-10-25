import { useAuiState, type ThreadMessage } from "@assistant-ui/react";
import { useEffect, useMemo, useRef, useState, type RefObject } from "react";
import type {
  KeyboardEvent as ReactKeyboardEvent,
  MouseEvent as ReactMouseEvent,
} from "react";
import { useTranslation } from "react-i18next";

import { readingTurnIndex } from "./chat-turn-geometry";
import { cn } from "@/shared/lib/cn";

const TURN_MARKER_HEIGHT = 12;

type ChatTurn = {
  assistantText: string;
  id: string;
  userText: string;
};

function previewText(message?: ThreadMessage) {
  if (!message) return "";
  return message.content
    .filter((part) => part.type === "text")
    .map((part) => part.text)
    .join("\n")
    .replace(/\s+/g, " ")
    .trim();
}

function chatTurns(messages: readonly ThreadMessage[]) {
  const turns: ChatTurn[] = [];
  for (let index = 0; index < messages.length; index += 1) {
    const message = messages[index];
    if (!message || message.role !== "user") continue;
    const nextMessage = messages[index + 1];
    turns.push({
      id: message.id,
      userText: previewText(message),
      assistantText:
        nextMessage?.role === "assistant" ? previewText(nextMessage) : "",
    });
  }
  return turns;
}

function findMessageElement(viewport: HTMLElement, messageID: string) {
  return Array.from(
    viewport.querySelectorAll<HTMLElement>("[data-message-id]"),
  ).find((element) => element.dataset.messageId === messageID);
}

function ChatTurnNavigator({
  viewportRef,
}: {
  viewportRef: RefObject<HTMLDivElement | null>;
}) {
  const { t } = useTranslation("chat");
  const messages = useAuiState((state) => state.thread.messages);
  const turns = useMemo(() => chatTurns(messages), [messages]);
  const turnIdsKey = turns.map((turn) => encodeURIComponent(turn.id)).join("|");
  const navigatorRef = useRef<HTMLDivElement>(null);
  const [activeIndex, setActiveIndex] = useState(0);
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
  const [previewTop, setPreviewTop] = useState<number | null>(null);
  const [isScrollable, setIsScrollable] = useState(false);

  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const turnIds = turnIdsKey
      ? turnIdsKey.split("|").map(decodeURIComponent)
      : [];
    let frame = 0;
    const update = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        setIsScrollable(
          turnIds.length > 1 &&
            viewport.scrollHeight > viewport.clientHeight + 1,
        );
        if (turnIds.length) setActiveIndex(readingTurnIndex(viewport, turnIds));
      });
    };

    update();
    viewport.addEventListener("scroll", update, { passive: true });
    const resizeObserver = new ResizeObserver(update);
    resizeObserver.observe(viewport);
    const messageList = viewport.firstElementChild;
    if (messageList instanceof HTMLElement) resizeObserver.observe(messageList);
    return () => {
      viewport.removeEventListener("scroll", update);
      resizeObserver.disconnect();
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, [turnIdsKey, viewportRef]);

  useEffect(() => {
    const navigator = navigatorRef.current;
    const marker = navigator?.querySelector<HTMLElement>(
      `[data-chat-turn-index="${activeIndex}"]`,
    );
    if (!navigator || !marker) return;
    const markerTop = marker.offsetTop;
    const markerBottom = markerTop + marker.offsetHeight;
    if (markerTop < navigator.scrollTop) {
      navigator.scrollTop = markerTop;
    } else if (markerBottom > navigator.scrollTop + navigator.clientHeight) {
      navigator.scrollTop = markerBottom - navigator.clientHeight;
    }
  }, [activeIndex, turns.length]);

  const indexAtPointer = (event: ReactMouseEvent<HTMLDivElement>) => {
    const navigator = event.currentTarget;
    const bounds = navigator.getBoundingClientRect();
    if (bounds.height <= 0 || turns.length === 0) return 0;
    const contentHeight = Math.max(
      navigator.scrollHeight,
      turns.length * TURN_MARKER_HEIGHT,
    );
    const relativeY = Math.max(
      0,
      Math.min(
        contentHeight - 1,
        event.clientY - bounds.top + navigator.scrollTop,
      ),
    );
    return Math.min(
      turns.length - 1,
      Math.floor(relativeY / TURN_MARKER_HEIGHT),
    );
  };
  const previewPositionFor = (event: ReactMouseEvent<HTMLDivElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect();
    const relativeY = event.clientY - bounds.top;
    const edge = Math.min(48, bounds.height / 2);
    return Math.max(edge, Math.min(bounds.height - edge, relativeY));
  };
  const updateHover = (event: ReactMouseEvent<HTMLDivElement>) => {
    setHoveredIndex(indexAtPointer(event));
    setPreviewTop(previewPositionFor(event));
  };
  const jumpToTurn = (index: number) => {
    const viewport = viewportRef.current;
    const turn = turns[index];
    if (!viewport || !turn) return;
    findMessageElement(viewport, turn.id)?.scrollIntoView({
      behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches
        ? "instant"
        : "smooth",
      block: "start",
    });
    setActiveIndex(index);
    setHoveredIndex(index);
  };
  const handleKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
    event.preventDefault();
    const direction = event.key === "ArrowUp" ? -1 : 1;
    jumpToTurn(
      Math.max(0, Math.min(turns.length - 1, activeIndex + direction)),
    );
  };

  if (!isScrollable) return null;
  const previewTurn = hoveredIndex === null ? undefined : turns[hoveredIndex];

  return (
    <div className="pointer-events-none absolute inset-y-4 left-2 z-20 flex items-center">
      <div className="pointer-events-auto relative h-fit max-h-full">
        <div
          ref={navigatorRef}
          data-chat-turn-navigator
          role="slider"
          tabIndex={0}
          aria-label={t("turnNavigator")}
          aria-orientation="vertical"
          aria-valuemin={1}
          aria-valuemax={turns.length}
          aria-valuenow={activeIndex + 1}
          aria-valuetext={t("turnNavigatorPosition", {
            current: activeIndex + 1,
            total: turns.length,
          })}
          className="focus-visible:ring-ring scrollbar-hidden w-control-sm max-h-80 cursor-pointer overflow-y-auto outline-none focus-visible:ring-2 focus-visible:ring-offset-2"
          onClick={(event) => jumpToTurn(indexAtPointer(event))}
          onKeyDown={handleKeyDown}
          onMouseLeave={() => {
            setHoveredIndex(null);
            setPreviewTop(null);
          }}
          onMouseMove={updateHover}
        >
          {turns.map((turn, index) => (
            <span
              key={turn.id}
              aria-hidden="true"
              data-chat-turn-index={index}
              data-chat-turn-marker
              className="flex h-3 w-full items-center"
            >
              <i
                className={cn(
                  "bg-border-strong block h-px w-2 transition-[width,background-color]",
                  index === activeIndex && "bg-foreground w-6",
                  index === hoveredIndex && "bg-foreground w-4",
                )}
              />
            </span>
          ))}
        </div>
        {previewTurn && previewTop !== null && (
          <div
            role="tooltip"
            className="border-border bg-surface-elevated pointer-events-none absolute left-8 w-[320px] -translate-y-1/2 rounded-lg border p-3 shadow-sm"
            style={{ top: previewTop }}
          >
            <p className="type-body line-clamp-2 font-medium">
              {previewTurn.userText || t("attachmentOnlyMessage")}
            </p>
            {previewTurn.assistantText && (
              <p className="border-border text-muted-foreground type-body mt-2 line-clamp-2 border-t pt-2">
                {previewTurn.assistantText}
              </p>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

export { ChatTurnNavigator };
