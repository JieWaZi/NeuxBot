# NeuxBot Workflow SDK ally.workflow/1

The source is authoritative. Use stable ASCII IDs for phases, nodes, groups and actions. Never change an existing ID just to rename its label. Meta is a pure JSON literal. Worker identities, schemas and tool policies must be statically resolvable.

The host compiles main logic to durable Go instructions, with JSON value semantics. This is a restricted DSL, not a full JavaScript VM. Assignment and captures copy values; do not write outer mutable captures from parallel tasks. Strings use Unicode code points. Regular expressions use the RE2 common subset (g/i/m/u). Date is frozen to run start. Put general JavaScript or Python into functionTask.

Function code is a complete program, not a bare function body: JavaScript must declare function main(input) { return JSON; }; Python must define def main(input): and return a JSON value. JavaScript main must be synchronous. Do not put a top-level return in the code string. Only the returned value is the Function result. Node outputSchema is optional. If supplied, use a JSON literal or a top-level literal constant; property access such as meta.outputSchema is not statically resolvable. Omit it only when node-level output validation is unnecessary.

Every user-facing inputSchema property must include a short title and a concise description in the user language. The input-parameter preview shows the title and description separately. Titles are noun phrases, ideally 2-10 Chinese characters or 1-4 words; descriptions are one short sentence, ideally at most 40 Chinese characters or 20 words. State what to provide and only the essential format, unit or missing-input behavior. Do not repeat the title, prefix text with required/optional, add long parenthetical explanations or describe the whole workflow. Express requiredness only through the schema required array and actual default values through schema default. Keep detailed business rules, branches and follow-up questions in worker instructions or workflow logic; preserve essential user-facing constraints in concise wording. For example, title: Requirements, description: Paste the current requirements.; title: Environment, description: Provide one environment and version per line.; title: Approval threshold, description: Ask for approval when remaining high-risk items reach this count., default: 1. Include only inputs needed for the user goal, and reuse these concise labels and descriptions when summarizing parameters in chat. Use stable keys for code, never as the only form label. Declare file inputs and final file outputs as {type: 'string', format: 'file', title, description}. Arrays use items with this same file schema. Input forms upload files and run admission replaces uploaded ally-file references with real absolute paths in the approved run scope. Workers must read those paths using actual tools; never treat references as file contents. Final file values must be paths to real files created inside flowContext.scope.workingDirectory; the user and automatic task API can download these files. Return actual paths rather than base64 data or made-up download URLs. Ordinary strings are never inferred to be file outputs. Schema presentation supports x-ui-control: input or textarea for strings, radio or select for scalar enum, switch or checkbox for booleans, and checkboxes for arrays whose items.enum lists the allowed values. x-ui-labels supplies user-facing choice labels in enum order. These annotations only select controls; values still follow the declared JSON Schema types and constraints. Use these same annotations in askUser schemas.

Await workers, human inputs, actions and child results. Parallel returns in input order. Pipeline advances each item immediately after its own preceding stage, then joins all items in original order. A null stage result is ordinary JSON, not a failure. Explicit throw stops the failed item.

Human defaults do not submit or approve. Approvals require a positive explicit value; skip and timeout preserve their source. Actions use separate operation permissions.

Worker chats use the same managed runtime workspace and native instruction-file loading as ordinary chats. Their runtime working directories are isolated; the approved business directory is shared through project links and additional directory authorization. Build shared file paths from flowContext.scope.workingDirectory and pass those absolute paths to worker prompts and results. Never assume two workers have the same process working directory. Use run-specific output paths, require actual tools to create and read deliverable files, and verify existence and nonempty content.

A structured worker must call allycli workflow results submit with a stable --request-id and an input JSON object whose data matches its schema. A submission is only a candidate until its Execution succeeds. Do not rely on JSON pasted into a final reply. For a root file schema, data is the actual file path. Add routing or summary fields only for real downstream consumers.

