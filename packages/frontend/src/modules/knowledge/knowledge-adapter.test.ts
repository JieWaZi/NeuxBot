import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { client } from "@/shared/api/generated/client.gen";
import { ApiError } from "@/shared/api/errors";
import {
  ALLY_LOCAL_ERROR_SURFACE,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { scopedKnowledgeOptions } from "@/shared/api/knowledge/knowledge-adapter-types";
import "./use-copy";
import { knowledgeAdapter } from "@/shared/api/knowledge";
import type { ResourceRef } from "@/shared/api/knowledge";

const origin = "http://knowledge-adapter.test";
const localRoot = `${origin}/api/v1/knowledge/libraries/kb`;
const remoteRoot = `${origin}/api/v1/knowledge/connections/connection/libraries/kb`;
const server = setupServer(
  http.get(
    `${remoteRoot}/operations/:requestId`,
    () => new HttpResponse(null, { status: 404 }),
  ),
);
const localRef = {
  backend: "local",
  connectionId: "local",
  knowledgeBaseId: "kb",
};
const customRef = {
  backend: "custom",
  connectionId: "connection",
  knowledgeBaseId: "kb",
};
const officialRef = { ...customRef, backend: "weknora" };
const integrationRef = { ...customRef, backend: "integration" };

beforeAll(() => {
  client.setConfig({ baseUrl: origin });
  server.listen({ onUnhandledRequest: "error" });
});
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

function capabilities(
  status = { supported: true, permission: "allowed", condition: "ready" },
) {
  const keys = [
    "read",
    "write",
    "rawOnly",
    "organize",
    "rebuild",
    "sourcePurge",
    "libraryDelete",
    "themeWrite",
    "settings",
    "history",
  ];
  return {
    profile: "neux-knowledge-v1",
    operations: Object.fromEntries(
      keys.map((key) => [
        key,
        status.supported &&
          status.permission === "allowed" &&
          status.condition === "ready",
      ]),
    ),
    details: Object.fromEntries(keys.map((key) => [key, status])),
    profiles: [{ id: "server", name: "服务器" }],
  };
}

describe("知识库 Adapter 的目标与能力边界", () => {
  it("资源身份作用域保留调用方局部错误声明供 Client 运行时处理", () => {
    const options = scopedKnowledgeOptions(
      customRef,
      withLocalErrorSurface({
        path: { connectionId: "other", libraryId: "other" },
      }),
    );
    expect(new Headers(options.headers).get(ALLY_LOCAL_ERROR_SURFACE)).toBe(
      "local",
    );
    expect(options.path).toEqual({
      connectionId: "connection",
      libraryId: "kb",
    });
  });

  it("统一远端读取保持完整目标且保留服务端识别元数据", async () => {
    server.use(
      http.get(remoteRoot, () =>
        HttpResponse.json({ id: "kb", name: "统一远端" }),
      ),
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json({
          ...capabilities(),
          serviceKind: "neuxally",
          uploadMode: "separate",
          connectionRevision: 3,
          evidence: ["explicit"],
        }),
      ),
    );
    const adapter = knowledgeAdapter(integrationRef);
    expect(
      (await adapter.getLibrary({ path: { libraryId: "other" } })).data.name,
    ).toBe("统一远端");
    expect(await adapter.getCapabilities()).toMatchObject({
      serviceKind: "neuxally",
      uploadMode: "separate",
      connectionRevision: 3,
      evidence: ["explicit"],
    });
  });
  it("统一远端缺少显式配置或图谱能力时不从读写权限推断", async () => {
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json({
          operations: { read: true, write: true },
          details: {
            read: {
              supported: true,
              permission: "allowed",
              condition: "ready",
            },
            write: {
              supported: true,
              permission: "allowed",
              condition: "ready",
            },
          },
        }),
      ),
    );
    const adapter = knowledgeAdapter(integrationRef);
    await expect(adapter.assertCapability("settings")).rejects.toMatchObject({
      code: "KnowledgeUnsupported",
    });
    await expect(adapter.assertCapability("graph")).rejects.toMatchObject({
      code: "KnowledgeUnsupported",
    });
  });
  it("统一上传只发送已确认模式，模式变更时不发送业务写入", async () => {
    let uploads = 0;
    let mode = "separate";
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json({
          ...capabilities(),
          uploadMode: mode,
          connectionRevision: 3,
        }),
      ),
      http.post(`${remoteRoot}/uploads`, async ({ request }) => {
        uploads++;
        const body = await request.json();
        expect(body).toMatchObject({
          requestId: "file-a",
          uploadMode: "separate",
          connectionRevision: 3,
        });
        return HttpResponse.json({
          requestId: "file-a",
          sourceId: "source-a",
          accepted: true,
          uploadMode: "separate",
          transferState: "accepted",
        });
      }),
    );
    const adapter = knowledgeAdapter(integrationRef);
    const confirmed = await adapter.getUploadContext();
    expect(confirmed).toEqual({
      uploadMode: "separate",
      connectionRevision: 3,
    });
    const options = {
      path: { libraryId: "kb" },
      body: { requestId: "file-a", filename: "a.md", bytes: "YQ==" },
    };
    expect((await adapter.uploadSource(options, confirmed)).data).toMatchObject(
      { accepted: true, sourceId: "source-a" },
    );
    mode = "automatic";
    await expect(
      adapter.uploadSource(
        { ...options, body: { ...options.body, requestId: "file-b" } },
        confirmed,
      ),
    ).rejects.toMatchObject({ code: "KnowledgeUploadModeChanged" });
    expect(uploads).toBe(1);
  });
  it("自动处理服务没有确认回执时只查询原上传，不重新发送", async () => {
    let uploads = 0;
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json({
          ...capabilities(),
          uploadMode: "automatic",
          connectionRevision: 3,
          details: {
            upload: {
              supported: true,
              permission: "unknown",
              condition: "automatic_processing",
            },
          },
        }),
      ),
      http.get(`${remoteRoot}/operations/file-a`, () =>
        HttpResponse.json({
          requestId: "file-a",
          operation: "source.upload.native",
          nativeUpload: true,
          transferState: "unknown",
          ref: integrationRef,
        }),
      ),
      http.get(`${remoteRoot}/uploads/file-a`, () =>
        HttpResponse.json({
          requestId: "file-a",
          accepted: false,
          uploadMode: "automatic",
          transferState: "unknown",
        }),
      ),
      http.post(`${remoteRoot}/uploads`, () => {
        uploads++;
        return HttpResponse.json({});
      }),
    );
    const adapter = knowledgeAdapter(integrationRef);
    const confirmed = await adapter.getUploadContext();
    await expect(
      adapter.uploadSource(
        {
          path: { libraryId: "kb" },
          body: { requestId: "file-a", filename: "a.md", bytes: "YQ==" },
        },
        confirmed,
      ),
    ).rejects.toMatchObject({ code: "KnowledgeRemoteOutcomeUnknown" });
    expect(uploads).toBe(0);
  });
  it("上传响应缺少接纳事实时保持未知，不能显示成功", async () => {
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json({
          ...capabilities(),
          uploadMode: "separate",
          connectionRevision: 3,
        }),
      ),
      http.post(`${remoteRoot}/uploads`, () =>
        HttpResponse.json({
          requestId: "file-a",
          accepted: false,
          transferState: "unknown",
        }),
      ),
    );
    const adapter = knowledgeAdapter(integrationRef);
    await expect(
      adapter.uploadSource(
        {
          path: { libraryId: "kb" },
          body: { requestId: "file-a", filename: "a.md", bytes: "YQ==" },
        },
        { uploadMode: "separate", connectionRevision: 3 },
      ),
    ).rejects.toMatchObject({ code: "KnowledgeRemoteOutcomeUnknown" });
  });
  it.each([localRef, customRef])(
    "读取以冻结引用覆盖调用方库身份且不预读能力 $backend",
    async (ref: ResourceRef) => {
      const root = ref.backend === "local" ? localRoot : remoteRoot;
      server.use(
        http.get(root, () => HttpResponse.json({ id: "kb", name: "正确目标" })),
      );
      const adapter = knowledgeAdapter(ref);
      const result = await adapter.getLibrary({
        path: { libraryId: "other" },
        throwOnError: true,
      });
      expect(result.data).toMatchObject({ id: "kb", name: "正确目标" });
    },
  );

  it("官方写入在任何 HTTP 请求之前明确拒绝", async () => {
    await expect(
      knowledgeAdapter(officialRef).importSource({
        path: { libraryId: "kb" },
        body: { requestId: "upload", filename: "a.md", bytes: "YQ==" },
      }),
    ).rejects.toMatchObject({ message: "远端能力未支持", status: 501 });
  });

  it("custom 权限拒绝与协议不支持分开反馈并且不发写入", async () => {
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json(
          capabilities({
            supported: true,
            permission: "denied",
            condition: "ready",
          }),
        ),
      ),
    );
    await expect(
      knowledgeAdapter(customRef).createTheme({
        path: { libraryId: "other" },
        body: {
          requestId: "theme",
          structureVersion: 3,
          theme: { name: "主题" },
        },
      }),
    ).rejects.toMatchObject({ code: "KnowledgeForbidden", status: 403 });
  });

  it("custom 能力读取失败不会推断允许", async () => {
    server.use(
      http.get(
        `${remoteRoot}/capabilities`,
        () => new HttpResponse(null, { status: 503 }),
      ),
    );
    await expect(
      knowledgeAdapter(customRef).assertCapability("write"),
    ).rejects.toBeDefined();
  });

  it("custom 配置条件不满足保留条件错误", async () => {
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json(
          capabilities({
            supported: true,
            permission: "allowed",
            condition: "profile_unavailable",
          }),
        ),
      ),
    );
    await expect(
      knowledgeAdapter(customRef).assertCapability("organize"),
    ).rejects.toMatchObject({
      code: "KnowledgeConditionUnavailable",
      status: 409,
    });
  });

  it("能力保留 details 与服务端 profile", async () => {
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json(capabilities()),
      ),
    );
    expect(await knowledgeAdapter(customRef).getCapabilities()).toMatchObject({
      details: {
        organize: {
          supported: true,
          permission: "allowed",
          condition: "ready",
        },
      },
      profiles: [{ id: "server", name: "服务器" }],
    });
  });

  it("远端本机专属配置与索引读取不会调用同 ID 本机资源", async () => {
    const adapter = knowledgeAdapter(customRef);
    const path = { libraryId: "kb" };
    for (const operation of [
      () => adapter.getLibraryExecutionDefaults({ path }),
      () => adapter.getLibraryEmbeddingProfile({ path }),
      () => adapter.getWikiIndex({ path }),
      () =>
        adapter.testLibraryEmbedding({
          body: { baseUrl: "https://example.com/v1", model: "embedding" },
        }),
    ])
      await expect(operation()).rejects.toBeInstanceOf(ApiError);
    await expect(
      knowledgeAdapter(officialRef).listLibraryActivity({ path }),
    ).rejects.toMatchObject({ status: 501 });
    await expect(
      knowledgeAdapter(officialRef).getSourceChunks({
        path: { ...path, sourceId: "source" },
      }),
    ).rejects.toMatchObject({ status: 501 });
  });

  it("图谱与历史能力不由远端标签推断", () => {
    expect(knowledgeAdapter(localRef)).toMatchObject({
      graphMode: "pages",
      revisionsIncludeContent: true,
      localConfiguration: true,
    });
    expect(knowledgeAdapter(customRef)).toMatchObject({
      graphMode: "pages",
      revisionsIncludeContent: false,
      localConfiguration: false,
    });
    expect(knowledgeAdapter(officialRef)).toMatchObject({
      graphMode: "remote",
      revisionsIncludeContent: false,
      localConfiguration: false,
    });
  });

  it("官方已支持读取允许服务端判权，本机未知动作明确拒绝", async () => {
    for (const key of ["read", "sourceDownload", "history", "themes", "graph"])
      await expect(
        knowledgeAdapter(officialRef).assertCapability(key),
      ).resolves.toBeUndefined();
    await expect(
      knowledgeAdapter(localRef).assertCapability("unknown_operation"),
    ).rejects.toMatchObject({ code: "KnowledgeUnsupported" });
  });

  it("custom 分块和活动读取始终使用连接范围，保留真实查询参数", async () => {
    server.use(
      http.get(`${remoteRoot}/sources/version/chunks`, () =>
        HttpResponse.json([
          {
            id: "chunk",
            chunkType: "child",
            content: "远端解析正文",
            sequence: 1,
          },
        ]),
      ),
      http.get(`${remoteRoot}/activity`, ({ request }) => {
        expect(new URL(request.url).searchParams.get("afterId")).toBe("12");
        expect(new URL(request.url).searchParams.get("action")).toBe(
          "knowledge_create",
        );
        return HttpResponse.json({
          data: [
            { id: 13, action: "knowledge_create", actorUserId: "remote-user" },
          ],
          nextCursor: 13,
        });
      }),
    );
    const adapter = knowledgeAdapter(customRef);
    expect(
      (
        await adapter.getSourceChunks({
          path: { libraryId: "other", sourceId: "version" },
        })
      ).data[0]?.content,
    ).toBe("远端解析正文");
    expect(
      (
        await adapter.listLibraryActivity({
          path: { libraryId: "other" },
          query: { afterId: 12, action: "knowledge_create", limit: 30 },
        })
      ).data.data?.[0]?.actorUserId,
    ).toBe("remote-user");
  });

  it("远端历史快照保留页面版本和分层语义地址", async () => {
    server.use(
      http.get(`${remoteRoot}/pages/page/revisions/2`, ({ request }) => {
        expect(new URL(request.url).searchParams.get("slug")).toBe("主题/页面");
        return HttpResponse.json({
          pageId: "page",
          version: 2,
          content: "远端真实历史",
        });
      }),
    );
    expect(
      (
        await knowledgeAdapter(officialRef).getRevision({
          path: {
            connectionId: "wrong",
            libraryId: "other",
            pageId: "page",
            version: 2,
          },
          query: { slug: "主题/页面" },
        })
      ).data.content,
    ).toBe("远端真实历史");
  });

  it("来源永久清理的预览确认状态恢复都保留同一远端目标", async () => {
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json(capabilities()),
      ),
      http.get(`${remoteRoot}/source-purges/preview`, ({ request }) => {
        expect(new URL(request.url).searchParams.get("sourceId")).toBe(
          "stable-source",
        );
        return HttpResponse.json({
          sourceId: "stable-source",
          digest: "frozen-digest",
        });
      }),
      http.post(`${remoteRoot}/source-purges/confirm`, async ({ request }) => {
        expect(await request.json()).toMatchObject({
          requestId: "confirm-original",
          sourceId: "stable-source",
          previewDigest: "frozen-digest",
          confirmed: true,
        });
        return HttpResponse.json({
          resourceId: "purge",
          requestId: "confirm-original",
          state: "accepted",
        });
      }),
      http.get(`${remoteRoot}/source-purges/purge`, () =>
        HttpResponse.json({ id: "purge", state: "blocked", version: 2 }),
      ),
      http.post(
        `${remoteRoot}/source-purges/purge/recover`,
        async ({ request }) => {
          expect(await request.json()).toMatchObject({
            requestId: "recover-original",
            expectedPolicyVersion: 3,
          });
          return HttpResponse.json({
            resourceId: "purge",
            requestId: "recover-original",
            state: "accepted",
          });
        },
      ),
    );
    const adapter = knowledgeAdapter(customRef);
    const path = { libraryId: "other" };
    const preview = (
      await adapter.previewSourcePurge({
        path,
        query: { sourceId: "stable-source" },
      })
    ).data;
    const receipt = (
      await adapter.confirmSourcePurge({
        path,
        body: {
          requestId: "confirm-original",
          sourceId: preview.sourceId || "",
          previewDigest: preview.digest || "",
          confirmed: true,
        },
      })
    ).data;
    expect(
      (
        await adapter.getSourcePurge({
          path: { ...path, purgeId: receipt.resourceId || "" },
        })
      ).data.state,
    ).toBe("blocked");
    expect(
      (
        await adapter.recoverSourcePurge({
          path: { ...path, purgeId: "purge" },
          body: { requestId: "recover-original", expectedPolicyVersion: 3 },
        })
      ).data.requestId,
    ).toBe("recover-original");
  });

  it("custom 创建使用连接范围，不查询空库或其他库的能力", async () => {
    server.use(
      http.post(
        `${origin}/api/v1/knowledge/connections/connection/libraries`,
        async ({ request }) => {
          expect(await request.json()).toEqual({
            requestId: "create",
            name: "远端库",
            wikiLanguage: "zh-CN",
          });
          return HttpResponse.json({
            requestId: "create",
            resourceId: "created",
            state: "saved",
          });
        },
      ),
    );
    expect(
      (
        await knowledgeAdapter({
          ...customRef,
          knowledgeBaseId: "",
        }).createLibrary({
          body: { requestId: "create", name: "远端库", wikiLanguage: "zh-CN" },
        })
      ).data.resourceId,
    ).toBe("created");
  });

  it("远端创建拒绝本机宿主配置而非静默丢弃", async () => {
    await expect(
      knowledgeAdapter({ ...customRef, knowledgeBaseId: "" }).createLibrary({
        body: {
          requestId: "create",
          name: "远端库",
          wikiLanguage: "zh-CN",
          executionDefaults: { runtimeId: "local-runtime" },
        },
      }),
    ).rejects.toMatchObject({ code: "KnowledgeUnsupported", status: 501 });
  });

  it("custom 写入使用连接范围并保留原请求身份", async () => {
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json(capabilities()),
      ),
      http.post(`${remoteRoot}/sources`, async ({ request }) => {
        expect(await request.json()).toMatchObject({
          requestId: "original",
          filename: "a.md",
          bytes: "YQ==",
        });
        return HttpResponse.json({
          requestId: "original",
          resourceId: "source",
          state: "saved",
        });
      }),
    );
    const result = await knowledgeAdapter(customRef).importSource({
      path: { libraryId: "other" },
      body: { requestId: "original", filename: "a.md", bytes: "YQ==" },
    });
    expect(result.data.resourceId).toBe("source");
  });

  it("远端整理只传服务器配置，不传本机执行字段", async () => {
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json(capabilities()),
      ),
      http.post(`${remoteRoot}/organize`, async ({ request }) => {
        expect(await request.json()).toEqual({
          requestId: "organize",
          expectedVersion: 2,
          structureVersion: 3,
          sourceVersionIds: ["source"],
          profileId: "server",
          rebuild: true,
          instruction: "按主题提取",
        });
        return HttpResponse.json({
          requestId: "organize",
          jobId: "job",
          state: "accepted",
        });
      }),
    );
    const body = {
      requestId: "organize",
      expectedVersion: 2,
      structureVersion: 3,
      sourceVersionIds: ["source"],
      profileId: "server",
      rebuild: true,
      instruction: "按主题提取",
      runtimeId: "local-runtime",
      accountId: "private-account",
      wakerId: "local-waker",
    };
    const result = await knowledgeAdapter(customRef).organize({
      path: { connectionId: "other", libraryId: "other" },
      body,
    });
    expect(result.data.receipt).toMatchObject({
      jobId: "job",
      state: "accepted",
    });
  });

  it("工厂固定资源引用，后续修改输入不改变目标", async () => {
    server.use(http.get(remoteRoot, () => HttpResponse.json({ id: "kb" })));
    const ref = { ...customRef };
    const adapter = knowledgeAdapter(ref);
    ref.connectionId = "changed";
    ref.knowledgeBaseId = "other";
    expect(
      (await adapter.getLibrary({ path: { libraryId: "ignored" } })).data.id,
    ).toBe("kb");
  });

  it("库已删除后的能力404仍可继续原持久删除操作对账", async () => {
    let deleted = false;
    let hostCalls = 0;
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        deleted
          ? new HttpResponse(null, { status: 404 })
          : HttpResponse.json(capabilities()),
      ),
      http.get(`${remoteRoot}/operations/original-delete`, () =>
        HttpResponse.json({
          ref: customRef,
          requestId: "original-delete",
          operation: "library.delete",
          receiptConfirmed: false,
        }),
      ),
      http.delete(remoteRoot, async ({ request }) => {
        expect(await request.json()).toEqual({
          requestId: "original-delete",
          expectedVersion: 1,
        });
        hostCalls++;
        if (!deleted) {
          deleted = true;
          return HttpResponse.error();
        }
        return HttpResponse.json({
          requestId: "original-delete",
          state: "accepted",
        });
      }),
    );
    const options = {
      path: { libraryId: "kb" },
      body: { requestId: "original-delete", expectedVersion: 1 },
    };
    await expect(
      knowledgeAdapter(customRef).deleteLibrary(options),
    ).rejects.toBeDefined();
    expect(
      (await knowledgeAdapter(customRef).deleteLibrary(options)).data.requestId,
    ).toBe("original-delete");
    expect(hostCalls).toBe(2);
  });

  it("写权限拒绝但原操作对账也不确定时保留未知结果身份", async () => {
    server.use(
      http.get(`${remoteRoot}/capabilities`, () =>
        HttpResponse.json(
          capabilities({
            supported: true,
            permission: "denied",
            condition: "ready",
          }),
        ),
      ),
      http.get(
        `${remoteRoot}/operations/original`,
        () => new HttpResponse(null, { status: 503 }),
      ),
    );
    await expect(
      knowledgeAdapter(customRef).writePage({
        path: { libraryId: "kb", pageId: "page" },
        body: {
          requestId: "original",
          expectedVersion: 1,
          structureVersion: 1,
          page: { content: "原草稿" },
        },
      }),
    ).rejects.toMatchObject({
      code: "KnowledgeRemoteOutcomeUnknown",
      status: 504,
    });
  });
});

