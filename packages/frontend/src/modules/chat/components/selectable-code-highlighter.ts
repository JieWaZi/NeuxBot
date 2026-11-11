import { createHighlighterCore, guessEmbeddedLanguages } from "shiki/core";
import type { HighlighterCore } from "shiki/core";
import { createOnigurumaEngine } from "shiki/engine/oniguruma";

// 文件预览保留已支持的入口语法与两套主题；静态嵌入由语法模块携带。
const languageLoaders = {
  c: () => import("shiki/langs/c.mjs"),
  cpp: () => import("shiki/langs/cpp.mjs"),
  csharp: () => import("shiki/langs/csharp.mjs"),
  css: () => import("shiki/langs/css.mjs"),
  go: () => import("shiki/langs/go.mjs"),
  html: () => import("shiki/langs/html.mjs"),
  java: () => import("shiki/langs/java.mjs"),
  javascript: () => import("shiki/langs/javascript.mjs"),
  jsx: () => import("shiki/langs/jsx.mjs"),
  json: () => import("shiki/langs/json.mjs"),
  jsonc: () => import("shiki/langs/jsonc.mjs"),
  kotlin: () => import("shiki/langs/kotlin.mjs"),
  less: () => import("shiki/langs/less.mjs"),
  lua: () => import("shiki/langs/lua.mjs"),
  python: () => import("shiki/langs/python.mjs"),
  ruby: () => import("shiki/langs/ruby.mjs"),
  rust: () => import("shiki/langs/rust.mjs"),
  scss: () => import("shiki/langs/scss.mjs"),
  sql: () => import("shiki/langs/sql.mjs"),
  svelte: () => import("shiki/langs/svelte.mjs"),
  toml: () => import("shiki/langs/toml.mjs"),
  typescript: () => import("shiki/langs/typescript.mjs"),
  tsx: () => import("shiki/langs/tsx.mjs"),
  vue: () => import("shiki/langs/vue.mjs"),
  xml: () => import("shiki/langs/xml.mjs"),
  yaml: () => import("shiki/langs/yaml.mjs"),
  dockerfile: () => import("shiki/langs/dockerfile.mjs"),
  makefile: () => import("shiki/langs/make.mjs"),
  bash: () => import("shiki/langs/shellscript.mjs"),
};
// 包含入口语法及其 embeddedLangsLazy 递归闭包；只在正文命中时补载。
const embeddedLanguageLoaders = new Map<
  string,
  (typeof languageLoaders)[keyof typeof languageLoaders]
