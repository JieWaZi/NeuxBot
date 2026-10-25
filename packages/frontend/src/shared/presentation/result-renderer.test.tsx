import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import "@/shared/i18n";
import type { PageDefinition } from "./catalog";
import { ResultRenderer } from "./result-renderer";

const Markdown = ({ source }: { source: string }) => <p>{source}</p>;
function show(page: PageDefinition, result: unknown) {
  return render(
    <ResultRenderer
      page={page}
      result={result}
      Markdown={Markdown}
      download={() => {}}
    />,
  );
}
describe("official json-render integration", () => {
  it("renders an agent-composed page with semantic hierarchy and optional details, without appending internal files", () => {
    const page: PageDefinition = {
      formatVersion: 1,
      catalogVersion: "workflow-result-v1",
      spec: {
        root: "page",
        elements: {
          page: {
            type: "Page",
            props: { width: "wide", align: "center", gap: "large" },
            children: ["heading", "summary", "cards", "details"],
          },
          heading: {
            type: "Heading",
            props: { value: "本次产出", level: "h1" },
          },
          summary: {
            type: "Callout",
            props: {
              title: "结论",
              value: { $state: "/result/summary" },
              tone: "accent",
            },
          },
          cards: {
            type: "Collection",
            props: {
              items: { $state: "/result/items" },
              layout: "grid",
              columns: 2,
            },
            children: ["card"],
          },
          card: { type: "Card", props: {}, children: ["item-title", "source"] },
          "item-title": {
            type: "Heading",
            props: { value: { $item: "title" }, level: "h3" },
          },
          source: {
            type: "Link",
            props: { label: "查看来源", href: { $item: "url" } },
          },
          details: {
            type: "Disclosure",
            props: { title: "辅助说明" },
            children: ["evidence"],
          },
          evidence: { type: "Text", props: { value: "完整证据" } },
        },
      },
    };
    render(
      <ResultRenderer
        page={page}
        result={{
          summary: "建议先阅读重点",
          items: [{ title: "第一项", url: "https://example.com/first" }],
        }}
        Markdown={Markdown}
        files={[
          {
            id: "cache",
            name: "internal.json",
            field: "/cache",
            available: true,
          },
        ]}
        filePaths={{ "/cache": "cache.json" }}
        download={() => {}}
      />,
    );
    expect(screen.getByRole("heading", { level: 1 })).toHaveClass(
      "type-page-title",
    );
    expect(
      screen.getByText("建议先阅读重点").closest("[data-presentation=callout]"),
    ).toHaveClass("bg-accent-surface");
    expect(screen.getByRole("heading", { level: 3 })).toHaveTextContent(
      "第一项",
    );
    expect(screen.getByRole("link", { name: /查看来源/ })).toHaveAttribute(
      "href",
      "https://example.com/first",
    );
    expect(screen.queryByText("internal.json")).not.toBeInTheDocument();
    expect(screen.queryByText("完整证据")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "辅助说明" }));
    expect(screen.getByText("完整证据")).toBeInTheDocument();
  });
  it("paginates nested item scopes while retaining full global state and absolute indices", () => {
    const page: PageDefinition = {
      formatVersion: 1,
      catalogVersion: "workflow-result-v1",
      spec: {
        root: "list",
        elements: {
          list: {
            type: "Collection",
            props: { items: { $state: "/result/items" }, pageSize: 1 },
            children: ["title", "index", "global", "nested"],
          },
          title: { type: "Text", props: { value: { $item: "title" } } },
          index: { type: "Text", props: { value: { $index: true } } },
          global: {
            type: "Text",
            props: { value: { $state: "/result/total" } },
          },
          nested: {
            type: "Collection",
            props: { items: { $item: "children" }, pageSize: 1 },
            children: ["child"],
          },
          child: { type: "Text", props: { value: { $item: "name" } } },
        },
      },
    };
    const items = Array.from({ length: 10000 }, (_, index) => ({
      title: `row-${index}`,
      children: [
        { name: `child-${index}-first` },
        { name: `child-${index}-second` },
      ],
    }));
    show(page, { items, total: 10000 });
    expect(screen.getByText("row-0")).toBeInTheDocument();
    expect(screen.getByText("child-0-first")).toBeInTheDocument();
    expect(screen.queryByText("row-9999")).not.toBeInTheDocument();
    fireEvent.click(screen.getAllByText("下一页")[0] ?? document.body);
    expect(screen.getByText("child-0-second")).toBeInTheDocument();
    fireEvent.click(screen.getAllByText("下一页")[1] ?? document.body);
    expect(screen.getByText("row-1")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
    expect(screen.getByText("10000")).toBeInTheDocument();
  });
  it("uses native conditional props and visibility and preserves false, zero and null", () => {
    show(
      {
        formatVersion: 1,
        catalogVersion: "workflow-result-v1",
        spec: {
          root: "stack",
          elements: {
            stack: {
              type: "Stack",
              props: {},
              children: ["false", "zero", "null", "conditional", "hidden"],
            },
            false: {
              type: "Text",
              props: { value: { $state: "/result/flag" } },
            },
            zero: {
              type: "Text",
              props: { value: { $state: "/result/count" } },
            },
            null: {
              type: "Text",
              props: { value: { $state: "/result/nothing" } },
            },
            conditional: {
              type: "Text",
              props: {
                value: {
                  $cond: { $state: "/result/count", eq: 0 },
                  $then: "empty",
                  $else: "full",
                },
              },
            },
            hidden: {
              type: "Text",
              props: { value: "hidden" },
              visible: { $state: "/result/flag" },
            },
          },
        },
      },
      { flag: false, count: 0, nothing: null },
    );
    expect(screen.getByText("false")).toBeInTheDocument();
    expect(screen.getByText("0")).toBeInTheDocument();
    expect(screen.getByText("null")).toBeInTheDocument();
    expect(screen.getByText("empty")).toBeInTheDocument();
    expect(screen.queryByText("hidden")).not.toBeInTheDocument();
  });
});