describe("历史快照的完整分页", () => {
  it("服务端总数增加后继续读取请求的真实版本", async () => {
    server.use(
      http.get(`${localRoot}/pages/page/revisions`, ({ request }) => {
        const offset = Number(new URL(request.url).searchParams.get("offset"));
        return HttpResponse.json(
          offset === 0
            ? { items: [{ version: 5 }, { version: 4 }], total: 3 }
            : offset === 2
              ? { items: [{ version: 3 }], total: 5 }
              : {
                  items: [
                    { version: 2, content: "真实历史正文" },
                    { version: 1 },
                  ],
                  total: 5,
                },
        );
      }),
    );
    const result = await knowledgeAdapter(localRef).getRevision({
      path: {
        connectionId: "local",
        libraryId: "other",
        pageId: "page",
        version: 2,
      },
    });
    expect(result.data.content).toBe("真实历史正文");
  });

  it("历史重复页不能当作完整读取后返回不存在", async () => {
    server.use(
      http.get(`${localRoot}/pages/page/revisions`, () =>
        HttpResponse.json({
          items: [{ version: 4 }, { version: 3 }],
          total: 4,
        }),
      ),
    );
    await expect(
      knowledgeAdapter(localRef).getRevision({
        path: {
          connectionId: "local",
          libraryId: "kb",
          pageId: "page",
          version: 2,
        },
      }),
    ).rejects.toMatchObject({
      code: "KnowledgeRemoteIncompatible",
      status: 502,
    });
  });
});

