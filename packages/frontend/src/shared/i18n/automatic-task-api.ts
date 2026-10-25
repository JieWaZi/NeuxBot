export const automaticTaskApiZh = {
  apiPrepareInputs:
    "将下面示例中的值替换为实际业务值，保留 args、字段名和 JSON 类型。固定参数由任务自动带入，无需传入。",
  apiPrepareNoInputs: "此任务无需填写输入参数，直接使用下面的请求参数。",
  apiSendRequest:
    "向下方调用地址发送 POST 请求，可复制触发示例在终端执行。在 X-API-Key 请求头填写已授权本任务的集成 API Key；每次新运行将 <UNIQUE_REQUEST_KEY> 替换为新的唯一值，同一次请求重试时复用原值。",
  apiReadRunState:
    "触发成功后，从返回结果取得 runId，替换查询状态示例中的 <RUN_ID>，即可查看本次运行状态。",
  apiReadResult:
    "查询结果接口返回运行状态、result 和 files。完成后使用 files 中的 id 下载文件；运行标识使用触发返回的 runId。",
  apiUploadFileHelp:
    "文件参数先通过上传接口提交 file，随后将返回的 reference 填入对应 args 字段。文件数组逐个上传；单个文件最多 100 MB。",
  requestBodyExample: "请求参数示例",
  apiCallGuide: "如何调用",
  apiParameters: "API 参数说明",
  apiParameterRequired: "必填",
  apiParameterOptional: "可选",
  exampleNeedsReplacement:
    "示例中的部分占位值不满足字段约束，请按字段说明填写真实值后再调用。",
  advancedInputSchema: "高级：参数 JSON Schema",
  noAPIParameters: "此任务无需传入参数，所有值由任务或工作流提供。",
  inputSchema: "参数 JSON Schema",
  apiHint: "保存后可取得调用地址；请在 API 集成中创建 Key 并授权本任务。",
  apiEditHint:
    "通过脚本触发运行；API Key 的签发、轮换与任务授权统一在 API 集成中管理。",
  switchWarning:
    "切换到定时后，本任务不再接受 API 触发。已有运行继续使用原配置。",
  apiIP: "IP 地址",
  apiIPHint:
    "127.0.0.1 仅供本机调用；填写设备 IP 可供其他设备调用。端口范围为 1–65535。",
  port: "端口",
  apiSaving: "正在应用…",
  apiConfirm: "确认",
};
export const automaticTaskApiEn = {
  apiPrepareInputs:
    "Replace the example values with real business data. Keep args, field names and JSON types. The task supplies fixed inputs automatically; do not send them.",
  apiPrepareNoInputs:
    "This task needs no caller inputs. Use the request body below.",
  apiSendRequest:
    "Send a POST request to the endpoint below, or copy the trigger example into a terminal. Put an integration API Key authorized for this task in the X-API-Key header. Replace <UNIQUE_REQUEST_KEY> with a new unique value for each new run; reuse it when retrying the same request.",
  apiReadRunState:
    "After the trigger succeeds, take runId from the response and replace <RUN_ID> in the status example to check this run.",
  apiReadResult:
    "The result endpoint returns run state, result and files. Once complete, download files using their id. Use the runId returned by the trigger endpoint.",
  apiUploadFileHelp:
    "Upload each file using the file field, then put the returned reference in its args field. Upload array items separately. Maximum 100 MB per file.",
  requestBodyExample: "Request body example",
  apiCallGuide: "How to call",
  apiParameters: "API parameters",
  apiParameterRequired: "Required",
  apiParameterOptional: "Optional",
  exampleNeedsReplacement:
    "Some placeholder values do not satisfy the field constraints. Follow the field descriptions and fill in real values before calling.",
  advancedInputSchema: "Advanced: input JSON Schema",
  noAPIParameters:
    "No API inputs are needed. The task and workflow supply all values.",
  inputSchema: "Input JSON Schema",
  apiHint:
    "After saving, use the endpoint with a Key created and authorized for this task in API Integration.",
  apiEditHint:
    "Trigger runs from a script. Manage API Key issuance, rotation, and task grants in API Integration.",
  switchWarning:
    "Switching to schedule stops API triggers for this task. Accepted runs retain their configuration.",
  apiIP: "IP address",
  apiIPHint:
    "127.0.0.1 allows local calls only. Use the device IP for calls from other devices. Ports range from 1 to 65535.",
  port: "Port",
  apiSaving: "Applying…",
  apiConfirm: "Confirm",
};
