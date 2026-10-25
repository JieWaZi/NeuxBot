import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { ApiError } from "@/shared/api/errors";
import { createKnowledgeUploadQueue } from "./knowledge-upload-queue";
import { KnowledgeUploadDialog as UploadSources } from "./knowledge-upload-dialog";

async function readyUploadInput() {
  await vi.dynamicImportSettled();
  return screen.findByLabelText<HTMLInputElement>("选择文件");
}

it("自动处理提示固定呈现，选择文件不发送，点击后显示远端接纳事实", async () => {
  const upload = vi.fn().mockResolvedValue({
    accepted: true,
    uploadMode: "automatic",
    sourceId: "source-a",
    processingState: "pending",
  });
  const user = userEvent.setup();
  render(
    <UploadSources opened mode="automatic" close={() => {}} upload={upload} />,
  );
  expect(screen.getAllByText(/上传后将由远端自动处理/).length).toBeGreaterThan(
    0,
  );
  await user.upload(await readyUploadInput(), [new File(["a"], "a.md")]);
  expect(upload).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "上传文档" }));
  expect(await screen.findByText("远端已接纳")).toBeVisible();
  expect(screen.queryByText("待整理")).not.toBeInTheDocument();
  expect(upload).toHaveBeenCalledTimes(1);
});

it("连接版本变化停止剩余文件，未知模式禁用上传", async () => {
  const upload = vi
    .fn()
    .mockRejectedValue(
      new ApiError("模式已变化", "KnowledgeUploadModeChanged", 409),
    );
  const user = userEvent.setup();
  const view = render(
    <UploadSources opened mode="separate" close={() => {}} upload={upload} />,
  );
  await user.upload(await readyUploadInput(), [
    new File(["a"], "a.md"),
    new File(["b"], "b.md"),
  ]);
  await user.click(screen.getByRole("button", { name: "上传文档" }));
  expect(
    await screen.findByText(
      "远端上传行为或连接版本已变化。当前队列已停止，请核对新提示后再次提交。",
    ),
  ).toBeVisible();
  expect(screen.queryByText("模式已变化")).not.toBeInTheDocument();
  expect(upload).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("button", { name: "重试失败文档" })).toBeDisabled();
  view.unmount();
  render(
    <UploadSources opened mode="unknown" close={() => {}} upload={upload} />,
  );
  expect(
    await screen.findByRole("button", { name: "上传文档" }),
  ).toBeDisabled();
});

it("review: 两份内容不同但元数据相同的文件不应被静默丢弃", async () => {
  const upload = vi.fn().mockResolvedValue(undefined);
  const user = userEvent.setup();
  render(<UploadSources opened close={() => {}} upload={upload} />);
  await user.upload(await readyUploadInput(), [
    new File(["a"], "readme.md", { lastModified: 1 }),
    new File(["b"], "readme.md", { lastModified: 1 }),
  ]);
  await user.click(
    screen.getByRole("button", { name: /上传文档|重试失败文档/ }),
  );
  await waitFor(() => expect(screen.getAllByText("待整理")).toHaveLength(2));
  expect(upload).toHaveBeenCalledTimes(2);
});

it("review: 上传未知回执后关闭重开弹窗，重试应保留原请求身份", async () => {
  const upload = vi
    .fn()
    .mockRejectedValueOnce(new ApiError("连接中断，是否接纳未知", "NETWORK"))
    .mockResolvedValueOnce(undefined);
  const file = new File(["a"], "readme.md", { lastModified: 1 });
  const user = userEvent.setup();
  const queue = createKnowledgeUploadQueue();
  const first = render(
    <UploadSources opened queue={queue} close={() => {}} upload={upload} />,
  );
  await user.upload(await readyUploadInput(), [file]);
  await user.click(
    screen.getByRole("button", { name: /上传文档|重试失败文档/ }),
  );
  expect(await screen.findByText("上传失败")).toBeVisible();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  first.unmount();
  render(
    <UploadSources opened queue={queue} close={() => {}} upload={upload} />,
  );
  await user.upload(await readyUploadInput(), [file]);
  await user.click(
    screen.getByRole("button", { name: /上传文档|重试失败文档/ }),
  );
  await waitFor(() => expect(upload).toHaveBeenCalledTimes(2));
  expect(upload.mock.calls[1]?.[1]).toBe(upload.mock.calls[0]?.[1]);
});