describe("已删除库的持久清理恢复", () => {
  it("新恢复身份依靠真实清单授权版本，不查询已删除库能力", async () => {
    let capabilitiesReads = 0;
    server.use(
      http.get(`${remoteRoot}/capabilities`, () => {
        capabilitiesReads++;
        return new HttpResponse(null, { status: 404 });
      }),
      http.get(`${remoteRoot}/deletion`, () =>
        HttpResponse.json({
          libraryId: "kb",
          version: 3,
          policyVersion: 2,
          state: "pending",
          pendingBlobCount: 1,
          deletedBlobCount: 0,
          blockedBlobCount: 0,
          retainedSharedBlobCount: 0,
        }),
      ),
      http.post(`${remoteRoot}/deletion/recover`, async ({ request }) => {
        expect(await request.json()).toEqual({
          requestId: "first-recovery",
          expectedVersion: 3,
        });
        return HttpResponse.json({
          requestId: "first-recovery",
          resourceId: "kb",
          state: "accepted",
        });
      }),
    );
    expect(
      (
        await knowledgeAdapter(customRef).recoverLibraryDeletion({
          path: { libraryId: "other" },
          body: { requestId: "first-recovery", expectedVersion: 3 },
        })
      ).data.requestId,
    ).toBe("first-recovery");
    expect(capabilitiesReads).toBe(0);
  });

  it("不存在的恢复请求不能跳过清单身份或版本校验", async () => {
    server.use(
      http.get(`${remoteRoot}/deletion`, () =>
        HttpResponse.json({
          libraryId: "other",
          version: 4,
          policyVersion: 2,
          state: "pending",
        }),
      ),
    );
    await expect(
      knowledgeAdapter(customRef).recoverLibraryDeletion({
        path: { libraryId: "kb" },
        body: { requestId: "first-recovery", expectedVersion: 3 },
      }),
    ).rejects.toMatchObject({ code: "KnowledgeConflict", status: 409 });
  });

  it("原恢复对账不可用且清单已推进时仍保留未知结果身份", async () => {
    server.use(
      http.get(
        `${remoteRoot}/operations/original-recovery`,
        () => new HttpResponse(null, { status: 503 }),
      ),
      http.get(`${remoteRoot}/deletion`, () =>
        HttpResponse.json({
          libraryId: "kb",
          version: 4,
          policyVersion: 2,
          state: "completed",
        }),
      ),
    );
    await expect(
      knowledgeAdapter(customRef).recoverLibraryDeletion({
        path: { libraryId: "kb" },
        body: { requestId: "original-recovery", expectedVersion: 3 },
      }),
    ).rejects.toMatchObject({
      code: "KnowledgeRemoteOutcomeUnknown",
      status: 504,
    });
  });

  it("已持久恢复请求只对账原身份，不因清单版本推进而再次拒绝", async () => {
    let capabilitiesReads = 0;
    server.use(
      http.get(`${remoteRoot}/capabilities`, () => {
        capabilitiesReads++;
        return new HttpResponse(null, { status: 404 });
      }),
      http.get(`${remoteRoot}/operations/original-recovery`, () =>
        HttpResponse.json({
          requestId: "original-recovery",
          operation: "library.delete.recover",
          ref: customRef,
          receiptConfirmed: false,
        }),
      ),
      http.post(`${remoteRoot}/deletion/recover`, async ({ request }) => {
        expect(await request.json()).toEqual({
          requestId: "original-recovery",
          expectedVersion: 3,
        });
        return HttpResponse.json({
          requestId: "original-recovery",
          resourceId: "kb",
          state: "accepted",
        });
      }),
    );
    expect(
      (
        await knowledgeAdapter(customRef).recoverLibraryDeletion({
          path: { libraryId: "kb" },
          body: { requestId: "original-recovery", expectedVersion: 3 },
        })
      ).data.requestId,
    ).toBe("original-recovery");
    expect(capabilitiesReads).toBe(0);
  });
});

