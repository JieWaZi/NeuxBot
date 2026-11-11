import defaultFile from "@/shared/assets/material-icon-theme/file.svg?no-inline";
import folder from "@/shared/assets/material-icon-theme/folder.svg?no-inline";
import folderOpen from "@/shared/assets/material-icon-theme/folder-open.svg?no-inline";

// 只扫描随项目保存的精选子集，不引入上游的完整图标库或远程图标请求。
const assets = import.meta.glob<string>("../assets/material-icon-theme/*.svg", {
  eager: true,
  import: "default",
  query: "?url&no-inline",
});

type FileTone =
  | "neutral"
  | "blue"
  | "cyan"
  | "teal"
  | "green"
  | "lime"
  | "amber"
  | "orange"
  | "red"
  | "purple";
type FileKind = { icon: string; tone: FileTone };

const kinds = {
  markdown: { icon: "markdown", tone: "blue" },
  text: { icon: "document", tone: "blue" },
  js: { icon: "javascript", tone: "amber" },
  typescript: { icon: "typescript", tone: "blue" },
  reactjs: { icon: "react", tone: "cyan" },
  reactts: { icon: "react_ts", tone: "blue" },
  vue: { icon: "vue", tone: "green" },
  html: { icon: "html", tone: "orange" },
  css: { icon: "css", tone: "purple" },
  go: { icon: "go", tone: "cyan" },
  python: { icon: "python", tone: "blue" },
  java: { icon: "java", tone: "red" },
  ruby: { icon: "ruby", tone: "red" },
  shell: { icon: "console", tone: "orange" },
  json: { icon: "json", tone: "amber" },
  yaml: { icon: "yaml", tone: "red" },
  toml: { icon: "toml", tone: "red" },
  dotenv: { icon: "tune", tone: "amber" },
  git: { icon: "git", tone: "orange" },
  docker: { icon: "docker", tone: "blue" },
  npm: { icon: "nodejs", tone: "green" },
  image: { icon: "image", tone: "teal" },
  pdf: { icon: "pdf", tone: "red" },
  word: { icon: "word", tone: "blue" },
  excel: { icon: "table", tone: "green" },
  powerpoint: { icon: "powerpoint", tone: "orange" },
  zip: { icon: "zip", tone: "lime" },
  rust: { icon: "rust", tone: "orange" },
  svg: { icon: "svg", tone: "amber" },
  log: { icon: "log", tone: "lime" },
  tsconfig: { icon: "tsconfig", tone: "blue" },
} satisfies Record<string, FileKind>;

const extensions: Record<string, keyof typeof kinds> = {
  md: "markdown",
  markdown: "markdown",
  txt: "text",
  log: "log",
  js: "js",
  mjs: "js",
  cjs: "js",
  jsx: "reactjs",
  ts: "typescript",
  mts: "typescript",
  cts: "typescript",
  tsx: "reactts",
  vue: "vue",
  html: "html",
  htm: "html",
  css: "css",
  go: "go",
  py: "python",
  java: "java",
  rb: "ruby",
  rs: "rust",
  sh: "shell",
  bash: "shell",
  zsh: "shell",
  fish: "shell",
  ps1: "shell",
  bat: "shell",
  cmd: "shell",
  json: "json",
  jsonc: "json",
  jsonl: "json",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  png: "image",
  jpg: "image",
  jpeg: "image",
  gif: "image",
  webp: "image",
  avif: "image",
  ico: "image",
  bmp: "image",
  svg: "svg",
  pdf: "pdf",
  doc: "word",
  docx: "word",
  xls: "excel",
  xlsx: "excel",
  csv: "excel",
  ppt: "powerpoint",
  pptx: "powerpoint",
  zip: "zip",
  tar: "zip",
  gz: "zip",
  tgz: "zip",
  bz2: "zip",
  xz: "zip",
  "7z": "zip",
  rar: "zip",
};

const filenames: Record<string, keyof typeof kinds> = {
  "package.json": "npm",
  "package-lock.json": "npm",
  ".npmrc": "npm",
  ".gitignore": "git",
  ".gitattributes": "git",
  ".gitmodules": "git",
  ".gitconfig": "git",
  dockerfile: "docker",
  "docker-compose.yml": "docker",
  "docker-compose.yaml": "docker",
  "compose.yml": "docker",
  "compose.yaml": "docker",
  ".dockerignore": "docker",
  ".env": "dotenv",
  "go.mod": "go",
  "go.sum": "go",
  gemfile: "ruby",
  "gemfile.lock": "ruby",
};

export function workspaceFileAppearance(
  name: string,
  isDirectory = false,
  isOpen = false,
) {
  if (isDirectory)
    return {
      src: isOpen ? folderOpen : folder,
      tone: "neutral" as const,
    };
  const basename = name.split(/[\\/]/).at(-1)?.toLowerCase() ?? "";
  const special =
    filenames[basename] ??
    (basename.startsWith(".env.") ? "dotenv" : undefined) ??
    (basename.startsWith("dockerfile.") ? "docker" : undefined) ??
    (/^tsconfig(?:\..+)?\.json$/.test(basename) ? "tsconfig" : undefined);
  const extension = basename.includes(".")
    ? basename.split(".").at(-1)
    : undefined;
  const key = special ?? (extension ? extensions[extension] : undefined);
  const kind = key ? kinds[key] : undefined;
  return {
    src: kind
      ? (assets[`../assets/material-icon-theme/${kind.icon}.svg`] ??
        defaultFile)
      : defaultFile,
    tone: kind?.tone ?? "neutral",
  };
}