When the user requests delivery to WeChat or Feishu, discover approved chats with allycli workflow resources list --kind imTargets. Discover the native IM operation through allycli workflow resources list --kind builtinActions, tool im_delivery, through a builtin action declaring its connector ID and awaiting ctx.callTool({callId, connectorId, toolName: 'im_delivery', input: {action: 'deliveries.create', targetId, content, files}}). Fixed literal targetIds are derived as workflow resource dependencies. Dynamic targetIds must declare the approved imTargetIds on the builtin action; runtime delivery cannot use an undeclared target. Admission checks these targets before any node runs. This durable action waits for delivery acceptance; query deliveries.get to verify sent, failed or unknown status. Never invent recipients or connector IDs. Resource IDs come from allycli workflow resources list; never invent or silently replace them. Do not send messages or publish outside an explicitly authorized action.

When optimizing a failed run, read all allycli workflow evidence list pages, including run.failure, instance failures and actionCalls. Inspect nested cause, hint, sourceRange and delivery receipts including partsSent and unknown status before proposing a repair. Preserve original call identities and never repeat an already completed or uncertain external operation. Read the current revision and pending annotations before saving. Validate complete source, fix diagnostics, then allycli workflow source save using expectedRevisionId and a stable requestId. At most three repair attempts per turn. Only the tool save receipt proves the source is active.


Declare user-provided values in meta.inputSchema and consume them through args. Give every input a concise title and description in the user language. Keep secrets and placeholders out of defaults. Declare file references with type:string, format:file; a reference is not file content.

Python requires a configured interpreter and executionMode:trusted_local. JavaScript supports pure execution. Authoring prepare includes functionLanguages when language is python.

JavaScript/Python functionTask defaults to 600000 ms (10 minutes), with the same hard maximum. Omit timeoutMs unless the user agreed to a shorter deadline. The entire run has a separate 24-hour host deadline; do not invent a shorter overall budget.

Workers have no independent timeout; worker timeoutMs is unsupported. User cancellation and actual execution failure remain effective.

HTTP actions default to 30000 ms. Command/script actions default to 600000 ms, with the same hard maximum. Omit timeoutMs unless the user agreed to a shorter deadline.

source save compiles and validates complete source, checks policies and expectedRevisionId, then commits an immutable revision and its receipt together. Use --source for the UTF-8 file and a stable --request-id. A successful immediate receipt with saved:true, revisionId and sourceDigest proves saving; unchanged:true means the current revision already has that exact source. No source get is needed after success. If the response is lost, query receipts get for the original operation/request ID. Read current source again on revision conflict. Saving does not prove a real run or external delivery succeeded.

Run budgets have different meanings: limits.concurrency caps simultaneous external calls, while limits.maxInstances caps the cumulative number of node instances in the entire run, including actions, functionTask, workers, human requests and child-workflow calls. Every loop iteration or batch task consumes additional instances; phase and log do not. Serial execution uses concurrency:1, never maxInstances:1 for a multi-step workflow. Unless the user requested a smaller total budget, omit maxInstances to use the bounded host default. If declaring it, count all required steps and allow for loops, batches and branches. The compiler rejects budgets below a provable minimum path, but dynamic paths may require more.

Every worker, including workers inside loops, branches and parallel groups, requires exactly one output declaration: meta.nodeResultPages[id] for composed in-app reading, meta.nodeResultFiles[id] with a readable title for actual file delivery, or meta.nodeResultPageOmissions[id] with a concrete reason for internal technical/control data without reading value or output the user explicitly declined to display. Intermediate output is not automatically exempt. File delivery uses static type:string, format:file schemas, or arrays/objects of these references, and the existing file viewer. Keep the report's prose, evidence and coverage in the actual file. Select the presentation feature only for composed pages; preparation includes its Catalog. Reuse that content and query the presentation topic only if it is missing. Revisit the declaration when the user changes the deliverable.

# Workflow SDK: presentation