describe("远端目录完整读取", () => {
  it("自有服务完整目录一次读取，不按父目录重复取整表", async () => {
    let reads = 0;
    server.use(
      http.get(`${remoteRoot}/themes`, ({ request }) => {
        reads++;
        expect(new URL(request.url).searchParams.get("all")).toBe("true");
        return HttpResponse.json({
          complete: true,
          items: [
            { id: "root", parentId: "", hasChildren: true },
            { id: "child", parentId: "root", hasChildren: true },
            { id: "leaf", parentId: "child", hasChildren: false },
          ],
        });
      }),
    );
    const result = await knowledgeAdapter(customRef).listThemes({
      path: { libraryId: "kb" },
    });
    expect(result.data.items).toHaveLength(3);
    expect(reads).toBe(1);
  });

  it("逐层读取父子目录并保留计数", async () => {
    server.use(
      http.get(`${remoteRoot}/themes`, ({ request }) => {
        const parentId =
          new URL(request.url).searchParams.get("parentId") || "";
        return HttpResponse.json({
          parentId,
          items: parentId
            ? [
                {
                  id: "child",
                  parentId: "root",
                  name: "子目录",
                  hasChildren: false,
                  pageCount: 2,
                },
              ]
            : [
                {
                  id: "root",
                  parentId: "",
                  name: "根目录",
                  hasChildren: true,
                  pageCount: 1,
                },
              ],
        });
      }),
    );
    const { data } = await knowledgeAdapter(officialRef).listThemes({
      path: { libraryId: "other" },
    });
    expect(data.items).toMatchObject([
      { id: "root", pageCount: 1 },
      { id: "child", parentId: "root", pageCount: 2 },
    ]);
  });

  it("子目录读取失败不能返回不完整目录", async () => {
    server.use(
      http.get(`${remoteRoot}/themes`, ({ request }) =>
        new URL(request.url).searchParams.get("parentId")
          ? new HttpResponse(null, { status: 503 })
          : HttpResponse.json({
              parentId: "",
              items: [{ id: "root", parentId: "", hasChildren: true }],
            }),
      ),
    );
    await expect(
      knowledgeAdapter(customRef).listThemes({ path: { libraryId: "kb" } }),
    ).rejects.toBeDefined();
  });

  it("目录循环明确失败而非截断成功", async () => {
    server.use(
      http.get(`${remoteRoot}/themes`, () =>
        HttpResponse.json({
          items: [{ id: "root", parentId: "", hasChildren: true }],
        }),
      ),
    );
    await expect(
      knowledgeAdapter(customRef).listThemes({ path: { libraryId: "kb" } }),
    ).rejects.toMatchObject({ code: "KnowledgeIncompleteDirectory" });
  });

  it("已取消的读取不会返回空目录", async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(
      knowledgeAdapter(customRef).listThemes({
        path: { libraryId: "kb" },
        signal: controller.signal,
      }),
    ).rejects.toBeDefined();
  });
});
