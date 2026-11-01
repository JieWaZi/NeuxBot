import { appendFile, writeFile } from "node:fs/promises";
import { performance as realPerformance } from "node:perf_hooks";
import {
  Profiler,
  createElement,
  type ComponentType,
  type ReactNode,
} from "react";
import { createRoot } from "react-dom/client";
import { flushSync } from "react-dom";
import { expect, it } from "vitest";
import { markdownWorkload } from "../../../scripts/performance/markdown-ab-workload.mjs";

const delay = (ms: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, ms));
const variant = process.env.NEUX_MARKDOWN_AB_VARIANT ?? "current";
const workload = markdownWorkload(
  process.env.NEUX_MARKDOWN_AB_WORKLOAD ?? "short-2kib",
);
const warmRuns = Number(process.env.NEUX_MARKDOWN_AB_WARM_RUNS ?? 3);
function visibleCode(container: HTMLElement): string {
  return [...container.querySelectorAll("pre code")]
    .map((code) => {
      const lines = [...code.children];
      // Streamdown uses display:block spans without newline text nodes; the new
      // renderer uses .line spans with newline siblings. Read both visual lines.
      return lines.length && lines.every((line) => line.tagName === "SPAN")
        ? lines.map((line) => line.textContent ?? "").join("\n")
        : (code.textContent ?? "");
    })
    .join("\n")
    .trimEnd();
}
const output = process.env.NEUX_MARKDOWN_AB_OUTPUT;
const trace = (event: object) =>
  output
    ? appendFile(
        `${output}.progress.jsonl`,
        JSON.stringify({
          time: new Date().toISOString(),
          variant,
          workload: workload.name,
          ...event,
        }) + "\n",
      )
    : Promise.resolve();

