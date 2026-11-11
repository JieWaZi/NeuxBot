import { describe, expect, it } from "vitest";
import { chatFileRoots, workingDirectoryRoots } from "./project-files-scope";

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

describe("workingDirectoryRoots", () => {
  it("excludes the shared space and duplicate roots without altering real paths", () => {
    expect(workingDirectoryRoots("", ["/"])).toEqual(["/"]);
    expect(
      workingDirectoryRoots("/managed/space", [
        "/managed/space/",
        "/project",
        "/project/",
        "",
        "/other",
      ]),
    ).toEqual(["/project", "/other"]);
    expect(
      workingDirectoryRoots("C:\\Space", [
        "c:/space/",
        "D:\\Project",
        "d:/project/",
      ]),
    ).toEqual(["D:\\Project"]);
  });
});
