import { describe, expectTypeOf, it } from "vitest";

import type {
  DeleteChatErrors,
  GetChatMessageFileChangeErrors,
  ListChatEventsErrors,
  ListChatMessagesErrors,
  UpdateChatErrors,
} from "./generated/types.gen";

describe("Chat OpenAPI 错误契约", () => {
  it("为生成客户端保留稳定的 HTTP 错误响应", () => {
    expectTypeOf<keyof DeleteChatErrors>().toEqualTypeOf<404 | 409 | 500>();
    expectTypeOf<keyof UpdateChatErrors>().toEqualTypeOf<400 | 404 | 500>();
    expectTypeOf<keyof ListChatEventsErrors>().toEqualTypeOf<500>();
    expectTypeOf<keyof ListChatMessagesErrors>().toEqualTypeOf<400 | 500>();
    expectTypeOf<keyof GetChatMessageFileChangeErrors>().toEqualTypeOf<
      400 | 404 | 500
    >();
  });
});