>(
  Object.entries({
    ...languageLoaders,
    abap: () => import("shiki/langs/abap.mjs"),
    bat: () => import("shiki/langs/bat.mjs"),
    bibtex: () => import("shiki/langs/bibtex.mjs"),
    clojure: () => import("shiki/langs/clojure.mjs"),
    coffee: () => import("shiki/langs/coffee.mjs"),
    dart: () => import("shiki/langs/dart.mjs"),
    diff: () => import("shiki/langs/diff.mjs"),
    docker: languageLoaders.dockerfile,
    elixir: () => import("shiki/langs/elixir.mjs"),
    erlang: () => import("shiki/langs/erlang.mjs"),
    fsharp: () => import("shiki/langs/fsharp.mjs"),
    "git-commit": () => import("shiki/langs/git-commit.mjs"),
    "git-rebase": () => import("shiki/langs/git-rebase.mjs"),
    gnuplot: () => import("shiki/langs/gnuplot.mjs"),
    graphql: () => import("shiki/langs/graphql.mjs"),
    groovy: () => import("shiki/langs/groovy.mjs"),
    handlebars: () => import("shiki/langs/handlebars.mjs"),
    haskell: () => import("shiki/langs/haskell.mjs"),
    "html-derivative": () => import("shiki/langs/html-derivative.mjs"),
    ini: () => import("shiki/langs/ini.mjs"),
    json5: () => import("shiki/langs/json5.mjs"),
    jsonl: () => import("shiki/langs/jsonl.mjs"),
    julia: () => import("shiki/langs/julia.mjs"),
    latex: () => import("shiki/langs/latex.mjs"),
    log: () => import("shiki/langs/log.mjs"),
    make: languageLoaders.makefile,
    markdown: () => import("shiki/langs/markdown.mjs"),
    "objective-c": () => import("shiki/langs/objective-c.mjs"),
    perl: () => import("shiki/langs/perl.mjs"),
    php: () => import("shiki/langs/php.mjs"),
    powershell: () => import("shiki/langs/powershell.mjs"),
    pug: () => import("shiki/langs/pug.mjs"),
    r: () => import("shiki/langs/r.mjs"),
    raku: () => import("shiki/langs/raku.mjs"),
    regexp: () => import("shiki/langs/regexp.mjs"),
    rst: () => import("shiki/langs/rst.mjs"),
    sass: () => import("shiki/langs/sass.mjs"),
    scala: () => import("shiki/langs/scala.mjs"),
    shellscript: languageLoaders.bash,
    stylus: () => import("shiki/langs/stylus.mjs"),
    swift: () => import("shiki/langs/swift.mjs"),
    vb: () => import("shiki/langs/vb.mjs"),
    xsl: () => import("shiki/langs/xsl.mjs"),
  }),
);
const embeddedLanguageAliases = new Map<string, string>([
  ["batch", "bat"],
  ["clj", "clojure"],
  ["coffeescript", "coffee"],
  ["c++", "cpp"],
  ["c#", "csharp"],
  ["cs", "csharp"],
  ["dockerfile", "docker"],
  ["erl", "erlang"],
  ["f#", "fsharp"],
  ["fs", "fsharp"],
  ["gql", "graphql"],
  ["hbs", "handlebars"],
  ["hs", "haskell"],
  ["properties", "ini"],
  ["js", "javascript"],
  ["cjs", "javascript"],
  ["mjs", "javascript"],
  ["jl", "julia"],
  ["kt", "kotlin"],
  ["kts", "kotlin"],
  ["makefile", "make"],
  ["md", "markdown"],
  ["objc", "objective-c"],
  ["ps", "powershell"],
  ["ps1", "powershell"],
  ["jade", "pug"],
  ["py", "python"],
  ["perl6", "raku"],
  ["regex", "regexp"],
  ["rb", "ruby"],
  ["rs", "rust"],
  ["bash", "shellscript"],
  ["sh", "shellscript"],
  ["shell", "shellscript"],
  ["zsh", "shellscript"],
  ["styl", "stylus"],
  ["ts", "typescript"],
  ["cts", "typescript"],
  ["mts", "typescript"],
  ["cmd", "vb"],
  ["yml", "yaml"],
]);
const themeLoaders = {
  dark: () => import("shiki/themes/one-dark-pro.mjs"),
  light: () => import("shiki/themes/one-light.mjs"),
};
export type SelectableCodeLanguage = keyof typeof languageLoaders;
type Theme = keyof typeof themeLoaders;
let pendingHighlighter: Promise<HighlighterCore> | undefined;

function highlighter(): Promise<HighlighterCore> {
  pendingHighlighter ??= createHighlighterCore({
    engine: createOnigurumaEngine(import("shiki/wasm")),
    langs: [],
    themes: [],
  }).catch((error: unknown) => {
    pendingHighlighter = undefined;
    throw error;
  });
  return pendingHighlighter;
}

/** 保留原文件类型、Oniguruma 规则和主题，失败由调用方保持纯文本。 */
export async function highlightSelectableCode(
  content: string,
  language: SelectableCodeLanguage,
  theme: Theme,
) {
  const [instance, grammar, appearance] = await Promise.all([
    highlighter(),
    languageLoaders[language](),
    themeLoaders[theme](),
  ]);
  await Promise.all([
    instance.loadLanguage(grammar.default),
    instance.loadTheme(appearance.default),
  ]);
  const embedded = await Promise.all(
    guessEmbeddedLanguages(content, language).map((name) =>
      embeddedLanguageLoaders.get(
        embeddedLanguageAliases.get(name) ?? name,
      )?.(),
    ),
  );
  await instance.loadLanguage(
    ...embedded.flatMap((entry) => (entry ? [entry.default] : [])),
  );
  return instance.codeToTokens(content, {
    lang: language,
    theme: appearance.default,
  }).tokens;
}
