export const meta = {
  sdk: "ally.workflow",
  title: "选择候选并分析",
  description: "脚本提供选项，用户决定，伙伴分析并校验",
  inputSchema: {
    type: "object",
    properties: { project: { type: "string", title: "项目名称" } },
  },
  outputSchema: {
    type: "object",
    properties: {
      analysisText: { type: "string" },
      valid: { type: "boolean" },
    },
    required: ["analysisText", "valid"],
    additionalProperties: false,
  },
  phases: [{ id: "work", title: "分析候选", detail: "查询、选择、分析与校验" }],
  actions: [
    {
      id: "list",
      type: "script",
      scriptId: "candidates",
      label: "获取候选",
      risk: "read",
      inputSchema: {
        type: "object",
        properties: { project: { type: "string" } },
        required: ["project"],
      },
      outputSchema: {
        type: "object",
        properties: {
          items: {
            type: "array",
            items: {
              type: "object",
              properties: {
                value: { type: "string" },
                label: { type: "string" },
              },
              required: ["value", "label"],
              additionalProperties: false,
            },
          },
        },
        required: ["items"],
      },
    },
    {
      id: "verify",
      type: "script",
      scriptId: "verify",
      label: "校验分析",
      risk: "read",
      inputSchema: {
        type: "object",
        properties: { text: { type: "string" }, selected: { type: "string" } },
        required: ["text", "selected"],
      },
      outputSchema: {
        type: "object",
        properties: { valid: { type: "boolean" } },
        required: ["valid"],
      },
    },
  ],
};

export const scripts = {
  candidates: {
    language: "python",
    protocol: "json",
    code: String.raw`import json, sys
x = json.load(sys.stdin)
json.dump({"items":[{"value":"a","label":x["project"]+"候选甲"},
                    {"value":"b","label":x["project"]+"候选乙"}]}, sys.stdout)
`,
  },
  verify: {
    language: "python",
    protocol: "json",
    code: String.raw`import json, sys
x = json.load(sys.stdin)
json.dump({"valid":bool(x["text"].strip()) and x["selected"] in x["text"]}, sys.stdout)
`,
  },
};

phase("work");
let project = args.project;
if (!project) {
  const answer = await askUser("请输入项目名称", {
    id: "project",
    label: "补充项目",
    phase: "work",
    kind: "text",
  });
  if (answer.status !== "answered") throw new Error("缺少项目名称");
  project = answer.value;
}
const listed = await action("list", {
  id: "list-options",
  label: "获取候选",
  phase: "work",
  input: { project },
});
const selected = await askUser("请选择候选", {
  id: "choose",
  label: "选择候选",
  phase: "work",
  kind: "select",
  choices: listed.data.items,
  allowCustom: false,
  allowSkip: false,
});
if (selected.status !== "answered") throw new Error("未选择候选");
const analysis = await worker(
  "分析项目 " + project + " 的候选 " + selected.value,
  {
    id: "analyze",
    label: "分析候选",
    phase: "work",
    resolve: { kind: "waker", wakerId: "waker-example" },
    schema: {
      type: "object",
      properties: { analysisText: { type: "string" } },
      required: ["analysisText"],
      additionalProperties: false,
    },
  },
);
const verified = await action("verify", {
  id: "verify-analysis",
  label: "校验分析",
  phase: "work",
  input: { text: analysis.analysisText, selected: selected.value },
  successCondition: { path: "/valid", equals: true },
});
return { analysisText: analysis.analysisText, valid: verified.data.valid };
