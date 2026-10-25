import type { TaskView } from "../model/types";
import { shellQuote } from "../model/workflow-inputs";
export { CopyValue } from "./copy-value";

export function apiExamples(
  taskId: string,
  publicBaseURL: string,
  workflow = false,
  contract?: TaskView["workflowInputContract"],
) {
  const endpoint = `${publicBaseURL.replace(/\/$/, "")}/automatic-tasks/${encodeURIComponent(taskId)}/runs`;
  const body = workflow ? { args: contract?.example ?? {} } : { input: "" };
  const examples = {
    endpoint,
    trigger: `curl -X POST ${shellQuote(endpoint)} \\\n  -H ${shellQuote("X-API-Key: <API_KEY>")} \\\n  -H 'Idempotency-Key: <UNIQUE_REQUEST_KEY>' \\\n  -H 'Content-Type: application/json' \\\n  -d ${shellQuote(JSON.stringify(body))}`,
    status: `curl ${shellQuote(`${endpoint}/<RUN_ID>`)} \\\n  -H ${shellQuote("X-API-Key: <API_KEY>")}`,
    upload: `curl -X POST ${shellQuote(endpoint.replace(/\/runs$/, "/files"))} -H ${shellQuote("X-API-Key: <API_KEY>")} -F 'file=@<INPUT_FILE>'`,
    result: `curl ${shellQuote(`${endpoint}/<RUN_ID>/result`)} -H ${shellQuote("X-API-Key: <API_KEY>")}`,
    download: `curl ${shellQuote(`${endpoint}/<RUN_ID>/files/<FILE_ID>`)} -H ${shellQuote("X-API-Key: <API_KEY>")} --output '<OUTPUT_FILE>'`,
  };
  return examples;
}
