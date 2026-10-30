import { http, HttpResponse } from "msw";
import { PERSONAL_ASSISTANT_WAKER_ID } from "@/modules/waker";

/** 完整聊天工作区查询真实 HTTP 边界的最小可用目录。 */
export function chatWorkspaceHandlers(origin: string) {
  const api = `${origin}/api/v1`;
  return [
    http.get(`${api}/wakers`, () =>
      HttpResponse.json({
        resources: [{ id: PERSONAL_ASSISTANT_WAKER_ID, name: "NeuxBot" }],
      }),
    ),
    http.get(`${api}/wakers/:wakerId/projects`, () =>
      HttpResponse.json({ resources: [] }),
    ),
    http.get(`${api}/chats`, () => HttpResponse.json({ resources: [] })),
    http.get(`${api}/wakers/:wakerId/plugin-references`, () =>
      HttpResponse.json([]),
    ),
    http.get(`${api}/wakers/:wakerId/skills`, () => HttpResponse.json([])),
    http.get(`${api}/wakers/:wakerId/connectors`, () => HttpResponse.json([])),
    http.get(`${api}/ai-accounts`, () => HttpResponse.json({ resources: [] })),
    http.get(`${api}/runtime-environments`, () =>
      HttpResponse.json({ resources: [] }),
    ),
    http.get(`${api}/runtimes`, () =>
      HttpResponse.json({
        resources: [
          {
            id: "codex",
            name: "Codex",
            enabled: true,
            state: "ready",
            isDefault: true,
            defaultModel: "native",
            defaultPermissionLevel: "default",
            models: ["native", "selected-model", "actual-model"].map((id) => ({
              id,
              name: id,
              permissionLevels: ["default", "auto", "full-access"],
            })),
          },
        ],
      }),
    ),
  ];
}