it.skipIf(!process.env.NEUX_MARKDOWN_AB_VARIANT)(
  "mounts, streams and completes the actual production Markdown component",
  async () => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: false });
    // jsdom has no layout. Model every measured code block as in view, so neither
    // implementation gets an advantage by skipping its production highlighter.
    class VisibleIntersectionObserver {
      private live = new Set<Element>();
      constructor(private callback: IntersectionObserverCallback) {}
      observe(target: Element) {
        this.live.add(target);
        queueMicrotask(() => {
          if (!this.live.has(target)) return;
          this.callback(
            [
              {
                target,
                isIntersecting: true,
                intersectionRatio: 1,
                intersectionRect: new DOMRect(0, 0, 800, 600),
                boundingClientRect: new DOMRect(0, 0, 800, 600),
                rootBounds: new DOMRect(0, 0, 800, 600),
                time: performance.now(),
              },
            ],
            this as unknown as IntersectionObserver,
          );
        });
      }
      unobserve(target: Element) {
        this.live.delete(target);
      }
      disconnect() {
        this.live.clear();
      }
      takeRecords() {
        return [];
      }
    }
    Object.assign(globalThis, {
      IntersectionObserver: VisibleIntersectionObserver,
    });
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: (query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener() {},
        removeListener() {},
        addEventListener() {},
        removeEventListener() {},
        dispatchEvent() {
          return false;
        },
      }),
    });
    const importStart = realPerformance.now();
    const importCpu = process.cpuUsage();
    await import("@/shared/i18n");
    const production =
      await import("@/modules/chat/components/chat-message-content");
    const textProvider =
      variant === "baseline"
        ? (await import("@assistant-ui/react")).TextMessagePartProvider
        : undefined;
    const componentImportWallMs = realPerformance.now() - importStart;
    const importCpuUsage = process.cpuUsage(importCpu);
    const componentImportCpuMs =
      (importCpuUsage.user + importCpuUsage.system) / 1000;
    const renderProduction = (text: string, isTerminal: boolean): ReactNode => {
      if (variant === "baseline") {
        if (!textProvider)
          throw new Error("Baseline text-part provider is unavailable");
        const AssistantText = (
          production as unknown as {
            AssistantText: ComponentType<{
              isTerminal: boolean;
              allowFileReferences: boolean;
            }>;
          }
        ).AssistantText;
        return createElement(
          textProvider,
          { text, isRunning: !isTerminal },
          createElement(AssistantText, {
            isTerminal,
            allowFileReferences: false,
          }),
        );
      }
      const AssistantMarkdown = (
        production as unknown as {
          AssistantMarkdown: ComponentType<{
            text: string;
            isTerminal: boolean;
            allowFileReferences: boolean;
          }>;
        }
      ).AssistantMarkdown;
      return createElement(AssistantMarkdown, {
        text,
        isTerminal,
        allowFileReferences: false,
      });
    };
    const trials: unknown[] = [];
    const persist = async () => {
      if (!output) return;
      await writeFile(
        output,
        JSON.stringify(
          {
            variant,
            workload: {
              name: workload.name,
              bytes: workload.bytes,
              codeLines: workload.codeLines,
              sha256: workload.sha256,
              batches: workload.batches,
              inputIntervalMs: workload.inputIntervalMs,
            },
            componentImportWallMs,
            componentImportCpuMs,
            trials,
            limitations: [
              "React development build and jsdom; no browser layout, paint, compositor or native platform",
              "All code blocks are simulated in view; actual production highlighting and table components run",
              "Fixed real-time input cadence; original smooth queue is preserved; inspect stream coverage before comparing CPU",
              "Cold means first component mount in a fresh process; module import time is separate; warm shares production grammar/token caches",
            ],
            buildMode: {
              nodeEnv: process.env.NODE_ENV,
              react:
                "19.2.8 development via Vitest; Profiler enabled; no StrictMode",
              vite: "same @vitejs/plugin-react defaults on both source graphs; no React Compiler configuration on either side (matches repository Vite defaults)",
              driver:
                "flushSync for each external source prop update; native real timers between inputs; no act drain of internal smooth/deferred work",
              setupOutsideTrial:
                "jsdom startup, DOM observer simulation and component dependency import; dependency import is recorded separately and includes Vite test transform overhead",
              setupInsideTrial:
                "React root mount, actual parsing/highlighting initialization, source updates, original smooth/deferred work, completion to fully highlighted exact code; unmount and correctness assertions excluded",
            },
          },
          null,
          2,
        ),
      );
    };
    await persist();
    for (let run = 0; run <= warmRuns; run++) {
      await trace({ run, stage: "begin" });
      const container = document.createElement("div");
      document.body.append(container);
      const root = createRoot(container);
      let commits = 0,
        renderActualMs = 0,
        renderBaseMs = 0;
      let driverRenderCalls = 0;
      let animationFrameCallbacks = 0;
      const originalRaf = globalThis.requestAnimationFrame;
      const originalWindowRaf = window.requestAnimationFrame;
      const measuredRaf = (callback: FrameRequestCallback) =>
        originalRaf.call(window, (time) => {
          animationFrameCallbacks++;
          callback(time);
        });
      Object.assign(globalThis, { requestAnimationFrame: measuredRaf });
      window.requestAnimationFrame = measuredRaf;
      let stage = "mount";
      const profilerEvents: unknown[] = [];
      const profile = (
        _id: string,
        phase: string,
        actual: number,
        base: number,
        startedAt: number,
        commitTime: number,
      ) => {
        commits++;
        renderActualMs += actual;
        renderBaseMs += base;
        profilerEvents.push({
          stage,
          phase,
          actualRenderMs: actual,
          baseRenderMs: base,
          startedAt,
          commitTime,
        });
      };
      const render = (text: string, terminal: boolean) => {
        driverRenderCalls++;
        flushSync(() =>
          root.render(
            createElement(
              Profiler,
              { id: "production-markdown", onRender: profile },
              renderProduction(text, terminal),
            ),
          ),
        );
      };
      const startCpu = process.cpuUsage();
      const start = realPerformance.now();
      render("", false);
      const mountWallMs = realPerformance.now() - start;
      const streamStart = realPerformance.now();
      stage = "stream";
      const streamCpu = process.cpuUsage();
      const beforeStreamCommits = commits;
      const sourceCodeChars = workload.code.length;
      const samples: unknown[] = [];
      const updateWallMs: number[] = [];
      for (const [index, chunk] of workload.chunks.entries()) {
        // Real timers preserve old smooth/deferred scheduling. Late work catches up
        // against a fixed input clock instead of adding its cost to every wait.
        await delay(
          Math.max(
            0,
            streamStart +
              index * workload.inputIntervalMs -
              realPerformance.now(),
          ),
        );
        const updateStart = realPerformance.now();
        render(chunk, false);
        await delay(0);
        updateWallMs.push(realPerformance.now() - updateStart);
        if ((index + 1) % 25 === 0)
          samples.push({
            inputBatch: index + 1,
            inputChars: chunk.length,
            inputClockLagMs:
              realPerformance.now() -
              streamStart -
              index * workload.inputIntervalMs,
            visibleCodeChars: visibleCode(container).length,
            finalMarkerVisible:
              container.textContent?.includes("FINAL_COMPONENT_AB_MARKER") ??
              false,
          });
        if ((index + 1) % 25 === 0)
          await trace({
            run,
            stage: "stream",
            batch: index + 1,
            inputClockLagMs:
              realPerformance.now() -
              streamStart -
              index * workload.inputIntervalMs,
          });
        if (realPerformance.now() - streamStart > 60_000)
          throw new Error("Source input exceeded 60s; see progress trace");
      }
      await delay(workload.inputIntervalMs);
      const streamEnd = realPerformance.now();
      const preCompletion = {
        visibleCodeChars: visibleCode(container).length,
        sourceCodeChars,
        ...(workload.codeLines
          ? {}
          : {
              visibleBodyTextChars: (
                container.querySelector('[data-chat-markdown="assistant"]')
                  ?.textContent ?? ""
              ).replace(/\s/g, "").length,
              expectedBodyTextChars: workload.text
                .replace(/^- /gm, "")
                .replace(/\s/g, "").length,
            }),
        finalMarkerVisible:
          container.textContent?.includes("FINAL_COMPONENT_AB_MARKER") ?? false,
        highlightedSpans: container.querySelectorAll("pre code span[style]")
          .length,
      };
      const streamCpuUsage = process.cpuUsage(streamCpu);
      const streamCommits = commits - beforeStreamCommits;
      const beforeCompleteCommits = commits;
      stage = "completion";
      const completionCpu = process.cpuUsage();
      const completionStart = realPerformance.now();
      await trace({ run, stage: "completion-start", preCompletion });
      render(workload.text, true);
      await trace({ run, stage: "completion-rendered" });
      let highlightedSpans = 0;
      // Old Shiki loads asynchronously; completion includes its actual rendering,
      // not just the plain fallback. New highlighting must pass the same check.
      while (realPerformance.now() - completionStart < 30_000) {
        highlightedSpans = container.querySelectorAll(
          "pre code span[style]",
        ).length;
        const code = visibleCode(container);
        if (
          highlightedSpans >= workload.codeLines * 2 &&
          code === workload.code
        )
          break;
        await delay(10);
      }
      const completionWallMs = realPerformance.now() - completionStart;
      const completionCpuUsage = process.cpuUsage(completionCpu);
      const totalCpuUsage = process.cpuUsage(startCpu);
      const totalWallMs = realPerformance.now() - start;
      const renderedCode = visibleCode(container);
      expect(renderedCode).toBe(workload.code);
      expect(container.querySelectorAll("table")).toHaveLength(
        workload.codeLines ? 1 : 0,
      );
      if (workload.codeLines)
        expect(
          container.querySelector('a[href="https://example.com/benchmark"]'),
        ).not.toBeNull();
      expect(container.textContent).toContain("FINAL_COMPONENT_AB_MARKER");
      if (workload.codeLines) expect(highlightedSpans).toBeGreaterThan(0);
      expect(driverRenderCalls).toBe(workload.batches + 2);
      expect(
        profilerEvents.filter(
          (event) => (event as { phase: string }).phase === "mount",
        ),
      ).toHaveLength(1);
      const stats = {
        run,
        cache: run === 0 ? "cold-first-component-mount" : "warm-remount",
        mountWallMs,
        streamWallMs: streamEnd - streamStart,
        completionWallMs,
        totalWallMs,
        streamCpuMs: (streamCpuUsage.user + streamCpuUsage.system) / 1000,
        completionCpuMs:
          (completionCpuUsage.user + completionCpuUsage.system) / 1000,
        totalCpuMs: (totalCpuUsage.user + totalCpuUsage.system) / 1000,
        commits,
        driverRenderCalls,
        rootCreateCount: 1,
        animationFrameCallbacks,
        streamCommits,
        completionCommits: commits - beforeCompleteCommits,
        profilerActualRenderMs: renderActualMs,
        profilerBaseRenderMs: renderBaseMs,
        profilerEvents,
        inputUpdateWallMs: updateWallMs,
        streamSamples: samples,
        preCompletion,
        sourceCodeChars,
        highlightedSpans,
        correctness: {
          exactCode: true,
          codeLines: workload.codeLines,
          table: workload.codeLines ? true : "not-in-plain-fixture",
          externalLink: workload.codeLines ? true : "not-in-plain-fixture",
          finalMarker: true,
          highlighting: workload.codeLines ? true : "not-in-plain-fixture",
        },
      };
      trials.push(stats);
      await persist();
      await trace({
        run,
        stage: "complete",
        totalCpuMs: stats.totalCpuMs,
        completionWallMs,
      });
      flushSync(() => root.unmount());
      Object.assign(globalThis, { requestAnimationFrame: originalRaf });
      window.requestAnimationFrame = originalWindowRaf;
      container.remove();
    }
  },
);