it("未知接纳的上传不能通过移除丢失原请求，不同 File 对象相同字节复用原项", async () => {
  const upload = vi
    .fn()
    .mockRejectedValueOnce(new ApiError("连接中断", "NETWORK"))
    .mockResolvedValueOnce(undefined);
  const queue = createKnowledgeUploadQueue();
  const user = userEvent.setup();
  const first = render(
    <UploadSources opened queue={queue} close={() => {}} upload={upload} />,
  );
  await user.upload(await readyUploadInput(), [
    new File(["same"], "readme.md", { lastModified: 1 }),
  ]);
  await user.click(screen.getByRole("button", { name: "上传文档" }));
  expect(await screen.findByText("上传失败")).toBeVisible();
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "移除 readme.md" })).toBeDisabled();
  first.unmount();
  render(
    <UploadSources opened queue={queue} close={() => {}} upload={upload} />,
  );
  await user.upload(await readyUploadInput(), [
    new File(["same"], "readme.md", { lastModified: 2 }),
  ]);
  await user.click(screen.getByRole("button", { name: "重试失败文档" }));
  await waitFor(() =>
    expect(screen.getByText("内容重复，沿用原上传项")).toBeVisible(),
  );
  expect(upload).toHaveBeenCalledTimes(2);
  expect(upload.mock.calls[1]?.[1]).toBe(upload.mock.calls[0]?.[1]);
});

it("上传进行中 Escape 不关闭外壳，完成后仍能正常关闭", async () => {
  let finish!: () => void;
  const close = vi.fn();
  const upload = vi.fn(
    () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  const user = userEvent.setup();
  render(<UploadSources opened close={close} upload={upload} />);
  await user.upload(await readyUploadInput(), [new File(["a"], "资料.md")]);
  await user.click(screen.getByRole("button", { name: "上传文档" }));
  await waitFor(() => expect(upload).toHaveBeenCalledOnce());
  await user.keyboard("{Escape}");
  expect(close).not.toHaveBeenCalled();
  expect(screen.getByRole("dialog", { name: "上传资料" })).toBeVisible();
  finish();
  await screen.findByText("待整理");
  await user.keyboard("{Escape}");
  expect(close).toHaveBeenCalledOnce();
});

it("文件夹将嵌套资料加入同一队列，保留相对路径展示并跳过隐藏和不支持文件", async () => {
  const upload = vi.fn().mockResolvedValue(undefined);
  const user = userEvent.setup({ applyAccept: false });
  render(<UploadSources opened close={() => {}} upload={upload} />);
  expect(
    (await screen.findAllByRole("radio")).map((item) => item.textContent),
  ).toEqual(["文件", "文件夹"]);
  await user.click(screen.getByRole("radio", { name: "文件夹" }));
  const input = screen.getByLabelText<HTMLInputElement>("选择文件夹");
  expect(input).toHaveAttribute("webkitdirectory");
  const sources: [string, string, string][] = [
    ["资料/第一章/readme.md", "readme.md", "one"],
    ["资料/第二章/readme.md", "readme.md", "two"],
    ["资料/.hidden/secret.md", "secret.md", "hidden"],
    ["资料/archive.zip", "archive.zip", "zip"],
  ];
  const files = sources.map(([path, name, content]) => {
    const file = new File([content], name);
    Object.defineProperty(file, "webkitRelativePath", { value: path });
    return file;
  });
  await user.upload(input, files);
  expect(screen.getByText("资料/第一章/readme.md")).toBeVisible();
  expect(screen.getByText("资料/第二章/readme.md")).toBeVisible();
  expect(screen.queryByText(/secret.md/)).not.toBeInTheDocument();
  expect(screen.getByRole("alert")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "上传文档" }));
  await waitFor(() => expect(screen.getAllByText("待整理")).toHaveLength(2));
  expect(upload).toHaveBeenCalledTimes(2);
  expect(upload.mock.calls[0]?.[0].name).toBe("readme.md");
});

it("文件与文件夹选择取消均保留既有队列，不产生空目录错误", async () => {
  const upload = vi.fn().mockResolvedValue(undefined);
  const user = userEvent.setup();
  render(<UploadSources opened close={() => {}} upload={upload} />);
  const files = await readyUploadInput();
  await user.upload(files, [new File(["existing"], "资料.md")]);
  fireEvent.change(files, { target: { files: [] } });
  await user.click(screen.getByRole("radio", { name: "文件夹" }));
  fireEvent.change(screen.getByLabelText("选择文件夹"), {
    target: { files: [] },
  });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByText("资料.md")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "上传文档" }));
  expect(await screen.findByText("待整理")).toBeVisible();
  expect(upload).toHaveBeenCalledTimes(1);
});
