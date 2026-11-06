import { describe, expect, it } from "vitest";
import { chatFileRoots } from "./project-files-scope";

describe("chatFileRoots", () => {
  it("browses actual cwd and each frozen project without a virtual namespace", () => {
    expect(
      chatFileRoots("/managed/Agents/waker", [
        "/home/project-a",
        "/home/project-b",
        "/home/project-a",
      ]),
    ).toEqual(["/managed/Agents/waker", "/home/project-a", "/home/project-b"]);
  });
  it("keeps an explicitly selected draft project directly accessible", () => {
    expect(chatFileRoots("", ["/home/project"])).toEqual(["/home/project"]);
  });
});
