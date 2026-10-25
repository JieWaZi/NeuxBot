import assert from "node:assert/strict";
import test from "node:test";
import { projectPublicOpenAPI } from "./public-openapi.mjs";
const route = {
  method: "GET",
  path: "/api/v1/example/:id",
  operationId: "PublishedExample",
};
const source = () => ({
  openapi: "3.0.3",
  info: { title: "management", version: "1" },
  paths: {
    "/api/v1/example/{id}": {
      get: {
        operationId: "PublishedExample",
        responses: {
          200: {
            content: {
              "application/json": {
                schema: { $ref: "#/components/schemas/Result" },
              },
            },
          },
        },
      },
    },
    "/api/v1/integrations/keys": { get: { operationId: "ListAPIKeys" } },
  },
  components: {
    schemas: {
      Result: { properties: { value: { $ref: "#/components/schemas/Value" } } },
      Value: { type: "string" },
      Secret: { type: "string" },
    },
  },
});
test("公开合同恰好选取实际登记并递归保留引用，不泄露管理模型", () => {
  const out = projectPublicOpenAPI(source(), [route]);
  assert.deepEqual(Object.keys(out.paths), ["/api/v1/example/{id}"]);
  assert.deepEqual(Object.keys(out.components.schemas).sort(), [
    "Result",
    "Value",
  ]);
  assert.deepEqual(out.paths["/api/v1/example/{id}"].get.security, [
    { APIKey: [] },
  ]);
  assert.deepEqual(out.components.securitySchemes.APIKey, {
    type: "apiKey",
    in: "header",
    name: "X-API-Key",
  });
});
test("缺失路径、操作标识错配、重复登记和悬空引用阻止生成", () => {
  assert.throws(
    () => projectPublicOpenAPI(source(), [{ ...route, path: "/missing" }]),
    /missing/,
  );
  assert.throws(
    () => projectPublicOpenAPI(source(), [{ ...route, operationId: "Wrong" }]),
    /operationId/,
  );
  assert.throws(
    () => projectPublicOpenAPI(source(), [route, route]),
    /duplicate/,
  );
  const doc = source();
  delete doc.components.schemas.Value;
  assert.throws(() => projectPublicOpenAPI(doc, [route]), /reference/);
});
