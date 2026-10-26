import { execFileSync } from "node:child_process";
import { chmod, copyFile, mkdir, rm } from "node:fs/promises";
import path from "node:path";

// 许可从实际引用的模块版本收集，不随源码副本或开发机本地路径交付。
export async function copyACPLicenses(root, destination) {
  const modules = [
    ["github.com/coder/acp-go-sdk", "acp-go-sdk", ["LICENSE"]],
    [
      "github.com/JieWaZi/acp-go",
      "acp-go",
      [
        "pkg/autoreview/UPSTREAM-LICENSE",
        "pkg/pi/UPSTREAM-LICENSE",
        "pkg/cursor/licenses/omnigent-LICENSE",
        "pkg/cursor/licenses/omnigent-NOTICE",
        "pkg/cursor/licenses/cursor-local-acp-LICENSE",
      ],
    ],
  ];
  for (const [modulePath, directory, files] of modules) {
    const module = JSON.parse(
      execFileSync("go", ["list", "-m", "-json", modulePath], {
        cwd: path.join(root, "server"),
        encoding: "utf8",
      }),
    );
    for (const file of files) {
      const output = path.join(destination, directory, file);
      await mkdir(path.dirname(output), { recursive: true });
      await rm(output, { force: true });
      await copyFile(path.join(module.Dir, file), output);
      await chmod(output, 0o644);
    }
  }
}