Evaluate every worker output, including intermediate steps, and choose exactly one presentation declaration. For requested file deliverables (HTML, Markdown, PDF, Office, images, source files or multiple files), declare meta.nodeResultFiles[stableWorkerId]:{title,description?}. It requires a static worker schema with format:"file" on the actual file values; a single file may use a root string schema, several files an array of file strings, or named file fields when downstream consumers need them. The host derives the file inventory from successful results. All file cards, including images, open its existing file viewer in a closable workbench tab. A single file opens directly in preview; multiple or mixed files open the first available file with a selectable, resizable file list. The viewer retains per-file code/preview choices, image zoom and scrolling. Keep canvas cards compact. Image generation uses the same file contract, without requiring JSON descriptions. No json-render Spec is needed. The worker must actually create files in the approved workflow scope at run-specific paths, verify they exist and submit their references through allycli workflow results submit. For a root file schema, data is the path itself. The result protocol is still required, but report content need not be duplicated into JSON fields or streaming JSONL.

For readable structured content composed inside NeuxBot, jointly author a static output schema and meta.nodeResultPages[stableWorkerId]. Plain text can use a page. For only internal technical/control data with no reading value, or user-declined display, write a concrete meta.nodeResultPageOmissions[stableWorkerId] reason. Intermediate status alone does not justify omission. Every worker must appear in exactly one of these three maps; allycli workflow source validate and allycli workflow source save reject missing or conflicting declarations and file views without declared file fields. When the user changes the deliverable, revise the prompt, schema, presentation declaration, downstream consumers and final return together. Trace old prompts, schemas/required lists, submitted fields, presentation bindings, downstream expressions, functions/actions and final returns. Remove abandoned fields, formatting steps and stale labels together; retain actual branch/loop consumers and agreed external contracts. Every remaining runtime read or submitted field must exist in its schema. Remove unused duplicate report fields and the old page rather than appending a FileList to it. Preserve evidence and coverage inside the actual document. File delivery should let the agent design for the requested format and reader, without imposing a fixed page template.

Each result page has formatVersion:1,catalogVersion:"workflow-result-v1",title,description and native json-render spec:{root,elements}. Presentations are frozen with the source revision and show exactly one successful attempt. They add no execution node, phase, action or model call. Never embed business data or secrets in page metadata.

For nodeResultPages, design a complete reading experience. The Agent decides the whole component tree, content hierarchy, reading width, grouping, columns, semantic surfaces, emphasis, alignment and spacing using the actual component properties. Start with Page and one Heading h1; this makes the native Spec own the full page header. Put the primary conclusion or deliverable first. Compose Card, Stack, Grid, Heading, Text, Metric, Callout, Collection and Disclosure as needed, not as a fixed template. Reports, comparisons and recommendation lists should have different layouts. Avoid flattening every schema property into KeyValue, duplicate titles or equivalent metrics, giant whitespace, nesting Card inside Card, or centered full-width source links. Use readable width for prose, grid/list Collection for repeated entries, and Disclosure only for secondary evidence. Preserve true coverage and uncertainty; bind numbers and content to the actual result. Allow empty results and long content. The renderer responds to its own pane width.

Use components in the actual catalog below and preserve the native flat root/elements format. Element fields are type, props, children, visible, repeat. children contains element IDs; all elements must be reachable and acyclic. Read output through {$state:"/result/path"}. Collection takes items (normally {$state:"/result/items"}), pageSize (1..50), and ordinary children: its public native repeat scope resolves {$item:"field"} and {$index:true}. Nested Collection uses {$item:"nestedArray"}, without a repeat on Collection itself. Text and Markdown use value; Table takes rows and columns:[{key,label}] where key is a literal row field. Keep business calculations in the workflow. Use format:file and FileList only for an actual file the user requested or agreed to receive, with a readable label. Internal JSON caches, complete API snapshots and intermediate files are implementation details: keep them out of the delivery schema and page, do not append them as downloads. Ordinary strings are not downloadable files. The user should read the output directly on the page.

