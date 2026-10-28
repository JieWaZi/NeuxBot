import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import "@/shared/i18n";
import { WorkflowFailure } from "./failure-details";

afterEach(cleanup);

it("错误显示具体原因和处理办法，不呈现内部协议封套", async () => {
  render(
    <WorkflowFailure
      value={{
        code: "TOOL_FAILED",
        reason: "IMWorkflowDeliveryNotAllowed",
        targetId: "chat",
        sourceRange: { start: { line: 81 } },
        cause: {
          code: "IMWorkflowDeliveryNotAllowed",
          hint: "enable_workflow_delivery",
        },
      }}
    />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent("此聊天未允许工作流发送");
  expect(screen.getByRole("alert")).toHaveTextContent("开启“工作流发送”");
  expect(
    screen.queryByText(/targetId|enable_workflow_delivery/),
  ).not.toBeInTheDocument();
});

it("平台结果不确定时保留已发送片段并提示核对", () => {
  render(
    <WorkflowFailure
      value={{
        code: "TOOL_FAILED",
        delivery: {
          deliveryId: "original",
          state: "unknown",
          errorCode: "IMPlatformUnavailable",
          partsSent: 1,
        },
      }}
    />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent("消息平台暂时不可用");
  expect(screen.getByText(/^发送结果不确定，请先核对/)).toHaveTextContent(
    "避免重复发送",
  );
  expect(screen.queryByText(/partsSent|deliveryId/)).not.toBeInTheDocument();
});
