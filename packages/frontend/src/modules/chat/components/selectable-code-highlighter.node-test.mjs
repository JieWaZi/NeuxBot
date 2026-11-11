import { build } from "esbuild";
import path from "node:path";
import { createRequire } from "node:module";
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";
import { Buffer } from "node:buffer";
import { tmpdir } from "node:os";
import { describe, it } from "node:test";
import assert from "node:assert/strict";

describe("SelectableCodeViewer 交付资源", () => {
  it("不交付文件预览无法选择的语言与主题", async () => {
    const result = await build({
      entryPoints: [
        path.join(import.meta.dirname, "selectable-code-viewer.tsx"),
      ],
      bundle: true,
      splitting: true,
      format: "esm",
      platform: "browser",
      outdir: path.join(tmpdir(), "neux-selectable-code-bundle-test"),
      write: false,
      metafile: true,
      logLevel: "silent",
    });
    const shippedInputs = Object.values(result.metafile.outputs).flatMap(
      (output) =>
        Object.entries(output.inputs)
          .filter(([, input]) => input.bytesInOutput > 0)
          .map(([name]) => name),
    );
    assert.equal(
      shippedInputs.some((name) => name.includes("/emacs-lisp.mjs")),
      false,
    );
    assert.equal(
      shippedInputs.some((name) => name.includes("/solarized-dark.mjs")),
      false,
    );
    assert.equal(
      shippedInputs.some((name) => name.includes("/one-dark-pro.mjs")),
      true,
    );
    assert.equal(
      shippedInputs.some((name) => name.includes("/one-light.mjs")),
      true,
    );
    const { bundledLanguagesInfo } = await import("shiki");
    const languageInfo = new Map(
      bundledLanguagesInfo.flatMap((info) =>
        [info.id, ...(info.aliases ?? [])].map((name) => [name, info]),
      ),
    );
    const pending = [
      "c",
      "cpp",
      "csharp",
      "css",
      "go",
      "html",
      "java",
      "javascript",
      "jsx",
      "json",
      "jsonc",
      "kotlin",
      "less",
      "lua",
      "python",
      "ruby",
      "rust",
      "scss",
      "sql",
      "svelte",
      "toml",
      "typescript",
      "tsx",
      "vue",
      "xml",
      "yaml",
      "dockerfile",
      "makefile",
      "bash",
    ];
    const visited = new Set();
    while (pending.length) {
      const info = languageInfo.get(pending.shift());
      assert.ok(info);
      if (visited.has(info.id)) continue;
      visited.add(info.id);
      assert.ok(
        shippedInputs.some((name) =>
          name.endsWith(`/@shikijs/langs/dist/${info.id}.mjs`),
        ),
        info.id,
      );
      const grammar = await info.import();
      for (const registration of grammar.default)
        pending.push(...(registration.embeddedLangsLazy ?? []));
    }
  });
});

async function isolatedHelperSource() {
  const require = createRequire(import.meta.url);
  const source = await readFile(
    path.join(import.meta.dirname, "selectable-code-highlighter.ts"),
    "utf8",
  );
  const result = await build({
    stdin: {
      contents:
        source +
        "\nexport async function disposeTestHighlighter() { if (pendingHighlighter) (await pendingHighlighter).dispose(); }",
      loader: "ts",
      resolveDir: import.meta.dirname,
      sourcefile: "selectable-code-highlighter.ts",
    },
    bundle: true,
    format: "esm",
    platform: "node",
    write: false,
    logLevel: "silent",
    plugins: [
      {
        name: "external-version-matched-shiki",
        setup(builder) {
          builder.onResolve(
            { filter: /^shiki(?:\/|$)/ },
            ({ path: specifier }) => ({
              path: pathToFileURL(require.resolve(specifier)).href,
              external: true,
            }),
          );
        },
      },
    ],
  });
  return `data:text/javascript;base64,${Buffer.from(result.outputFiles[0].contents).toString("base64")}`;
}

async function originalTokens(content, language, theme) {
  const { createHighlighter } = await import("shiki");
  const { createSingletonShorthands, guessEmbeddedLanguages } =
    await import("shiki/core");
  const original = createSingletonShorthands(createHighlighter, {
    guessEmbeddedLanguages,
  });
  try {
    return (
      await original.codeToTokens(content, {
        lang: language,
        theme: theme === "dark" ? "one-dark-pro" : "one-light",
      })
    ).tokens;
  } finally {
    (await original.getSingletonHighlighter()).dispose();
  }
}

async function coldTokens(source, identity, content, language, theme) {
  const { highlightSelectableCode, disposeTestHighlighter } = await import(
    `${source}#${identity}`
  );
  try {
    return await highlightSelectableCode(content, language, theme);
  } finally {
    await disposeTestHighlighter();
  }
}

it("所有既有文件语法在独立冷实例中保持原 Shiki 两主题的 token 结果", async () => {
  const source = await isolatedHelperSource();
  const languages = [
    "c",
    "cpp",
    "csharp",
    "css",
    "go",
    "html",
    "java",
    "javascript",
    "jsx",
    "json",
    "jsonc",
    "kotlin",
    "less",
    "lua",
    "python",
    "ruby",
    "rust",
    "scss",
    "sql",
    "svelte",
    "toml",
    "typescript",
    "tsx",
    "vue",
    "xml",
    "yaml",
    "dockerfile",
    "makefile",
    "bash",
  ];
  for (const theme of ["dark", "light"]) {
    for (const language of languages) {
      const content =
        language === "vue" || language === "svelte"
          ? '<script lang="ts">const name = "NeuxBot";</script>\n<style>.name { color: red }</style>\n<p>{name}</p>'
          : 'const name = "NeuxBot";\n// example\nreturn 42;';
      assert.deepEqual(
        await coldTokens(
          source,
          `${language}/${theme}`,
          content,
          language,
          theme,
        ),
        await originalTokens(content, language, theme),
        `${language}/${theme}`,
      );
    }
  }
});

it("首次打开 Vue/Svelte 时补载其延迟嵌入语法及原别名", async () => {
  const source = await isolatedHelperSource();
  const cases = [
    [
      "vue",
      '<style lang="scss">$accent: #f00; .name { color: $accent; &:hover { color: blue } }</style>',
    ],
    [
      "svelte",
      '<style lang="scss">$accent: #f00; .name { color: $accent; &:hover { color: blue } }</style>',
    ],
    [
      "vue",
      '<script lang="tsx">const view = <span className="name">Hello</span>;</script>',
    ],
    ["vue", '<template lang="pug">div.name(v-if="ready") Hello</template>'],
    ["vue", '<script lang="coffeescript">square = (x) -> x * x</script>'],
    [
      "vue",
      '<template lang="md">```swift\nlet name = "NeuxBot"\n```</template>',
    ],
  ];
  for (const theme of ["dark", "light"]) {
    for (const [index, [language, content]] of cases.entries()) {
      assert.deepEqual(
        await coldTokens(
          source,
          `embedded/${index}/${theme}`,
          content,
          language,
          theme,
        ),
        await originalTokens(content, language, theme),
        `${language}/${index}/${theme}`,
      );
    }
  }
});