Allowed read-only directives are $state, $item, $index and native conditional props ($cond/$then/$else). Native visibility may use eq/neq/gt/gte/lt/lte/not/and/or. Never use on, watch, actions, $bindState, $bindItem, $computed, $template, spec.state, style, DOM tags, JavaScript, external resources or custom directives. Do not use the library default streaming JSONL prompt: save the complete workflow source through allycli workflow source validate and allycli workflow source save.

An optional meta.inputPage uses formatVersion:1,catalogVersion:"workflow-input-v1",spec. Use layout components and Field with a literal path such as "/args/query". Every input field must be covered. Preserve optional/nullable/composed parent objects as whole Field controls; only required plain objects may be split into leaf fields. Field "/args" represents the whole schema, including composed/scalar schemas. Field type, label, required state, choices, file input and validation come from inputSchema; do not duplicate them or hide fields conditionally. Fields write only their own local draft via the host. Submit/approval/save remain business host controls. Normal forms require no inputPage: the host derives a deterministic layout.

Budgets: a page is at most 128 KiB, 256 elements and 32 levels; tables at most 20 columns. Collections paginate rather than mounting all items. Query this topic during authoring whenever the workflow has workers. Before saving, check every worker's page, file delivery or omission reason, and ensure all page bindings match the declared output schema.

## Actual component property schemas