it("supports native repeat inside a paginated collection and refuses an unbounded repeat", () => {
  const page: PageDefinition = {
    formatVersion: 1,
    catalogVersion: "workflow-result-v1",
    spec: {
      root: "list",
      elements: {
        list: {
          type: "Collection",
          props: { items: { $state: "/result/items" }, pageSize: 1 },
          children: ["repeat"],
        },
        repeat: {
          type: "Stack",
          props: {},
          repeat: { statePath: { $item: "children" } },
          children: ["name"],
        },
        name: { type: "Text", props: { value: { $item: "name" } } },
      },
    },
  };
  const result = {
    items: [{ children: [{ name: "first" }, { name: "second" }] }],
  };
  const view = show(page, result);
  expect(screen.getByText("first")).toBeInTheDocument();
  expect(screen.getByText("second")).toBeInTheDocument();
  view.rerender(
    <ResultRenderer
      page={page}
      result={{
        items: [
          { children: Array.from({ length: 51 }, () => ({ name: "large" })) },
        ],
      }}
      Markdown={Markdown}
      download={() => {}}
    />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent("超出展示上限");
  expect(screen.queryByText("large")).not.toBeInTheDocument();
});

it("shares the same frozen data across hosts without sharing reading state", () => {
  const page: PageDefinition = {
    formatVersion: 1,
    catalogVersion: "workflow-result-v1",
    spec: {
      root: "list",
      elements: {
        list: {
          type: "Collection",
          props: { items: { $state: "/result/items" }, pageSize: 1 },
          children: ["text"],
        },
        text: { type: "Text", props: { value: { $item: "title" } } },
      },
    },
  };
  const result = { items: [{ title: "第一项" }, { title: "第二项" }] };
  render(
    <>
      <div data-testid="tab">
        <ResultRenderer
          page={page}
          result={result}
          Markdown={Markdown}
          download={() => {}}
        />
      </div>
      <div data-testid="message">
        <ResultRenderer
          page={page}
          result={result}
          compact
          Markdown={Markdown}
          download={() => {}}
        />
      </div>
    </>,
  );
  fireEvent.click(within(screen.getByTestId("message")).getByText("下一页"));
  expect(
    within(screen.getByTestId("tab")).getByText("第一项"),
  ).toBeInTheDocument();
  expect(
    within(screen.getByTestId("message")).getByText("第二项"),
  ).toBeInTheDocument();
});