```json
{
  "catalogs": {
    "workflow-result-v1": {
      "Page": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "width": {
            "type": "string",
            "enum": ["readable", "wide", "full"]
          },
          "align": {
            "type": "string",
            "enum": ["start", "center"]
          },
          "gap": {
            "type": "string",
            "enum": ["none", "small", "medium", "large"]
          }
        },
        "additionalProperties": false
      },
      "Stack": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "direction": {
            "type": "string",
            "enum": ["vertical", "horizontal"]
          },
          "gap": {
            "type": "string",
            "enum": ["none", "small", "medium", "large"]
          },
          "align": {
            "type": "string",
            "enum": ["start", "center", "end", "stretch"]
          },
          "justify": {
            "type": "string",
            "enum": ["start", "center", "end", "between"]
          }
        },
        "additionalProperties": false
      },
      "Grid": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "columns": {
            "type": "integer",
            "minimum": 1,
            "maximum": 4
          },
          "gap": {
            "type": "string",
            "enum": ["none", "small", "medium", "large"]
          },
          "ratio": {
            "type": "string",
            "enum": ["equal", "wide-start", "wide-end"]
          },
          "align": {
            "type": "string",
            "enum": ["start", "center", "end", "stretch"]
          }
        },
        "additionalProperties": false
      },
      "Section": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "title": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          },
          "description": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          },
          "gap": {
            "type": "string",
            "enum": ["none", "small", "medium", "large"]
          }
        },
        "additionalProperties": false
      },
      "Card": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "tone": {
            "type": "string",
            "enum": [
              "default",
              "accent",
              "info",
              "success",
              "warning",
              "danger"
            ]
          }
        },
        "additionalProperties": false
      },
      "Heading": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "value": {},
          "level": {
            "type": "string",
            "enum": ["h1", "h2", "h3"]
          },
          "align": {
            "type": "string",
            "enum": ["start", "center", "end"]
          }
        },
        "required": ["value"],
        "additionalProperties": false
      },
      "Callout": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "title": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          },
          "value": {},
          "tone": {
            "type": "string",
            "enum": [
              "default",
              "accent",
              "info",
              "success",
              "warning",
              "danger"
            ]
          }
        },
        "required": ["value"],
        "additionalProperties": false
      },
      "Disclosure": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "title": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          },
          "defaultOpen": {
            "type": "boolean"
          }
        },
        "required": ["title"],
        "additionalProperties": false
      },
      "Text": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "value": {},
          "tone": {
            "type": "string",
            "enum": ["default", "muted", "accent"]
          },
          "size": {
            "type": "string",
            "enum": ["body", "label", "caption"]
          },
          "weight": {
            "type": "string",
            "enum": ["normal", "medium", "semibold"]
          },
          "align": {
            "type": "string",
            "enum": ["start", "center", "end"]
          }
        },
        "required": ["value"],
        "additionalProperties": false
      },
      "Markdown": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "value": {}
        },
        "required": ["value"],
        "additionalProperties": false
      },
      "KeyValue": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "label": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          },
          "value": {}
        },
        "required": ["label", "value"],
        "additionalProperties": false
      },
      "Metric": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "label": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          },
          "value": {},
          "unit": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          },
          "description": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          },
          "tone": {
            "type": "string",
            "enum": [
              "default",
              "accent",
              "info",
              "success",
              "warning",
              "danger"
            ]
          }
        },
        "required": ["label", "value"],
        "additionalProperties": false
      },
      "Badge": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "value": {},
          "tone": {
            "type": "string",
            "enum": ["default", "success", "warning", "danger"]
          }
        },
        "required": ["value"],
        "additionalProperties": false
      },
      "Table": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "rows": {},
          "columns": {
            "minItems": 1,
            "maxItems": 20,
            "type": "array",
            "items": {
              "type": "object",
              "properties": {
                "key": {
                  "type": "string"
                },
                "label": {
                  "type": "string"
                }
              },
              "required": ["key", "label"],
              "additionalProperties": false
            }
          },
          "pageSize": {
            "type": "integer",
            "minimum": 1,
            "maximum": 50
          }
        },
        "required": ["rows", "columns"],
        "additionalProperties": false
      },
      "Collection": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "items": {},
          "pageSize": {
            "type": "integer",
            "minimum": 1,
            "maximum": 50
          },
          "layout": {
            "type": "string",
            "enum": ["list", "grid"]
          },
          "columns": {
            "type": "integer",
            "minimum": 1,
            "maximum": 4
          },
          "gap": {
            "type": "string",
            "enum": ["none", "small", "medium", "large"]
          }
        },
        "required": ["items"],
        "additionalProperties": false
      },
      "Link": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "label": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          },
          "href": {},
          "variant": {
            "type": "string",
            "enum": ["inline", "button"]
          }
        },
        "required": ["label", "href"],
        "additionalProperties": false
      },
      "FileList": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "files": {},
          "label": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          }
        },
        "required": ["files"],
        "additionalProperties": false
      }
    },
    "workflow-input-v1": {
      "Stack": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "direction": {
            "type": "string",
            "enum": ["vertical", "horizontal"]
          },
          "gap": {
            "type": "string",
            "enum": ["small", "medium", "large"]
          }
        },
        "additionalProperties": false
      },
      "Grid": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "columns": {
            "type": "integer",
            "minimum": 1,
            "maximum": 4
          }
        },
        "additionalProperties": false
      },
      "Section": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "title": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          },
          "description": {
            "anyOf": [
              {
                "type": "string"
              },
              {
                "type": "object",
                "properties": {},
                "additionalProperties": {}
              }
            ]
          }
        },
        "additionalProperties": false
      },
      "Field": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "path": {
            "type": "string",
            "pattern": "^\\/args(?:\\/.*)?$"
          }
        },
        "required": ["path"],
        "additionalProperties": false
      },
      "Text": {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "type": "object",
        "properties": {
          "value": {},
          "tone": {
            "type": "string",
            "enum": ["default", "muted"]
          }
        },
        "required": ["value"],
        "additionalProperties": false
      }
    }
  },
  "descriptions": {
    "workflow-result-v1": {
      "Page": {
        "description": "完整结果页根容器。Agent 设计其全部子树；readable 适合长报告，wide 适合卡片与数据，full 适合宽表格。页面标题放入 Heading，避免重复标题。",
        "slots": ["default"],
        "example": {
          "width": "wide",
          "align": "center",
          "gap": "large"
        }
      },
      "Stack": {
        "description": "自由组合纵向内容或可换行的横向标题、状态与来源，选择间距、对齐和分布。",
        "slots": ["default"],
        "example": {
          "direction": "horizontal",
          "gap": "small",
          "align": "center",
          "justify": "between"
        }
      },
      "Grid": {
        "description": "按所在容器宽度降列的网格。双列可选择主次栏比例；窄聊天和右侧 Tab 自动单列。",
        "slots": ["default"],
        "example": {
          "columns": 2,
          "ratio": "wide-start",
          "gap": "large",
          "align": "start"
        }
      },
      "Section": {
        "description": "无重复底板的内容分区；标题、说明和内容建立阅读节奏。背景与强调由 Card 单独选择。",
        "slots": ["default"],
        "example": null
      },
      "Card": {
        "description": "通用语义表面容器，可组织任意组件，不自带业务字段或固定布局。使用主题中性或强调底色，无常驻边框和阴影；避免层层套 Card。",
        "slots": ["default"],
        "example": {
          "tone": "accent"
        }
      },
      "Heading": {
        "description": "页面、分区、条目标题的真实排版层级；每页一个 h1，正文不要用标题样式。",
        "slots": [],
        "example": {
          "value": "本次分析",
          "level": "h1"
        }
      },
      "Callout": {
        "description": "突出关键结论、风险或数据覆盖边界；tone 来自语义主题，不暗示未经确认的成功。",
        "slots": [],
        "example": {
          "title": "阅读范围",
          "value": "建议基于已提供的信息。",
          "tone": "info"
        }
      },
      "Disclosure": {
        "description": "用户可展开的辅助证据与长细节，完整内容保留在页面中；核心结论和下一步不要默认折叠。",
        "slots": ["default"],
        "example": null
      },
      "Text": {
        "description": "普通文本；长文本可展开",
        "slots": [],
        "example": null
      },
      "Markdown": {
        "description": "安全 Markdown；长内容可展开，不加载远程图片",
        "slots": [],
        "example": null
      },
      "KeyValue": {
        "description": "名称和值",
        "slots": [],
        "example": null
      },
      "Metric": {
        "description": "真实指标，强调数值与口径，辅助说明可解释覆盖范围；同组指标共享 Grid，不重复展示同义数量。",
        "slots": [],
        "example": null
      },
      "Badge": {
        "description": "状态标签",
        "slots": [],
        "example": null
      },
      "Table": {
        "description": "分页数据表，key 是行内字段名",
        "slots": [],
        "example": null
      },
      "Collection": {
        "description": "分页集合，可按列表或响应式卡片网格展示。Agent 设计每项完整子树，通过 $item 和 $index 读取当前项，无需 repeat。",
        "slots": ["default"],
        "example": null
      },
      "Link": {
        "description": "仅支持 http/https 的外部链接",
        "slots": [],
        "example": null
      },
      "FileList": {
        "description": "仅用于用户明确需要的真实交付文件；label 使用可读交付名称。内部 JSON 缓存、完整响应和中间数据不属于交付物，不应声明或展示。files 为 Schema 中明确声明的真实路径或路径数组。",
        "slots": [],
        "example": null
      }
    },
    "workflow-input-v1": {
      "Stack": {
        "description": "按顺序排列子组件",
        "slots": ["default"],
        "example": null
      },
      "Grid": {
        "description": "响应式网格",
        "slots": ["default"],
        "example": null
      },
      "Section": {
        "description": "带标题的内容分区",
        "slots": ["default"],
        "example": null
      },
      "Field": {
        "description": "绑定 inputSchema 字段的完整输入组件；类型、标题、校验和文件能力来自 Schema，path 是 JSON Pointer",
        "slots": [],
        "example": null
      },
      "Text": {
        "description": "输入表单中的说明文本",
        "slots": [],
        "example": null
      }
    }
  },
  "digests": {
    "workflow-result-v1": "3a64968fed261413f1bb44129c8c0882daa520934550b7fd8f02b126feb93f70",
    "workflow-input-v1": "66e95c4e9fbe2669a3dedbb06ef2f52c0381f34004b8301940798a3b20349864"
  }
}

```
